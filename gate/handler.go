package gate

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Embedder is the model: LiteRT in the service, a fake in the tests.
type Embedder interface {
	Embed(x []float32) ([]float32, error)
	InputShape() []int
	OutputShape() []int
}

// Config is everything the routes need; cmd/gate fills it from flags.
type Config struct {
	Model       Embedder // nil = the model did not load: every call is UNAVAILABLE (fails closed)
	LoadError   string   // why Model is nil
	Head        *Head
	ModelSHA256 string
	HeadSHA256  string
	Build       string
	Recorder    *Recorder
	MaxImage    int64   // bytes; 0 = 5 MiB
	Queue       int     // the most calls that wait for the model; more = 503 busy; 0 = 8
	Shadow      *Shadow // a second gate, scored after the answer (entry 3); nil = none
}

// Shadow is a second gate. It scores the same decoded photo AFTER the answer
// is sent, on its own slot (one interpreter x one thread), so it never
// changes the answer or its time. It writes its own events line (step
// "gate_shadow") with the same request and trace ids and the caller's
// verdict. When its slot is busy, the photo is skipped and counted.
type Shadow struct {
	Model       Embedder
	Head        *Head
	ModelSHA256 string
	HeadSHA256  string
	LoadError   string
	wg          sync.WaitGroup
}

// Wait waits until every shadow run that started has ended (tests, stop).
func (s *Shadow) Wait() {
	if s != nil {
		s.wg.Wait()
	}
}

// Response is the body of POST /v1/gate. Every verdict is a 200; the caller
// must treat UNAVAILABLE as a refusal.
type Response struct {
	RequestID string  `json:"request_id"`
	TraceID   string  `json:"trace_id"`
	Verdict   string  `json:"verdict"`
	Food      bool    `json:"food"`
	P         float64 `json:"p"`
	Threshold float64 `json:"threshold"`
	Why       string  `json:"why,omitempty"`
	ImageSHA  string  `json:"image_sha256"`
	MS        Timings `json:"ms"`
}

// Timings are the milliseconds of each step.
type Timings struct {
	Queue  float64 `json:"queue"`
	Decode float64 `json:"decode"`
	Prep   float64 `json:"prep"`
	Invoke float64 `json:"invoke"`
	Head   float64 `json:"head"`
	Total  float64 `json:"total"`
}

type errorBody struct {
	RequestID string `json:"request_id"`
	TraceID   string `json:"trace_id"`
	Error     string `json:"error"`
}

var traceForm = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// gate is the state of the routes.
type gate struct {
	Config
	slot          chan struct{} // the model: one call at a time
	waiting       chan struct{} // the queue in front of the slot
	shadowSlot    chan struct{}
	served, busy  atomic.Int64
	shadowRuns    atomic.Int64
	shadowSkipped atomic.Int64
}

// New makes the routes: GET /healthz and POST /v1/gate. They are internal
// (no Caddy route); the inference service is the caller.
func New(c Config) http.Handler {
	if c.MaxImage == 0 {
		c.MaxImage = 5 << 20
	}
	if c.Queue == 0 {
		c.Queue = 8
	}
	g := &gate{Config: c, slot: make(chan struct{}, 1), waiting: make(chan struct{}, c.Queue), shadowSlot: make(chan struct{}, 1)}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { g.health(w) })
	mux.HandleFunc("/v1/gate", g.score)
	return mux
}

// health is 200 when the model is loaded, else 503 with the load error.
func (g *gate) health(w http.ResponseWriter) {
	body := map[string]any{"ok": g.Model != nil, "service": "gate", "build": g.Build,
		"model_sha256": g.ModelSHA256, "head_sha256": g.HeadSHA256, "served": g.served.Load(), "busy": g.busy.Load(),
		"jpeg_decoder": JPEGDecoder}
	if g.Head != nil {
		body["threshold"], body["input_side"] = g.Head.Threshold, g.Head.InputSide
	}
	if g.LoadError != "" {
		body["error"] = g.LoadError
	}
	if g.Recorder != nil {
		body["events_written"], body["events_dropped"] = g.Recorder.Written(), g.Recorder.Dropped()
	}
	if s := g.Shadow; s != nil {
		sh := map[string]any{"ok": s.Model != nil && s.Head != nil, "model_sha256": s.ModelSHA256,
			"head_sha256": s.HeadSHA256, "runs": g.shadowRuns.Load(), "skipped": g.shadowSkipped.Load()}
		if s.Head != nil {
			sh["threshold"], sh["input_side"], sh["resample"] = s.Head.Threshold, s.Head.InputSide, s.Head.Spec().Resample
		}
		if s.LoadError != "" {
			sh["error"] = s.LoadError
		}
		body["shadow"] = sh
	}
	status := http.StatusOK
	if g.Model == nil {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, body)
}

