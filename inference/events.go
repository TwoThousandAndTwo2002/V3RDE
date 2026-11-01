package inference

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Event is one line per request in <dir>/events-YYYYMMDD.jsonl (the
// telemetry sidecar ships the folder). No image bytes, no key, no token.
// X32a: one line per /v1/analyze request, every exit included; the field
// names follow the router "scan" event and the X31 inventory event.
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
	GateAtMS      int64       `json:"gate_at_ms"` // X48a: the gate start, ms after the request start
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
	Rejected      []string    `json:"rejected,omitempty"` // X49r: why answers failed the contract (box: reason), at most RejectLogMax
	Calls         []EventCall `json:"calls,omitempty"`
	CostUSD       float64     `json:"cost_usd"`          // what this request paid
	CapKey        string      `json:"cap_key,omitempty"` // X44a: the caller's daily-cap key (the rate-limit key)
	SpentTodayUSD float64     `json:"spent_today_usd"`   // the budget after it (UTC day)
	EngineMS      int64       `json:"engine_ms,omitempty"`
	TotalMS       int64       `json:"total_ms"`
	Model         string      `json:"model"`
	Build         string      `json:"build"`
	Error         string      `json:"error,omitempty"`
	// X32a: the one route
	Caller          string        `json:"caller,omitempty"`  // session | bearer
	UserID          string        `json:"user_id,omitempty"` // "" for bearer callers
	Code            string        `json:"code,omitempty"`    // the error code; "" on 200
	Source          string        `json:"source,omitempty"`  // barcode | vision
	PhotoStored     bool          `json:"photo_stored"`
	Barcode         string        `json:"barcode,omitempty"`
	BarcodeOutcome  string        `json:"barcode_outcome,omitempty"`
	BarcodeStatus   int           `json:"barcode_status,omitempty"`
	BarcodeMS       int64         `json:"barcode_ms,omitempty"`
	CatalogServings float64       `json:"catalog_servings,omitempty"`
	Barcodes        []BarcodeRead `json:"barcodes,omitempty"`              // I1: every code of a photo with many codes
	Of              string        `json:"of,omitempty"`                    // I3: a find's scan (its request_id)
	Find            *FindRec      `json:"find,omitempty"`                  // I3: what the find asked
	StockPhoto      string        `json:"stock_photo,omitempty"`           // N1: cutout | as_is | none; "" = not looked up
	Items           int           `json:"items"`                           // items in the answer
	DroppedNoFood   int           `json:"dropped_model_no_food,omitempty"` // the model's answer said no food
	DroppedListed   int           `json:"dropped_already_listed,omitempty"`
	NotNamed        int           `json:"not_named,omitempty"` // counted, not named (over the limit or too small)
	Dedup           string        `json:"dedup,omitempty"`     // ok | unavailable | skipped (no user)
	DedupMS         int64         `json:"dedup_ms,omitempty"`
	FrameW          int           `json:"frame_w,omitempty"` // the frame of the box coordinates
	FrameH          int           `json:"frame_h,omitempty"`
	Boxes           []EventBox    `json:"boxes,omitempty"`
	PhotoIDs        []string      `json:"photo_ids,omitempty"`
	// X32e: how the app took the photo (its "capture" form field, a small
	// JSON object: source photo|frame|native, ms from the press, the preview
	// video's time and state). Logged as sent; never used for the answer.
	Capture json.RawMessage `json:"capture,omitempty"`
	// X32g: what the user did with the answer (step "labels"; request_id =
	// the analyze request it labels). The truth for tuning thresholds.
	Labels *LabelSet `json:"labels,omitempty"`
	// X41a: the item photo bundle (step "photos_bundle"; no image bytes)
	Bundle *BundleStat `json:"bundle,omitempty"`
	// X44b: the scan ran as a job (?async=1); the line is written when it ends
	Async bool `json:"async,omitempty"`
}

// LabelSet is the app's report on one answer (X32g, T9 2026-10-27: "we are
// logging the corrected labels"): every item of the answer, the model's
// title and servings, the user's final ones, and whether it was kept.
type LabelSet struct {
	Outcome string      `json:"outcome"` // posted | retake | closed
	Locale  string      `json:"locale,omitempty"`
	Items   []LabelItem `json:"items"`
	// X48u (T9 2026-10-30: "all the changes made by users to fields generated
	// by the models should be recorded as this can feed the self improvement
	// process"): every change in order, and the chat lines.
	Edits []LabelEdit `json:"edits,omitempty"`
	Chat  []LabelChat `json:"chat,omitempty"`
}

