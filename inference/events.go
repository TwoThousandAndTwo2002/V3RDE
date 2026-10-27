package inference

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Event is one line per /v1/inventory request in
// <dir>/events-YYYYMMDD.jsonl (the telemetry sidecar ships the folder).
// No image bytes, no key, no token. The router logs the same trace_id.
type Event struct {
	TS            string      `json:"ts"`
	Service       string      `json:"service"`
	Step          string      `json:"step"`
	TraceID       string      `json:"trace_id"`
	RequestID     string      `json:"request_id"`
	Status        int         `json:"status"`
	Route         string      `json:"route"`
	RemoteIP      string      `json:"remote_ip"`
	ForwardedIP   string      `json:"forwarded_ip,omitempty"`
	ImageSHA256   string      `json:"image_sha256,omitempty"`
	ImageBytes    int         `json:"image_bytes"`
	Mime          string      `json:"mime,omitempty"`
	GateVerdict   string      `json:"gate_verdict,omitempty"`
	GateP         float64     `json:"gate_p,omitempty"`
	GateStatus    int         `json:"gate_status,omitempty"`
	GateMS        int64       `json:"gate_ms"`
	Orientation   int         `json:"orientation,omitempty"`
	Width         int         `json:"width,omitempty"`
	Height        int         `json:"height,omitempty"`
	Instances     int         `json:"sam_instances,omitempty"`
	Kept          int         `json:"kept,omitempty"`
	Named         int         `json:"named,omitempty"`
	Small         int         `json:"small,omitempty"`
	Trays         int         `json:"trays,omitempty"`
	Listings      int         `json:"listings,omitempty"`
	Units         int         `json:"units,omitempty"`
	Counted       int         `json:"counted,omitempty"`
	NotListed     int         `json:"not_listed,omitempty"`
	NotFood       bool        `json:"not_food,omitempty"`
	Calls         []EventCall `json:"calls,omitempty"`
	CostUSD       float64     `json:"cost_usd"`        // what this request paid
	SpentTodayUSD float64     `json:"spent_today_usd"` // the budget after it (UTC day)
	EngineMS      int64       `json:"engine_ms,omitempty"`
	TotalMS       int64       `json:"total_ms"`
	Model         string      `json:"model"`
	Build         string      `json:"build"`
	Error         string      `json:"error,omitempty"`
}

// EventCall is one API call of the request.
type EventCall struct {
	Purpose      string  `json:"purpose"`
	Model        string  `json:"model"`
	Status       int     `json:"status"`
	MS           int64   `json:"ms"`
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	CostUSD      float64 `json:"cost_usd"`
	Error        string  `json:"error,omitempty"`
}

func (ev *Event) fromInventory(inv *Inventory) {
	if inv == nil {
		return
	}
	ev.Orientation, ev.Width, ev.Height = inv.Orientation, inv.Width, inv.Height
	ev.Instances, ev.Kept, ev.Named, ev.Small, ev.Trays = inv.Instances, inv.Kept, inv.Named, inv.Small, inv.Trays
	ev.Listings, ev.Units, ev.Counted, ev.NotListed, ev.NotFood = inv.Listings(), inv.Units(), inv.Counted(), len(inv.NotListed), inv.NotFood
	ev.CostUSD, ev.EngineMS = inv.SpentUSD(), inv.MS
	for _, c := range inv.Calls {
		ev.Calls = append(ev.Calls, EventCall{Purpose: c.Purpose, Model: c.Model, Status: c.Status, MS: c.MS,
			InputTokens: c.Usage.InputTokens, OutputTokens: c.Usage.OutputTokens, CostUSD: c.Cost(), Error: c.Error})
	}
}

// Events appends one line per request with fsync, so the spend in the file
// is the spend the budget restores after a restart.
type Events struct {
	Dir string
	mu  sync.Mutex
	now func() time.Time
}

func NewEvents(dir string) *Events { return &Events{Dir: dir, now: time.Now} }

// Path is the file for day t (UTC).
func (e *Events) Path(t time.Time) string {
	return filepath.Join(e.Dir, "events-"+t.UTC().Format("20060102")+".jsonl")
}

// Record writes ev; an error is logged, never fatal to the request.
func (e *Events) Record(ev Event) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.write(ev); err != nil {
		log.Printf("events: %v (request %s)", err, ev.RequestID)
	}
}

func (e *Events) write(ev Event) error {
	f, err := os.OpenFile(e.Path(e.now()), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	if err := json.NewEncoder(w).Encode(ev); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}
	return f.Sync()
}

// SpentOn sums cost_usd over the lines of day t: the budget's start value.
func (e *Events) SpentOn(t time.Time) float64 {
	f, err := os.Open(e.Path(t))
	if err != nil {
		return 0
	}
	defer f.Close()
	s := 0.0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var l struct {
			CostUSD float64 `json:"cost_usd"`
		}
		if json.Unmarshal(sc.Bytes(), &l) == nil {
			s += l.CostUSD
		}
	}
	return s
}

// Budget is the daily spend cap (T9, 2026-10-26: $20 per UTC day). A photo
// reserves Estimate before it runs and settles at its real cost, so
// photos running at the same time cannot pass the cap together.
type Budget struct {
	Cap      float64
	Estimate float64 // reserved per photo (0.10: about 2x the X30b mean)
	mu       sync.Mutex
	day      string
	spent    float64
	held     float64
	now      func() time.Time
}

// NewBudget starts at what events already records for today.
func NewBudget(capUSD, estimate float64, ev *Events) *Budget {
	b := &Budget{Cap: capUSD, Estimate: estimate, now: time.Now}
	b.day = b.now().UTC().Format("20060102")
	if ev != nil {
		b.spent = ev.SpentOn(b.now())
	}
	return b
}

func (b *Budget) rollLocked() {
	if d := b.now().UTC().Format("20060102"); d != b.day {
		b.day, b.spent = d, 0
	}
}

// Spent is today's settled spend.
func (b *Budget) Spent() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollLocked()
	return b.spent
}

// Hold is one photo's reservation.
type Hold struct {
	b    *Budget
	amt  float64
	day  string
	done bool
}

// Reserve holds Estimate; false when spent + held + Estimate > Cap.
func (b *Budget) Reserve() (*Hold, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollLocked()
	if b.spent+b.held+b.Estimate > b.Cap {
		return nil, false
	}
	b.held += b.Estimate
	return &Hold{b: b, amt: b.Estimate, day: b.day}, true
}

// Settle frees the hold and adds the real cost (once).
func (h *Hold) Settle(cost float64) {
	b := h.b
	b.mu.Lock()
	defer b.mu.Unlock()
	if h.done {
		return
	}
	h.done = true
	b.held -= h.amt
	if b.held < 0 {
		b.held = 0
	}
	b.rollLocked()
	if h.day == b.day {
		b.spent += cost
	}
}
