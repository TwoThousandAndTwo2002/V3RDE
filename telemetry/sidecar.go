package telemetry

import (
	"encoding/json"
	"log"
	"path/filepath"
	"strings"
	"time"
)

// Shipped wraps one events line of a service: who, which build, from where.
// Line is the service's own JSON, not changed.
type Shipped struct {
	Kind      string          `json:"kind"` // "event"
	Service   string          `json:"service"`
	Build     string          `json:"build"`
	ShippedAt string          `json:"shipped_at"`
	File      string          `json:"file"`
	Offset    int64           `json:"offset"`
	Line      json.RawMessage `json:"line"`
}

// Sample is one second of the service process.
type Sample struct {
	Kind    string  `json:"kind"` // "sample"
	TS      string  `json:"ts"`
	Service string  `json:"service"`
	Build   string  `json:"build"`
	PID     int     `json:"pid"`
	CPUPct  float64 `json:"cpu_pct"`
	RSSMiB  float64 `json:"rss_mib"`
	Threads int     `json:"threads"`
	Error   string  `json:"error,omitempty"`
}

// Lifecycle is a change of the service process, as the sidecar sees it:
//   - "service_started": the first sight of the process (no earlier record);
//   - "service_restarted": a NEW process (its start time is not the one on
//     record): a crash, a restart or a recreate;
//   - "service_down": the process cannot be read (one time per outage);
//   - "service_up": it can be read again after service_down.
//
// When the service's PID 1 stops, its PID namespace ends and this sidecar
// stops too; Docker starts both again, and the new sidecar reports
// service_restarted from the start time it saved. The ledger puts these
// lines next to the events, so a crash shows next to the requests it cut.
type Lifecycle struct {
	Kind      string `json:"kind"` // "lifecycle"
	TS        string `json:"ts"`
	Service   string `json:"service"`
	Build     string `json:"build"`
	Event     string `json:"event"`
	PrevStart uint64 `json:"prev_start_ticks,omitempty"`
	Start     uint64 `json:"start_ticks,omitempty"`
	Error     string `json:"error,omitempty"`
}

// startKey keeps the service's last start time in the offsets file (no
// events file name starts with "@").
const startKey = "@service_start_ticks"

// Sidecar ships and samples one service.
type Sidecar struct {
	Service, Build string
	EventsDir      string // the service's events files (read-only)
	SpoolDir       string // the sidecar's own output
	ProcRoot       string // "/proc"
	PID            int    // 1 = the service, in its shared PID namespace
	Now            func() time.Time

	off      *Offsets
	out      Appender // events-YYYYMMDD.jsonl: shipped events and lifecycle lines
	samples  Appender // samples-YYYYMMDD.jsonl
	prevTick uint64
	prevAt   time.Time
	down     bool
}

// Open loads the saved offsets (a missing file is an empty start).
func (s *Sidecar) Open() error {
	if s.Now == nil {
		s.Now = time.Now
	}
	s.out = Appender{Dir: s.SpoolDir, Prefix: "events"}
	s.samples = Appender{Dir: s.SpoolDir, Prefix: "samples"}
	var err error
	s.off, err = LoadOffsets(filepath.Join(s.SpoolDir, "offsets.json"))
	return err
}

// Close closes the spool files.
func (s *Sidecar) Close() {
	s.out.Close()
	s.samples.Close()
}

