package barcode

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

// The outcomes of one lookup.
const (
	Hit         = "HIT"         // in the catalog: the listing can be filled
	Miss        = "MISS"        // a valid code the catalog does not hold
	Invalid     = "INVALID"     // not a GTIN-8/12/13/14, or its check digit fails
	Unavailable = "UNAVAILABLE" // the catalog is not loaded or failed (fails closed)
)

// Config is everything the routes need; cmd/barcode fills it from flags.
type Config struct {
	Store         Store // nil = the catalog did not load: every lookup is UNAVAILABLE
	LoadError     string
	CatalogSHA256 string
	Build         string
	Recorder      *Recorder
}

// Response is the body of GET /v1/barcode/{code}. Every outcome is a 200:
// MISS and INVALID are answers the caller acts on (it goes on to the gate).
type Response struct {
	RequestID  string            `json:"request_id"`
	TraceID    string            `json:"trace_id"`
	Outcome    string            `json:"outcome"`
	Found      bool              `json:"found"`
	GTIN14     string            `json:"gtin14,omitempty"`
	Admissible bool              `json:"admissible"`
	Product    *Product          `json:"product"`
	Provenance map[string]string `json:"provenance,omitempty"`
	Why        string            `json:"why,omitempty"`
	MS         struct {
		Lookup float64 `json:"lookup"`
		Total  float64 `json:"total"`
	} `json:"ms"`
}

// maxCode is the longest code text kept (a GTIN has at most 14 digits).
const maxCode = 64

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

// counts is the number of answers per outcome since the start.
type counts struct{ hit, miss, invalid, unavailable atomic.Int64 }

func (c *counts) add(outcome string) {
	switch outcome {
	case Hit:
		c.hit.Add(1)
	case Miss:
		c.miss.Add(1)
	case Invalid:
		c.invalid.Add(1)
	case Unavailable:
		c.unavailable.Add(1)
	}
}

// New makes the routes: GET /healthz and GET /v1/barcode/{code}. They are
// internal (no Caddy route); the inference service is the caller.
func New(c Config) http.Handler {
	n := &counts{}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { c.health(w, n) })
	mux.HandleFunc("/v1/barcode/", func(w http.ResponseWriter, r *http.Request) { c.lookup(w, r, n) })
	return mux
}

// health is 200 with the catalog, or 503 with the load error.
func (c Config) health(w http.ResponseWriter, n *counts) {
	body := map[string]any{"ok": c.Store != nil, "service": "barcode", "build": c.Build,
		"catalog_sha256": c.CatalogSHA256, "hit": n.hit.Load(), "miss": n.miss.Load(),
		"invalid": n.invalid.Load(), "unavailable": n.unavailable.Load()}
	status := http.StatusOK
	if c.Store == nil {
		status = http.StatusServiceUnavailable
		body["error"] = c.LoadError
	} else {
		body["catalog_rows"] = c.Store.Rows()
	}
	if c.Recorder != nil {
		body["events_written"], body["events_dropped"] = c.Recorder.Written(), c.Recorder.Dropped()
	}
	writeJSON(w, status, body)
}

// lookup is GET /v1/barcode/{code}. Each answer writes one events line.
func (c Config) lookup(w http.ResponseWriter, r *http.Request, n *counts) {
	start := time.Now()
	id := newID()
	trace := r.Header.Get("X-Trace-Id")
	if !traceForm.MatchString(trace) {
		trace = id
	}
	w.Header().Set("X-Request-Id", id)
	w.Header().Set("X-Trace-Id", trace)
	raw := strings.TrimPrefix(r.URL.Path, "/v1/barcode/")
	if len(raw) > maxCode {
		raw = raw[:maxCode]
	}
	ev := Event{TS: start.UTC().Format(time.RFC3339Nano), Service: "barcode", Step: "barcode", TraceID: trace,
		RequestID: id, Code: Normalize(raw), CatalogSHA256: c.CatalogSHA256, Build: c.Build}
	if c.Store != nil {
		ev.CatalogRows = c.Store.Rows()
	}
	res := Response{RequestID: id, TraceID: trace}
	answer := func(status int) {
		res.MS.Total = ms(time.Since(start))
		ev.Status, ev.Outcome, ev.GTIN14, ev.Admissible, ev.Why = status, res.Outcome, res.GTIN14, res.Admissible, res.Why
		ev.LookupMS, ev.TotalMS = res.MS.Lookup, res.MS.Total
		c.Recorder.Record(ev)
		n.add(res.Outcome)
		writeJSON(w, status, res)
	}

	if r.Method != http.MethodGet {
		res.Outcome, res.Why = Invalid, "GET only"
		answer(http.StatusMethodNotAllowed)
		return
	}
	if c.Store == nil {
		res.Outcome, res.Why = Unavailable, "catalog not loaded: "+c.LoadError
		answer(http.StatusOK)
		return
	}
	t := time.Now()
	p, g14, found, invalid, err := Lookup(c.Store, raw)
	res.MS.Lookup = ms(time.Since(t))
	res.GTIN14 = g14
	switch {
	case invalid != nil:
		res.Outcome, res.Why = Invalid, invalid.Error()
	case err != nil:
		res.Outcome, res.Why = Unavailable, err.Error()
		ev.Error = err.Error()
	case !found:
		res.Outcome = Miss
	default:
		res.Outcome, res.Found, res.Product = Hit, true, &p
		res.Admissible, res.Provenance = Admissible(p), Provenance(p)
	}
	answer(http.StatusOK)
}