// score is POST /v1/gate. The order: the method, the photo, a loaded gate,
// the decode, the preprocess, the queue, the model, the head. Each call
// writes one events line.
func (g *gate) score(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	id := newID()
	trace := r.Header.Get("X-Trace-Id")
	if !traceForm.MatchString(trace) {
		trace = id
	}
	w.Header().Set("X-Request-Id", id)
	w.Header().Set("X-Trace-Id", trace)
	ev := Event{TS: start.UTC().Format(time.RFC3339Nano), Service: "gate", Step: "gate", TraceID: trace,
		RequestID: id, ModelSHA256: g.ModelSHA256, HeadSHA256: g.HeadSHA256, Build: g.Build, P: -1}
	if g.Head != nil {
		ev.Threshold = g.Head.Threshold
	}
	fail := func(status int, msg string) {
		ev.Status, ev.Error, ev.TotalMS = status, msg, ms(time.Since(start))
		g.Recorder.Record(ev)
		writeJSON(w, status, errorBody{RequestID: id, TraceID: trace, Error: msg})
	}
	if r.Method != http.MethodPost {
		fail(http.StatusMethodNotAllowed, "POST only")
		return
	}
	img, status, msg := readImage(r, g.MaxImage)
	ev.ImageBytes = len(img)
	if status != 0 {
		fail(status, msg)
		return
	}
	sum := sha256.Sum256(img)
	ev.ImageSHA256 = hex.EncodeToString(sum[:])
	res := Response{RequestID: id, TraceID: trace, P: -1, ImageSHA: ev.ImageSHA256}
	if g.Head != nil {
		res.Threshold = g.Head.Threshold
	}
	answer := func() {
		res.MS.Total = ms(time.Since(start))
		ev.Status, ev.Verdict, ev.P, ev.Why = http.StatusOK, res.Verdict, res.P, res.Why
		ev.QueueMS, ev.DecodeMS, ev.PrepMS, ev.InvokeMS, ev.HeadMS, ev.TotalMS =
			res.MS.Queue, res.MS.Decode, res.MS.Prep, res.MS.Invoke, res.MS.Head, res.MS.Total
		g.Recorder.Record(ev)
		g.served.Add(1)
		writeJSON(w, http.StatusOK, res)
	}
	if g.Model == nil || g.Head == nil {
		res.Verdict, res.Why = Unavailable, "gate not loaded: "+g.LoadError
		answer()
		return
	}

	// X51 review (security F1): the slot is taken before the decode, so one photo at a time is decoded and held
	// (a 50-megapixel PNG of 16 bits takes about 750 MB while it is decoded; two at once were more than the gate's
	// 1 GB). The slot is given back when the request returns (X51 re-check: so its decoded photo is no longer held
	// when the next decode starts), or at once when the decode fails. The queue in front of the slot is as before.
	select {
	case g.waiting <- struct{}{}:
	default:
		g.busy.Add(1)
		fail(http.StatusServiceUnavailable, "busy: gate queue full")
		return
	}
	t := time.Now()
	g.slot <- struct{}{}
	<-g.waiting
	res.MS.Queue = ms(time.Since(t))
	held := true
	free := func() {
		if held {
			held = false
			<-g.slot
		}
	}
	defer free()

	t = time.Now()
	rgb, wd, ht, format, err := Decode(img)
	res.MS.Decode = ms(time.Since(t))
	ev.Format, ev.Width, ev.Height = format, wd, ht
	if err != nil {
		free()
		fail(http.StatusUnprocessableEntity, err.Error())
		return
	}
	// after the answer is written: the shadow scores the same decoded photo
	defer func() { g.shadow(res.Verdict, ev, rgb, wd, ht, format) }()

	t = time.Now()
	x := TensorFor(rgb, wd, ht, g.Head.InputSide, g.Head.Spec())
	res.MS.Prep = ms(time.Since(t))

	t = time.Now()
	v, err := g.Model.Embed(x)
	res.MS.Invoke = ms(time.Since(t))
	if err != nil {
		res.Verdict, res.Why = Unavailable, err.Error()
		answer()
		return
	}
	t = time.Now()
	label, p, norm, why := g.Head.Verdict(v)
	res.MS.Head = ms(time.Since(t))
	res.Verdict, res.Food, res.P, res.Why = label, label == Food, p, why
	ev.Norm = norm
	answer()
}

