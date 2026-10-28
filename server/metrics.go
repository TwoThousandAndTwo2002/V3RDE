package analyze

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Line is one metrics record per request. It never holds image bytes, the
// Meta key or the bearer token.
type Line struct {
	RequestID       string `json:"request_id"`
	TS              string `json:"ts"`
	RemoteIP        string `json:"remote_ip"`
	ForwardedIP     string `json:"forwarded_ip,omitempty"`
	Method          string `json:"method"`
	Path            string `json:"path"`
	Status          int    `json:"status"`
	ImageBytes      int    `json:"image_bytes"`
	Mime            string `json:"mime,omitempty"`
	LatencyMS       int64  `json:"latency_ms"`
	UpstreamMS      int64  `json:"upstream_ms"`
	UpstreamStatus  int    `json:"upstream_status"`
	UpstreamID      string `json:"upstream_id,omitempty"`
	InputTokens     int    `json:"input_tokens"`
	OutputTokens    int    `json:"output_tokens"`
	ReasoningTokens int    `json:"reasoning_tokens"`
	SchemaValid     bool   `json:"schema_valid"`
	Model           string `json:"model"`
	Name            string `json:"name,omitempty"`
	Error           string `json:"error,omitempty"`
}

// Recorder appends one JSON line per request to <Dir>/metrics-YYYYMMDD.jsonl
// (the UTC day). Each line is written with one write call and synced to the
// disk before Write returns, so an answered request is never lost in a crash.
type Recorder struct {
	Dir string
	mu  sync.Mutex
	now func() time.Time
}

// NewRecorder writes into dir (made when the first line comes).
func NewRecorder(dir string) *Recorder { return &Recorder{Dir: dir, now: time.Now} }

// Path is the file of today.
func (rc *Recorder) Path() string {
	return filepath.Join(rc.Dir, "metrics-"+rc.now().UTC().Format("20060102")+".jsonl")
}

// Write appends l. The caller logs an error; the request does not fail.
func (rc *Recorder) Write(l Line) error {
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if err := os.MkdirAll(rc.Dir, 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(rc.Path(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