// LabelEdit is one change of the review, in order. Field: title | servings |
// remove | pickup | photo | condition | diet | portions | weight (X49: the
// details the user changed: meal_condition, is_non_veg 0/1, serving_type,
// weight in kg). How: hand | voice | text | undo. From and To are the values
// (titles, servings, the photo choice, the details; "" = none); the pickup
// text is never sent (an address), only that it changed and how.
type LabelEdit struct {
	Item  int    `json:"item"` // 0 = the pickup
	Field string `json:"field"`
	From  string `json:"from,omitempty"`
	To    string `json:"to,omitempty"`
	How   string `json:"how"`
}

// LabelChat is one chat line of the review: how it came (voice | text), the
// words (as the voice ledger keeps the transcript), the changes it made.
type LabelChat struct {
	How     string `json:"how"`
	Text    string `json:"text"`
	Changes int    `json:"changes"`
	Undone  bool   `json:"undone,omitempty"`
	// Pickup: the line changed the pickup, so its words may hold an address:
	// the app sends no text, and the service keeps none (X48u review)
	Pickup bool `json:"pickup,omitempty"`
}

// LabelItem is one item of the answer as the user left it.
type LabelItem struct {
	Item          int    `json:"item"`
	PhotoID       string `json:"photo_id,omitempty"`
	ModelTitle    string `json:"model_title"`
	FinalTitle    string `json:"final_title"`
	ModelServings int    `json:"model_servings"`
	FinalServings int    `json:"final_servings"`
	Kept          bool   `json:"kept"`
	// X48u: the photo that posted: original | touched_up ("" = none chosen
	// or not posted); the touch-up job's end when one ran: done | kept | failed
	Photo   string `json:"photo,omitempty"`
	Touchup string `json:"touchup,omitempty"`
}

// CaptureField keeps the app's "capture" field when it is one JSON object of
// at most 512 bytes; anything else is dropped (X32e).
func CaptureField(s string) json.RawMessage {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 512 || s[0] != '{' || !json.Valid([]byte(s)) {
		return nil
	}
	return json.RawMessage(s)
}

// EventBox is one kept SAM box and what became of it (X32a, T9: every box
// is logged, for the training set). Box is [left, top, right, bottom] in
// SAM pixels on the upright stored photo (Event frame_w x frame_h).
type EventBox struct {
	N       int    `json:"n"`
	Box     [4]int `json:"box"`
	Outcome string `json:"outcome"`        // item | not_food | model_no_food | already_listed | not_named
	Item    int    `json:"item,omitempty"` // 1-based item in the answer (outcome item)
	Name    string `json:"name,omitempty"`
}

// EventCall is one API call of the request.
type EventCall struct {
	Purpose      string  `json:"purpose"`
	Model        string  `json:"model"`
	Status       int     `json:"status"`
	MS           int64   `json:"ms"`
	AtMS         int64   `json:"at_ms"`             // X48a: start, ms after the request start
	WaitMS       int64   `json:"wait_ms,omitempty"` // X48a: the wait for the food gate
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	CostUSD      float64 `json:"cost_usd"`
	Error        string  `json:"error,omitempty"`
}

// RejectLogMax is how many rejections one event keeps.
const RejectLogMax = 5

