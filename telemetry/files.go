// Package telemetry is the sidecar and the ledger of V3RDE.
//
//   - A sidecar runs next to one service, in that service's PID namespace.
//     Every second it reads the service's CPU, memory and threads from
//     /proc, and it ships the service's own events files line by line,
//     with the service name and the build.
//   - The ledger merges the spools of every sidecar into one log and one
//     samples file per day, and answers GET /v1/trace/{id}.
//
// Neither is on the request path: the services only append to their own
// files, and everything here reads files. The read positions are saved, so
// a restart goes on where it stopped; only complete lines (ending in '\n')
// are shipped, so a half-written event is never cut.
//
// V3RDE #29 (Oct 29 2026): this package is written new for V3RDE. The file
// formats (the spool and ledger lines, the offsets file on the volume), the
// lifecycle signals and the trace route are kept the same as the telemetry
// the app used before the Hackathon.
package telemetry

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// maxRead is the most one ReadNew reads; the rest comes on the next call.
const maxRead = 64 << 20

// Offsets is how far each file has been read, saved as {"offsets":{...}}.
type Offsets struct {
	mu   sync.Mutex
	path string
	M    map[string]int64 `json:"offsets"`
}

// LoadOffsets reads path; a missing file is an empty start.
func LoadOffsets(path string) (*Offsets, error) {
	o := &Offsets{path: path, M: map[string]int64{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return o, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, o); err != nil {
		return nil, err
	}
	if o.M == nil {
		o.M = map[string]int64{}
	}
	return o, nil
}

// Get is the offset of key (0 when there is none).
func (o *Offsets) Get(key string) int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.M[key]
}

// Set changes the offset of key in memory; Save writes it.
func (o *Offsets) Set(key string, v int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.M[key] = v
}

// Save writes the offsets through a temp file and a rename, so a crash
// leaves the old file or the new one.
func (o *Offsets) Save() error {
	o.mu.Lock()
	b, err := json.Marshal(o)
	o.mu.Unlock()
	if err != nil {
		return err
	}
	tmp := o.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync() // the X38e review: an empty offsets file after a power cut stopped the start
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, o.path)
}

// Line is one complete line, without spaces around it, and the byte offset
// in the file where its line starts.
type Line struct {
	Off int64
	B   []byte
}

// ReadNew returns the complete lines of path after offset, and the offset
// just after the last '\n'. A part line at the end stays for the next call.
// A file shorter than offset (it was replaced) is read from the start.
// Empty lines are skipped. At most maxRead bytes are read per call.
func ReadNew(path string, offset int64) ([]Line, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, offset, err
	}
	if st.Size() < offset {
		offset = 0
	}
	if st.Size() == offset {
		return nil, offset, nil
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	data, err := io.ReadAll(io.LimitReader(f, maxRead))
	if err != nil {
		return nil, offset, err
	}
	last := bytes.LastIndexByte(data, '\n')
	if last < 0 {
		return nil, offset, nil
	}
	var lines []Line
	for pos := 0; pos <= last; {
		n := bytes.IndexByte(data[pos:], '\n')
		if t := bytes.TrimSpace(data[pos : pos+n]); len(t) > 0 {
			lines = append(lines, Line{Off: offset + int64(pos), B: bytes.Clone(t)})
		}
		pos += n + 1
	}
	return lines, offset + int64(last) + 1, nil
}

// Files is dir/*.jsonl in name order (names with YYYYMMDD sort by day).
func Files(dir string) []string {
	m, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	sort.Strings(m)
	return m
}

// Appender writes whole lines to <Dir>/<Prefix>-YYYYMMDD.jsonl; it keeps the
// file of one day open.
type Appender struct {
	Dir, Prefix string
	day         string
	f           *os.File
}

// Write appends line and a '\n' to the file of day.
func (a *Appender) Write(day string, line []byte) error {
	if a.f == nil || day != a.day {
		a.Close()
		f, err := os.OpenFile(filepath.Join(a.Dir, a.Prefix+"-"+day+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		a.f, a.day = f, day
	}
	buf := make([]byte, 0, len(line)+1)
	buf = append(append(buf, line...), '\n') // one write: never a line without its '\n' from us
	_, err := a.f.Write(buf)
	return err
}

// Sync puts the written lines on the disk; the caller does it before it
// saves the offsets of those lines, so a power cut does not lose lines whose
// offsets were saved (the X38e review).
func (a *Appender) Sync() error {
	if a.f == nil {
		return nil
	}
	return a.f.Sync()
}

// Close closes the open file.
func (a *Appender) Close() {
	if a.f != nil {
		a.f.Close()
		a.f = nil
	}
}
