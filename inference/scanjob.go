package inference

// X44b (F1, T9 2026-10-29: "no visible waits"): the scan as a job.
//
//	POST /api/analyze?async=1 (or /v1/analyze?async=1): the same checks as
//	     the 200 route up to the photo (caller, rate limit, image), then
//	     202 {request_id, scan_id, status:"running"}; the rest of the scan
//	     runs in the service.
//	GET  /api/analyze/scan/{id} (or /v1/...): the job, to its owner only:
//	     {scan_id, request_id, status, stage, boxes, named, http_status, answer}
//	     status running | done | failed; stage checking | finding | naming |
//	     done; boxes: the boxes being named (0-1000 grid, box n = boxes[n-1]);
//	     named: [{n, name}] as chunks finish; answer: the body the 200 route
//	     gives (status done) or its error body (failed, http_status = its code).
//
// Why polling and not a stream: the public name is an API Gateway HTTP API,
// and API Gateway streams a response only for REST APIs (docs/sources/X44b.md),
// so the app asks again. Each poll is a short request, far under the 30 s limit.
//
// X48c: a held poll. GET .../scan/{id}?since=V&wait=MS answers at once when
// the job's version is not V (or the job ended); else it holds the request
// until the job changes or MS ends (at most ScanHoldMax). The app sends the
// version it has, so it sees each change as it happens, with no sleep between
// polls. With no wait the GET answers at once, as before.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Scan job states and stages.
const (
	ScanRunning = "running"
	ScanDone    = "done"
	ScanFailed  = "failed"
)

// ScanKeep is how long a finished job stays readable; ScanMaxJobs is how many
// jobs the service holds (running and finished).
const (
	ScanKeep    = 10 * time.Minute
	ScanMaxJobs = 64
	// ScanHoldMax is the longest held poll (X48c), far under the 30 s limit
	// of the API Gateway integration.
	ScanHoldMax = 10 * time.Second
)

// NamedBox is one named box of a running scan.
type NamedBox struct {
	N    int    `json:"n"`
	Name string `json:"name"`
}

// ScanJob is the GET answer.
type ScanJob struct {
	ScanID     string          `json:"scan_id"`
	RequestID  string          `json:"request_id"`
	Status     string          `json:"status"`
	Stage      string          `json:"stage"`
	Boxes      []Norm          `json:"boxes"`
	Named      []NamedBox      `json:"named"`
	HTTPStatus int             `json:"http_status,omitempty"`
	Answer     json.RawMessage `json:"answer,omitempty"`
	ElapsedMS  int64           `json:"elapsed_ms"`
	// Version goes up by one at each change (stage, boxes, a name, the end).
	Version int           `json:"version"`
	changed chan struct{} // closed at each change (X48c)
	kind    string
	userID  string
	sha     string // X49r: the photo's sha256 and the barcode (same())
	started time.Time
	ended   time.Time
	named   map[int]string
}

// scanJobs holds the jobs of one service.
type scanJobs struct {
	mu   sync.Mutex
	jobs map[string]*ScanJob
	now  func() time.Time
}

// ScanSameAfter is how long after a good end the same photo of the same user
// gets that job again (X49r): the app's tries are 2 s apart (3 at most), so a
// scan of the same photo later is a new scan (with the already-listed check).
const ScanSameAfter = 10 * time.Second

// same is the job of this user for the same photo (X49r): one that runs, or
// one that ended well less than ScanSameAfter ago. The same kind of caller
// (the scan route answers only the kind that started the job). The app sends a photo again
// when the network failed, and the first request can have reached the service;
// then the scan runs and is paid once. A failed job is not given again, so
// "Try again" after a failure starts a new scan. "" user or photo: none.
func (s *scanJobs) same(kind, userID, sha string) *ScanJob {
	if userID == "" || sha == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	var best *ScanJob
	for _, j := range s.jobs {
		if j.kind != kind || j.userID != userID || j.sha != sha {
			continue
		}
		if j.Status == ScanRunning || (j.Status == ScanDone && now.Sub(j.ended) < ScanSameAfter) {
			if best == nil || j.started.After(best.started) {
				best = j
			}
		}
	}
	return best
}

// start adds a job; nil when ScanMaxJobs jobs are still running or kept.
func (s *scanJobs) start(id, requestID, kind, userID string) *ScanJob {
	return s.startFor(id, requestID, kind, userID, "")
}