func (ev *Event) fromInventory(inv *Inventory) {
	if inv == nil {
		return
	}
	ev.Orientation, ev.Width, ev.Height = inv.Orientation, inv.Width, inv.Height
	ev.Instances, ev.Kept, ev.Named, ev.Small, ev.Trays = inv.Instances, inv.Kept, inv.Named, inv.Small, inv.Trays
	ev.Listings, ev.Units, ev.Counted, ev.NotListed, ev.NotFood = inv.Listings(), inv.Units(), inv.Counted(), len(inv.NotListed), inv.NotFood
	ev.CostUSD, ev.EngineMS = inv.SpentUSD(), inv.MS
	for i, rj := range inv.Rejected {
		if i == RejectLogMax {
			ev.Rejected = append(ev.Rejected, fmt.Sprintf("... %d more", len(inv.Rejected)-i))
			break
		}
		ev.Rejected = append(ev.Rejected, fmt.Sprintf("box %d: %s", rj.Box, cutText(rj.Reason, RejectTextMax+200)))
	}
	for _, c := range inv.Calls {
		ev.Calls = append(ev.Calls, EventCall{Purpose: c.Purpose, Model: c.Model, Status: c.Status, MS: c.MS, AtMS: c.AtMS, WaitMS: c.WaitMS,
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
	s, _ := e.SpentByKey(t)
	return s
}

// SpentByKey sums cost_usd over the lines of day t: the total, and the part
// of each cap_key (X44a). A line from before X44a has no cap_key: it counts
// in the total only.
func (e *Events) SpentByKey(t time.Time) (float64, map[string]float64) {
	by := map[string]float64{}
	f, err := os.Open(e.Path(t))
	if err != nil {
		return 0, by
	}
	defer f.Close()
	s := 0.0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var l struct {
			CostUSD float64 `json:"cost_usd"`
			CapKey  string  `json:"cap_key"`
		}
		if json.Unmarshal(sc.Bytes(), &l) == nil {
			s += l.CostUSD
			if l.CapKey != "" && l.CostUSD != 0 {
				by[l.CapKey] += l.CostUSD
			}
		}
	}
	return s, by
}

// Budget is the daily spend cap (T9, 2026-10-26: $20 per UTC day). A photo
// reserves Estimate before it runs and settles at its real cost, so
// photos running at the same time cannot pass the cap together.
//
// X44a (security audit S1 item 2): PerCaller is a second cap for each caller
// key (the rate-limit key: one signed-in user, or one bearer address), so one
// caller cannot use the whole day for every caller. 0 = off.
type Budget struct {
	Cap       float64
	Estimate  float64 // reserved per photo (0.10: about 2x the X30b mean)
	PerCaller float64
	mu        sync.Mutex
	day       string
	spent     float64
	held      float64
	byKey     map[string]float64 // settled today, per caller key
	heldKey   map[string]float64
	now       func() time.Time
}

// NewBudget starts at what events already records for today.
func NewBudget(capUSD, estimate float64, ev *Events) *Budget {
	b := &Budget{Cap: capUSD, Estimate: estimate, now: time.Now, byKey: map[string]float64{}, heldKey: map[string]float64{}}
	b.day = b.now().UTC().Format("20060102")
	if ev != nil {
		b.spent, b.byKey = ev.SpentByKey(b.now())
	}
	return b
}

func (b *Budget) rollLocked() {
	if d := b.now().UTC().Format("20060102"); d != b.day {
		b.day, b.spent = d, 0
		b.byKey = map[string]float64{}
	}
	if b.byKey == nil {
		b.byKey = map[string]float64{}
	}
	if b.heldKey == nil {
		b.heldKey = map[string]float64{}
	}
}

// SpentBy is today's settled spend of one caller key.
func (b *Budget) SpentBy(key string) float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollLocked()
	return b.byKey[key]
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
	key  string
	done bool
}

// Reserve holds Estimate with no caller key (the service cap only); false
// when spent + held + Estimate > Cap.
func (b *Budget) Reserve() (*Hold, bool) {
	h, why := b.ReserveFor("")
	return h, why == ""
}

// ReserveFor holds Estimate for caller key. The answer is the hold, or nil
// and why: CodeDailyCap (the day is used for everyone) or CodeCallerCap
// (this key's spent + held + Estimate > PerCaller).
func (b *Budget) ReserveFor(key string) (*Hold, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollLocked()
	if b.spent+b.held+b.Estimate > b.Cap {
		return nil, CodeDailyCap
	}
	if key != "" && b.PerCaller > 0 && b.byKey[key]+b.heldKey[key]+b.Estimate > b.PerCaller {
		return nil, CodeCallerCap
	}
	b.held += b.Estimate
	if key != "" {
		b.heldKey[key] += b.Estimate
	}
	return &Hold{b: b, amt: b.Estimate, day: b.day, key: key}, ""
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
	if h.key != "" {
		if b.heldKey[h.key] -= h.amt; b.heldKey[h.key] <= 1e-12 {
			delete(b.heldKey, h.key)
		}
	}
	if h.day == b.day {
		b.spent += cost
		if h.key != "" && cost != 0 {
			b.byKey[h.key] += cost
		}
	}
}