// Ship copies every new complete line of every events file. The offsets are
// saved after each file, so a crash ships at most one file's batch again
// (at least once); (service, file, offset) names a line exactly, so a
// repeat can be found. A line that is not JSON is shipped as
// {"unparsed": "<the line>"}.
func (s *Sidecar) Ship() (int, error) {
	n := 0
	for _, path := range Files(s.EventsDir) {
		name := filepath.Base(path)
		lines, next, err := ReadNew(path, s.off.Get(name))
		if err != nil {
			return n, err
		}
		if len(lines) == 0 {
			continue
		}
		now := s.Now().UTC()
		for _, ln := range lines {
			raw := ln.B
			if !json.Valid(raw) {
				raw, _ = json.Marshal(map[string]string{"unparsed": string(raw)})
			}
			b, _ := json.Marshal(Shipped{Kind: "event", Service: s.Service, Build: s.Build,
				ShippedAt: now.Format(time.RFC3339Nano), File: name, Offset: ln.Off, Line: raw})
			if err := s.out.Write(now.Format("20060102"), b); err != nil {
				return n, err
			}
			n++
		}
		if err := s.out.Sync(); err != nil {
			return n, err
		}
		s.off.Set(name, next)
		if err := s.off.Save(); err != nil {
			return n, err
		}
	}
	return n, nil
}

// lifecycle writes one Lifecycle line into the events spool and the log.
func (s *Sidecar) lifecycle(now time.Time, event string, prev, start uint64, why string) {
	b, _ := json.Marshal(Lifecycle{Kind: "lifecycle", TS: now.Format(time.RFC3339Nano), Service: s.Service,
		Build: s.Build, Event: event, PrevStart: prev, Start: start, Error: why})
	if err := s.out.Write(now.Format("20060102"), b); err != nil {
		log.Printf("lifecycle: %v", err)
	}
	log.Printf("%s: %s (start %d -> %d) %s", s.Service, event, prev, start, why)
}

// SampleOnce records the CPU (since the last sample), the memory and the
// threads, and reports the lifecycle changes. The last start time seen is
// saved with the offsets, so it stays after the sidecar's own restart.
func (s *Sidecar) SampleOnce() Sample {
	now := s.Now().UTC()
	smp := Sample{Kind: "sample", TS: now.Format(time.RFC3339Nano), Service: s.Service, Build: s.Build, PID: s.PID}
	p, err := ReadProc(s.ProcRoot, s.PID)
	if err != nil {
		smp.Error = err.Error()
		if !s.down {
			s.down = true
			s.lifecycle(now, "service_down", uint64(s.off.Get(startKey)), 0, err.Error())
		}
	} else {
		s.seen(now, p)
		if !s.prevAt.IsZero() {
			smp.CPUPct = round2(CPUPercent(s.prevTick, p.CPUTicks, now.Sub(s.prevAt).Seconds()))
		}
		smp.RSSMiB, smp.Threads = round2(float64(p.RSSKiB)/1024), p.Threads
		s.prevTick, s.prevAt = p.CPUTicks, now
	}
	b, _ := json.Marshal(smp)
	if err := s.samples.Write(now.Format("20060102"), b); err != nil {
		log.Printf("samples: %v", err)
	}
	return smp
}

// seen reports service_up after an outage, and service_started or
// service_restarted when the start time is not the one on record.
func (s *Sidecar) seen(now time.Time, p ProcStat) {
	if s.down {
		s.down = false
		s.lifecycle(now, "service_up", 0, p.StartTime, "")
	}
	prev := uint64(s.off.Get(startKey))
	if prev == p.StartTime {
		return
	}
	event := "service_restarted"
	if prev == 0 {
		event = "service_started"
	}
	s.lifecycle(now, event, prev, p.StartTime, "")
	s.off.Set(startKey, int64(p.StartTime))
	if err := s.off.Save(); err != nil {
		log.Printf("offsets: %v", err)
	}
	s.prevAt = time.Time{} // a new process: no CPU delta across it
}

// Run samples and ships every interval until stop is closed. A missing
// events file is not logged (the service has not written yet).
func (s *Sidecar) Run(interval time.Duration, stop <-chan struct{}) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		s.SampleOnce()
		if _, err := s.Ship(); err != nil && !strings.Contains(err.Error(), "no such file") {
			log.Printf("ship: %v", err)
		}
		select {
		case <-stop:
			return
		case <-t.C:
		}
	}
}

// round2 rounds to 2 decimals, half up, through int64 (as before: a value
// above about 9.2e16 overflows, which /proc cannot give).
func round2(f float64) float64 { return float64(int64(f*100+0.5)) / 100 }
