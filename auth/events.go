package auth

// The events log: one JSON line per request, in the shape the telemetry
// ledger reads (the same as barcode, gate and the other services), so the
// ledger can join this service to a scan by trace_id and to CPU and memory
// by time. A line has the route PATTERN (no ids, no query), the method, the
// status, the bytes, the time, the caller's address and the trace id. It
// never has a body, a password, an email or a cookie.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

// Event is one request.
type Event struct {
	TS          string  `json:"ts"`
	Service     string  `json:"service"`
	Step        string  `json:"step"`
	TraceID     string  `json:"trace_id"`
	RequestID   string  `json:"request_id"`
	Method      string  `json:"method"`
	Route       string  `json:"route"`
	Status      int     `json:"status"`
	Bytes       int64   `json:"bytes"`
	RemoteIP    string  `json:"remote_ip"`
	ForwardedIP string  `json:"forwarded_ip,omitempty"`
	TotalMS     float64 `json:"total_ms"`
	Build       string  `json:"build"`
}

// Recorder appends events to <Dir>/events-YYYYMMDD.jsonl (the UTC day)
// from its own goroutine. Record never blocks: when the buffer is full, the
// event is dropped and counted.
type Recorder struct {
	Dir     string
	ch      chan Event
	dropped atomic.Int64
	written atomic.Int64
	done    chan struct{}
}

// NewRecorder starts the writer with a buffer of buf events.
func NewRecorder(dir string, buf int) *Recorder {
	r := &Recorder{Dir: dir, ch: make(chan Event, buf), done: make(chan struct{})}
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

// Dropped is the number of events not written (a full buffer or a failed write).
func (r *Recorder) Dropped() int64 { return r.dropped.Load() }

// Written is the number of events written.
func (r *Recorder) Written() int64 { return r.written.Load() }

// Close writes the queued events and stops the writer.
func (r *Recorder) Close() { close(r.ch); <-r.done }

// dayFile is the open file of one UTC day.
type dayFile struct {
	day string
	f   *os.File
}

// get opens the file of today when the day changed (or no file is open).
func (d *dayFile) get(dir string) (*os.File, error) {
	today := time.Now().UTC().Format("20060102")
	if d.f != nil && d.day == today {
		return d.f, nil
	}
	d.close()
	f, err := os.OpenFile(filepath.Join(dir, "events-"+today+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	d.day, d.f = today, f
	return f, nil
}

func (d *dayFile) close() {
	if d.f != nil {
		d.f.Close()
		d.f = nil
	}
}

func (r *Recorder) loop() {
	defer close(r.done)
	var df dayFile
	defer df.close()
	for e := range r.ch {
		f, err := df.get(r.Dir)
		if err != nil {
			log.Printf("events: %v", err)
			r.dropped.Add(1)
			continue
		}
		line, _ := json.Marshal(e)
		if _, err := f.Write(append(line, '\n')); err != nil {
			r.dropped.Add(1)
			continue
		}
		r.written.Add(1)
	}
}

// traceID is the form of a caller's X-Trace-Id that is kept.
var traceID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// counter keeps the status and the body size of a response.
type counter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (c *counter) WriteHeader(code int) {
	if c.status == 0 {
		c.status = code
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *counter) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	n, err := c.ResponseWriter.Write(b)
	c.bytes += int64(n)
	return n, err
}

// Logged wraps the service's routes. The trace id is the caller's
// X-Trace-Id when its form is good, else a new id. The route is the
// ServeMux pattern that matched (https://pkg.go.dev/net/http#Request,
// Request.Pattern), so ids in paths and queries never reach the log.
func Logged(service, build string, rec *Recorder, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var b [16]byte
		_, _ = rand.Read(b[:])
		id := hex.EncodeToString(b[:])
		trace := r.Header.Get("X-Trace-Id")
		if !traceID.MatchString(trace) {
			trace = id
		}
		cw := &counter{ResponseWriter: w}
		h.ServeHTTP(cw, r)
		if cw.status == 0 {
			cw.status = http.StatusOK
		}
		remote := r.RemoteAddr
		if host, _, err := net.SplitHostPort(remote); err == nil {
			remote = host
		}
		first, _, _ := strings.Cut(r.Header.Get("X-Forwarded-For"), ",")
		route := r.Pattern
		if route == "" {
			route = "(unmatched)"
		}
		rec.Record(Event{TS: start.UTC().Format(time.RFC3339Nano), Service: service, Step: "request",
			TraceID: trace, RequestID: id, Method: r.Method, Route: route, Status: cw.status, Bytes: cw.bytes,
			RemoteIP: remote, ForwardedIP: strings.TrimSpace(first),
			TotalMS: float64(time.Since(start).Microseconds()) / 1000, Build: build})
	})
}
