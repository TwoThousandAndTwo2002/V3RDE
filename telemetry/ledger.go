package telemetry

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Ledger merges every sidecar spool (<SpoolRoot>/<service>/events-*.jsonl
// and samples-*.jsonl) into <Dir>/ledger-YYYYMMDD.jsonl and
// <Dir>/samples-YYYYMMDD.jsonl. The day is the spool file's day, so a line
// goes into the day it was shipped. The read positions are saved.
type Ledger struct {
	SpoolRoot, Dir string
	off            *Offsets
	ev, sm         Appender
	mu             sync.Mutex
	merged         int64
	now            func() time.Time // nil = time.Now (the tests set it)
}

// Open loads the saved positions.
func (l *Ledger) Open() error {
	l.ev = Appender{Dir: l.Dir, Prefix: "ledger"}
	l.sm = Appender{Dir: l.Dir, Prefix: "samples"}
	var err error
	l.off, err = LoadOffsets(filepath.Join(l.Dir, "offsets.json"))
	return err
}

// Close closes the ledger files.
func (l *Ledger) Close() {
	l.ev.Close()
	l.sm.Close()
}

// spoolDay is the YYYYMMDD of a spool file name.
var spoolDay = regexp.MustCompile(`-(\d{8})\.jsonl$`)

// Merge copies every new complete line of every spool; it returns how many.
func (l *Ledger) Merge() (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	dirs, _ := filepath.Glob(filepath.Join(l.SpoolRoot, "*"))
	for _, dir := range dirs {
		for _, path := range Files(dir) {
			name := filepath.Base(path)
			to := l.target(name)
			day := spoolDay.FindStringSubmatch(name)
			if to == nil || day == nil {
				continue
			}
			key := filepath.Base(dir) + "/" + name
			lines, next, err := ReadNew(path, l.off.Get(key))
			if err != nil {
				// the X38e review: one file that cannot be read stopped the
				// merge of every spool after it; now it is logged and skipped
				log.Printf("ledger: %s: %v", key, err)
				continue
			}
			for _, ln := range lines {
				if err := to.Write(day[1], ln.B); err != nil {
					return n, err
				}
				n++
			}
			if len(lines) > 0 {
				if err := to.Sync(); err != nil {
					return n, err
				}
				l.off.Set(key, next)
				if err := l.off.Save(); err != nil {
					return n, err
				}
			}
		}
	}
	l.merged += int64(n)
	return n, nil
}

// target is the ledger file of a spool file: events -> ledger, samples ->
// samples, any other file -> nil.
func (l *Ledger) target(name string) *Appender {
	switch {
	case strings.HasPrefix(name, "events-"):
		return &l.ev
	case strings.HasPrefix(name, "samples-"):
		return &l.sm
	}
	return nil
}

// Run merges every interval until stop is closed.
func (l *Ledger) Run(interval time.Duration, stop <-chan struct{}) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		_, _ = l.Merge()
		select {
		case <-stop:
			return
		case <-t.C:
		}
	}
}

// traceForm is the form of a trace id the route accepts.
var traceForm = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// Trace returns every ledger line of the given days (in that order) whose
// event has trace_id = id. When one of them names an analyze_request_id, a
// second pass adds the analyze lines with that request_id.
func (l *Ledger) Trace(id string, days []string) ([]json.RawMessage, error) {
	var out []json.RawMessage
	analyzeID := ""
	match := func(b []byte) bool { return bytes.Contains(b, []byte(`"trace_id":"`+id+`"`)) }
	for pass := 0; pass < 2; pass++ {
		if pass == 1 {
			if analyzeID == "" {
				break
			}
			req, svc := []byte(`"request_id":"`+analyzeID+`"`), []byte(`"service":"analyze"`)
			match = func(b []byte) bool { return bytes.Contains(b, req) && bytes.Contains(b, svc) }
		}
		for _, day := range days {
			l.scanDay(day, func(b []byte) {
				// a line that is not JSON (a torn line, or the last line while
				// Merge writes it) is skipped: one bad line made the whole
				// answer empty (the X38e review)
				if !match(b) || !json.Valid(b) {
					return
				}
				out = append(out, bytes.Clone(b))
				if pass == 0 && analyzeID == "" {
					analyzeID = analyzeRequestID(b)
				}
			})
		}
	}
	return out, nil
}

// scanDay calls f with each line of the ledger file of day (no file: no lines).
func (l *Ledger) scanDay(day string, f func([]byte)) {
	file, err := os.Open(filepath.Join(l.Dir, "ledger-"+day+".jsonl"))
	if err != nil {
		return
	}
	defer file.Close()
	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		f(sc.Bytes())
	}
}

// analyzeRequestID is line.analyze_request_id of a shipped line, or "".
func analyzeRequestID(b []byte) string {
	var s struct {
		Line struct {
			AnalyzeRequestID string `json:"analyze_request_id"`
		} `json:"line"`
	}
	if json.Unmarshal(b, &s) != nil {
		return ""
	}
	return s.Line.AnalyzeRequestID
}

// Handler is GET /healthz and GET /v1/trace/{id} (today and yesterday, UTC).
// It is internal only (no Caddy route).
func (l *Ledger) Handler(build string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		l.mu.Lock()
		n := l.merged
		l.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "service": "ledger", "build": build, "merged": n})
	})
	mux.HandleFunc("GET /v1/trace/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		w.Header().Set("Content-Type", "application/json")
		if !traceForm.MatchString(id) {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "bad trace id"})
			return
		}
		clock := l.now
		if clock == nil {
			clock = time.Now
		}
		now := clock().UTC()
		days := []string{now.Format("20060102"), now.AddDate(0, 0, -1).Format("20060102")}
		lines, err := l.Trace(id, days)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"trace_id": id, "events": lines}); err != nil {
			log.Printf("trace %s: %v", id, err)
		}
	})
	return mux
}
