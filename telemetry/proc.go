package telemetry

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ClockTicks is USER_HZ: 100 on Linux x86_64 and arm64, the unit of the
// CPU times in /proc/<pid>/stat ("measured in clock ticks (divide by
// sysconf(_SC_CLK_TCK))": https://www.mankier.com/5/proc_pid_stat).
const ClockTicks = 100

// ProcStat is what one read of /proc/<pid> gives.
type ProcStat struct {
	CPUTicks  uint64 // utime + stime
	StartTime uint64 // field 22, ticks after boot: it changes when the process is a new one
	RSSKiB    uint64 // VmRSS
	Threads   int
}

// ParseStat reads utime + stime (fields 14 and 15) and starttime (field 22)
// from /proc/<pid>/stat. Field 2 (comm) is in parentheses and can hold
// spaces and parentheses, so the fields are counted after the LAST ')'.
func ParseStat(b []byte) (ticks, start uint64, err error) {
	end := bytes.LastIndexByte(b, ')')
	if end < 0 {
		return 0, 0, fmt.Errorf("stat: no comm")
	}
	f := strings.Fields(string(b[end+1:]))
	// after comm, field 3 (state) is f[0]: field n is f[n-3]
	const utime, stime, starttime = 14 - 3, 15 - 3, 22 - 3
	if len(f) <= starttime {
		return 0, 0, fmt.Errorf("stat: %d fields", len(f))
	}
	u, err1 := strconv.ParseUint(f[utime], 10, 64)
	s, err2 := strconv.ParseUint(f[stime], 10, 64)
	st, err3 := strconv.ParseUint(f[starttime], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, fmt.Errorf("stat: utime/stime/starttime")
	}
	return u + s, st, nil
}

// ParseStatus reads VmRSS (kB) and Threads from /proc/<pid>/status
// (proc_pid_status(5)). A key that is not there stays 0.
func ParseStatus(b []byte) (rss uint64, threads int, err error) {
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		f := strings.Fields(val)
		if len(f) == 0 {
			continue
		}
		switch key {
		case "VmRSS":
			rss, _ = strconv.ParseUint(f[0], 10, 64)
		case "Threads":
			threads, _ = strconv.Atoi(f[0])
		}
	}
	return rss, threads, sc.Err()
}

// ReadProc reads the stat and the status of pid under root (normally /proc).
func ReadProc(root string, pid int) (ProcStat, error) {
	var p ProcStat
	// root + "/<pid>/..." exactly as before (also for root "" or "/proc/"):
	// the path is in the error text that goes into the spool.
	stat, err := os.ReadFile(fmt.Sprintf("%s/%d/stat", root, pid))
	if err != nil {
		return p, err
	}
	if p.CPUTicks, p.StartTime, err = ParseStat(stat); err != nil {
		return p, err
	}
	status, err := os.ReadFile(fmt.Sprintf("%s/%d/status", root, pid))
	if err != nil {
		return p, err
	}
	p.RSSKiB, p.Threads, err = ParseStatus(status)
	return p, err
}

// CPUPercent is the share of ONE core used between two reads secs apart:
// 100 = one full core (the docker stats way). A counter that went down (a
// new process) or no time is 0.
func CPUPercent(prev, cur uint64, secs float64) float64 {
	if secs <= 0 || cur < prev {
		return 0
	}
	return float64(cur-prev) / ClockTicks / secs * 100
}