// startFor is start with the photo's sha256 (X49r: same()).
func (s *scanJobs) startFor(id, requestID, kind, userID, sha string) *ScanJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.jobs == nil {
		s.jobs = map[string]*ScanJob{}
	}
	now := s.now()
	var ended []*ScanJob
	for k, j := range s.jobs {
		if !j.ended.IsZero() {
			if now.Sub(j.ended) > ScanKeep {
				delete(s.jobs, k)
				continue
			}
			ended = append(ended, j)
		}
	}
	if len(s.jobs) >= ScanMaxJobs { // full: drop the oldest finished jobs first
		sort.Slice(ended, func(a, b int) bool { return ended[a].ended.Before(ended[b].ended) })
		for _, j := range ended {
			if len(s.jobs) < ScanMaxJobs {
				break
			}
			delete(s.jobs, j.ScanID)
		}
		if len(s.jobs) >= ScanMaxJobs {
			return nil
		}
	}
	j := &ScanJob{ScanID: id, RequestID: requestID, Status: ScanRunning, Stage: "checking", Boxes: []Norm{},
		Named: []NamedBox{}, kind: kind, userID: userID, sha: sha, started: now, named: map[int]string{},
		changed: make(chan struct{})}
	s.jobs[id] = j
	return j
}

func (s *scanJobs) stage(j *ScanJob, stage string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j.Status == ScanRunning && j.Stage != stage {
		j.Stage = stage
		j.bump()
	}
}

// bump marks a change: the version goes up and the held polls answer (s.mu held).
func (j *ScanJob) bump() {
	j.Version++
	close(j.changed)
	j.changed = make(chan struct{})
}

// progress takes the engine's reports (any goroutine).
func (s *scanJobs) progress(j *ScanJob, p Progress) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j.Status != ScanRunning {
		return
	}
	moved := false
	if p.Stage != "" && p.Stage != j.Stage {
		j.Stage, moved = p.Stage, true
	}
	if p.Boxes != nil {
		j.Boxes, moved = append([]Norm(nil), p.Boxes...), true
	}
	for n, name := range p.Named {
		if name != "" && j.named[n] != name {
			j.named[n], moved = name, true
		}
	}
	if moved {
		j.bump()
	}
}

// finish stores the answer the 200 route would have written.
func (s *scanJobs) finish(j *ScanJob, status int, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j.HTTPStatus, j.Answer = status, json.RawMessage(bytes.TrimSpace(body))
	if !json.Valid(j.Answer) {
		j.Answer = nil
	}
	j.Status, j.Stage = ScanFailed, "done"
	if status == http.StatusOK {
		j.Status = ScanDone
	}
	j.ended = s.now()
	j.bump()
}

// hold is get, held until the job's version is not since, the job ended,
// hold ended or done is closed (the caller went away). hold <= 0: at once.
func (s *scanJobs) hold(done <-chan struct{}, id, kind, userID string, since int, hold time.Duration) (ScanJob, bool) {
	if hold > ScanHoldMax {
		hold = ScanHoldMax
	}
	var t <-chan time.Time
	if hold > 0 {
		tm := time.NewTimer(hold)
		defer tm.Stop()
		t = tm.C
	}
	for {
		s.mu.Lock()
		j, ok := s.jobs[id]
		if !ok || j.kind != kind || j.userID != userID {
			s.mu.Unlock()
			return ScanJob{}, false
		}
		if hold <= 0 || j.Version != since || !j.ended.IsZero() {
			c := s.copyOf(j)
			s.mu.Unlock()
			return c, true
		}
		ch := j.changed
		s.mu.Unlock()
		select {
		case <-ch:
		case <-done:
			hold = 0
		case <-t:
			hold = 0
		}
	}
}

// get is a copy of job id when kind and userID own it.
func (s *scanJobs) get(id, kind, userID string) (ScanJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok || j.kind != kind || j.userID != userID {
		return ScanJob{}, false
	}
	return s.copyOf(j), true
}

// copyOf is a copy of j for the answer (s.mu held).
func (s *scanJobs) copyOf(j *ScanJob) ScanJob {
	c := *j
	c.changed = nil
	c.Boxes = append([]Norm{}, j.Boxes...)
	c.Named = []NamedBox{}
	for n, name := range j.named {
		c.Named = append(c.Named, NamedBox{N: n, Name: name})
	}
	sort.Slice(c.Named, func(a, b int) bool { return c.Named[a].N < c.Named[b].N })
	end := j.ended
	if end.IsZero() {
		end = s.now()
	}
	c.ElapsedMS = end.Sub(j.started).Milliseconds()
	return c
}

// capture is the ResponseWriter of a scan that runs as a job.
type capture struct {
	h      http.Header
	status int
	body   bytes.Buffer
}

func newCapture() *capture { return &capture{h: http.Header{}} }

func (c *capture) Header() http.Header { return c.h }
func (c *capture) WriteHeader(s int) {
	if c.status == 0 {
		c.status = s
	}
}
func (c *capture) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	return c.body.Write(b)
}
