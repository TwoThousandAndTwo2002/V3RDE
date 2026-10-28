package barcode

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// Event is one line of the barcode service's log: enough to replay the
// lookup (the code as scanned, its canonical key, the catalog by sha256 and
// the code by build). It is the shape the telemetry ledger reads.
type Event struct {
	TS            string  `json:"ts"`
	Service       string  `json:"service"`
	Step          string  `json:"step"`
	TraceID       string  `json:"trace_id"`
	RequestID     string  `json:"request_id"`
	Status        int     `json:"status"`
	Outcome       string  `json:"outcome"`
	Code          string  `json:"code"`
	GTIN14        string  `json:"gtin14,omitempty"`
	Admissible    bool    `json:"admissible"`
	Why           string  `json:"why,omitempty"`
	LookupMS      float64 `json:"lookup_ms"`
	TotalMS       float64 `json:"total_ms"`
	CatalogSHA256 string  `json:"catalog_sha256"`
	CatalogRows   int     `json:"catalog_rows"`
	Build         string  `json:"build"`
	Error         string  `json:"error,omitempty"`
}

// Recorder appends events to <Dir>/events-YYYYMMDD.jsonl (the UTC day)
// from its own goroutine. Record never blocks a request: when the buffer is
// full, the event is dropped and counted, so the log cannot add time. The
// folder is a volume, so the log stays after a restart; the sidecar sends it.
type Recorder struct {
	Dir     string
	ch      chan Event
	dropped atomic.Int64
	written atomic.Int64
	done    chan struct{}
	now     func() time.Time
}

// NewRecorder starts the writer with a buffer of buf events.
func NewRecorder(dir string, buf int) *Recorder {
	r := &Recorder{Dir: dir, ch: make(chan Event, buf), done: make(chan struct{}), now: time.Now}
	go r.loop()
	return r
}

// Record queues e; a nil recorder does nothing.
func (r *Recorder) Record(e Event) {
	if r == nil {
		return
	}
	select {
	case r.ch <- e:
	default:
		r.dropped.Add(1)
	}
}

// Dropped is the number of events not written.
func (r *Recorder) Dropped() int64 { return r.dropped.Load() }

// Written is the number of events written.
func (r *Recorder) Written() int64 { return r.written.Load() }

// Close writes what is queued and stops the writer.
func (r *Recorder) Close() {
	close(r.ch)
	<-r.done
}

func (r *Recorder) loop() {
	defer close(r.done)
	var f *os.File
	day := ""
	defer func() {
		if f != nil {
			f.Close()
		}
	}()
	for e := range r.ch {
		if today := r.now().UTC().Format("20060102"); f == nil || today != day {
			if f != nil {
				f.Close()
				f = nil
			}
			nf, err := os.OpenFile(filepath.Join(r.Dir, "events-"+today+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				log.Printf("events: %v", err)
				r.dropped.Add(1)
				continue
			}
			f, day = nf, today
		}
		line, _ := json.Marshal(e)
		if _, err := f.Write(append(line, '\n')); err != nil {
			log.Printf("events write: %v", err)
			r.dropped.Add(1)
			continue
		}
		r.written.Add(1)
	}
}