// shadow starts the shadow run when there is a shadow, the caller got a
// verdict and the shadow slot is free; a busy slot is counted as skipped.
func (g *gate) shadow(primary string, ev Event, rgb []uint8, wd, ht int, format string) {
	s := g.Shadow
	if s == nil || s.Model == nil || s.Head == nil {
		return
	}
	select {
	case g.shadowSlot <- struct{}{}:
	default:
		g.shadowSkipped.Add(1)
		return
	}
	if primary == "" { // the caller got an error, not a verdict
		<-g.shadowSlot
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { <-g.shadowSlot }()
		start := time.Now()
		sev := Event{TS: start.UTC().Format(time.RFC3339Nano), Service: "gate", Step: "gate_shadow",
			TraceID: ev.TraceID, RequestID: ev.RequestID, Status: http.StatusOK, P: -1, Threshold: s.Head.Threshold,
			ImageSHA256: ev.ImageSHA256, ImageBytes: ev.ImageBytes, Format: format, Width: wd, Height: ht,
			ModelSHA256: s.ModelSHA256, HeadSHA256: s.HeadSHA256, Build: g.Build, PrimaryVerdict: primary}
		t := time.Now()
		x := TensorFor(rgb, wd, ht, s.Head.InputSide, s.Head.Spec())
		sev.PrepMS = ms(time.Since(t))
		t = time.Now()
		v, err := s.Model.Embed(x)
		sev.InvokeMS = ms(time.Since(t))
		if err != nil {
			sev.Verdict, sev.Why = Unavailable, err.Error()
		} else {
			t = time.Now()
			sev.Verdict, sev.P, sev.Norm, sev.Why = s.Head.Verdict(v)
			sev.HeadMS = ms(time.Since(t))
		}
		sev.TotalMS = ms(time.Since(start))
		g.Recorder.Record(sev)
		g.shadowRuns.Add(1)
	}()
}

// readImage reads the photo: multipart/form-data (field "image"), JSON
// {"image_base64"} or a raw image/* body. The body is capped before any
// decoding (https://pkg.go.dev/net/http#MaxBytesReader).
func readImage(r *http.Request, max int64) ([]byte, int, string) {
	r.Body = http.MaxBytesReader(nil, r.Body, max*4/3+4096)
	ct := r.Header.Get("Content-Type")
	var img []byte
	switch {
	case strings.HasPrefix(ct, "multipart/form-data"):
		if err := r.ParseMultipartForm(max); err != nil {
			return nil, http.StatusRequestEntityTooLarge, "image too large or bad multipart"
		}
		f, _, err := r.FormFile("image")
		if err != nil {
			return nil, http.StatusBadRequest, "multipart field 'image' missing"
		}
		defer f.Close()
		if img, err = io.ReadAll(io.LimitReader(f, max+1)); err != nil {
			return nil, http.StatusBadRequest, "image unreadable"
		}
	case strings.HasPrefix(ct, "application/json"):
		var body struct {
			ImageBase64 string `json:"image_base64"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return nil, http.StatusBadRequest, "bad JSON body"
		}
		var err error
		if img, err = base64.StdEncoding.DecodeString(strings.TrimSpace(body.ImageBase64)); err != nil {
			return nil, http.StatusBadRequest, "image_base64 is not base64"
		}
	case strings.HasPrefix(ct, "image/"):
		var err error
		if img, err = io.ReadAll(io.LimitReader(r.Body, max+1)); err != nil {
			return nil, http.StatusRequestEntityTooLarge, "image too large"
		}
	default:
		return nil, http.StatusUnsupportedMediaType, "send multipart/form-data, application/json or image/*"
	}
	if int64(len(img)) > max {
		return nil, http.StatusRequestEntityTooLarge, "image too large"
	}
	if len(img) == 0 {
		return nil, http.StatusBadRequest, "empty image"
	}
	return img, 0, ""
}
