package gate

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// Event is one line of the gate's log: enough to replay the call (the photo
// by image_sha256 in the photo store, the code by build, the model and the
// head by their sha256), and never image bytes.
type Event struct {
	TS          string  `json:"ts"`
	Service     string  `json:"service"`
	Step        string  `json:"step"`
	TraceID     string  `json:"trace_id"`
	RequestID   string  `json:"request_id"`
	Status      int     `json:"status"`
	Verdict     string  `json:"verdict,omitempty"`
	P           float64 `json:"p"`
	Threshold   float64 `json:"threshold"`
	Norm        float64 `json:"norm,omitempty"`
	Why         string  `json:"why,omitempty"`
	ImageSHA256 string  `json:"image_sha256,omitempty"`
	ImageBytes  int     `json:"image_bytes"`
	Format      string  `json:"format,omitempty"`
	Width       int     `json:"width,omitempty"`
	Height      int     `json:"height,omitempty"`
	QueueMS     float64 `json:"queue_ms"`
	DecodeMS    float64 `json:"decode_ms"`
	PrepMS      float64 `json:"prep_ms"`
	InvokeMS    float64 `json:"invoke_ms"`
	HeadMS      float64 `json:"head_ms"`
	TotalMS     float64 `json:"total_ms"`
	ModelSHA256 string  `json:"model_sha256"`
	HeadSHA256  string  `json:"head_sha256"`
	Build       string  `json:"build"`
	Error       string  `json:"error,omitempty"`
	// PrimaryVerdict is set on a shadow line (step "gate_shadow"): the
	// verdict the caller got.
	PrimaryVerdict string `json:"primary_verdict,omitempty"`
}

// Recorder appends events to <Dir>/events-YYYYMMDD.jsonl (the UTC day) from
// its own goroutine. Record never blocks a request: when the buffer is full
// the event is dropped and counted. The folder is a volume; the sidecar
// ships it.
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
