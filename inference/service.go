package inference

// The /v1/inventory service (X31). One photo in, every food item in it out.
//
//	bearer token -> per-IP rate limit -> read the image (analyze's formats
//	and checks, R2) -> food gate (PE-Core-S16-384) -> busy check -> daily
//	spend cap -> Engine.Run (auto) -> answer + one event line
//
// The top-level fields are the largest group (R4), so a client of
// /v1/analyze reads the answer unchanged; items[] holds every group.
// Design: docs/inference_engine_design.md; sources: docs/sources/X31.md.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

// Routes of an inventory answer.
const (
	RouteInventory   = "inventory"   // items found and named
	RouteNotFood     = "not_food"    // the gate said NOT_FOOD, or every product is not food
	RouteUnavailable = "unavailable" // the gate could not decide: refused (fails closed)
	RouteBusy        = "busy"        // MaxInFlight photos are running
	RouteBudget      = "budget"      // the daily spend cap is reached
	RouteError       = "error"
)

// ServiceConfig is everything the handler needs; cmd/inventory fills it.
type ServiceConfig struct {
	Engine      *Engine
	Auth        *analyze.Bearer
	Limiter     *analyze.Limiter
	Budget      *Budget
	Events      *Events
	GateURL     string // http://gate:8084
	GateHTTP    *http.Client
	MaxImage    int64         // bytes (8 MB: the X32 upload, about 2376 px at JPEG 0.9)
	MaxInFlight int           // photos at the same time; more get 503 busy
	Timeout     time.Duration // per photo, gate and every API call included (55 s: under the LB's 60 s)
	Origin      string        // CORS allow-origin; "" = no CORS headers
	Build       string
	slots       chan struct{}
}

// InventoryAnswer is the 200 body.
type InventoryAnswer struct {
	RequestID string `json:"request_id"`
	TraceID   string `json:"trace_id"`
	Route     string `json:"route"`
	Message   string `json:"message,omitempty"`
	// The largest group in the analyze contract (R4). Empty when not food.
	analyze.Result
	Items       []Item  `json:"items"`
	NotListed   []Item  `json:"not_listed"`
	NotFood     bool    `json:"not_food"`
	CloserPhoto bool    `json:"closer_photo"`
	Listings    int     `json:"listings"` // one per item group
	Counted     int     `json:"counted"`  // every physical item found, named or not
	Width       int     `json:"width"`    // upright pixels; boxes are in these
	Height      int     `json:"height"`
	GateVerdict string  `json:"gate_verdict"`
	GateP       float64 `json:"gate_p"`
	ImageSHA256 string  `json:"image_sha256"`
	CostUSD     float64 `json:"cost_usd"`
	Model       string  `json:"model"`
	LatencyMS   int64   `json:"latency_ms"`
}

type inventoryError struct {
	RequestID string `json:"request_id"`
	TraceID   string `json:"trace_id"`
	Route     string `json:"route"`
	Error     string `json:"error"`
}

// NewService builds the mux: GET /healthz and POST /v1/inventory.
func NewService(c ServiceConfig) http.Handler {
	if c.MaxImage == 0 {
		c.MaxImage = 8 << 20
	}
	if c.MaxInFlight <= 0 {
		c.MaxInFlight = 3
	}
	if c.Timeout == 0 {
		c.Timeout = 55 * time.Second
	}
	if c.GateHTTP == nil {
		c.GateHTTP = &http.Client{Timeout: 20 * time.Second}
	}
	c.slots = make(chan struct{}, c.MaxInFlight)
	c.Engine.defaults() // once, before any request: Run then only reads the fields
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{"ok": true, "service": "inventory", "build": c.Build,
			"in_flight": len(c.slots), "max_in_flight": c.MaxInFlight}
		if c.Budget != nil {
			body["spent_today_usd"], body["cap_usd"] = c.Budget.Spent(), c.Budget.Cap
		}
		writeJSONResp(w, http.StatusOK, body)
	})
	mux.HandleFunc("/v1/inventory", c.inventory)
	return mux
}

func writeJSONResp(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (c *ServiceConfig) cors(w http.ResponseWriter) {
	if c.Origin == "" {
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", c.Origin)
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Max-Age", "600")
	w.Header().Set("Vary", "Origin")
}

func (c *ServiceConfig) inventory(w http.ResponseWriter, r *http.Request) {
	t0 := time.Now()
	id := analyze.NewRequestID()
	trace := strings.TrimSpace(r.Header.Get("X-Trace-Id"))
	if trace == "" || len(trace) > 64 {
		trace = id
	}
	w.Header().Set("X-Request-Id", id)
	w.Header().Set("X-Trace-Id", trace)
	c.cors(w)
	remote, fwd := analyze.ClientIPs(r)
	ev := Event{TS: t0.UTC().Format(time.RFC3339Nano), Service: "inventory", Step: "inventory", TraceID: trace,
		RequestID: id, RemoteIP: remote, ForwardedIP: fwd, Build: c.Build, Model: c.Engine.Muse}
	finish := func(status int, route string) {
		ev.Status, ev.Route, ev.TotalMS = status, route, time.Since(t0).Milliseconds()
		if c.Budget != nil {
			ev.SpentTodayUSD = c.Budget.Spent()
		}
		c.Events.Record(ev)
	}
	fail := func(status int, route, msg string) {
		ev.Error = msg
		finish(status, route)
		writeJSONResp(w, status, inventoryError{RequestID: id, TraceID: trace, Route: route, Error: msg})
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		fail(http.StatusMethodNotAllowed, RouteError, "POST only")
		return
	}
	if !c.Auth.OK(r) {
		fail(http.StatusUnauthorized, RouteError, "missing or bad bearer token")
		return
	}
	key := remote
	if fwd != "" {
		key = fwd
	}
	if c.Limiter != nil && !c.Limiter.Allow(key) {
		fail(http.StatusTooManyRequests, RouteError, "rate limited")
		return
	}
	img, mime, st, msg := analyze.Config{MaxImage: c.MaxImage}.ReadImage(r)
	ev.ImageBytes, ev.Mime = len(img), mime
	if st != 0 {
		fail(st, RouteError, msg)
		return
	}
	sum := sha256.Sum256(img)
	ev.ImageSHA256 = hex.EncodeToString(sum[:])
	ans := InventoryAnswer{RequestID: id, TraceID: trace, ImageSHA256: ev.ImageSHA256, Model: c.Engine.Muse,
		Items: []Item{}, NotListed: []Item{}}
	done := func(status int) {
		ans.LatencyMS = time.Since(t0).Milliseconds()
		finish(status, ans.Route)
		writeJSONResp(w, status, ans)
	}

	ctx, cancel := context.WithTimeout(r.Context(), c.Timeout)
	defer cancel()

	// 1. the food gate: non-food never reaches the API ($0)
	tg := time.Now()
	verdict, p, gst, gerr := c.gate(ctx, trace, fwd, img, mime)
	ev.GateMS, ev.GateStatus, ev.GateVerdict, ev.GateP = time.Since(tg).Milliseconds(), gst, verdict, p
	ans.GateVerdict, ans.GateP = verdict, p
	switch {
	case gerr != nil || verdict == "" || verdict == "UNAVAILABLE":
		if gerr != nil {
			ev.Error = "gate: " + gerr.Error()
		}
		ans.GateVerdict = "UNAVAILABLE"
		ans.Route, ans.Message = RouteUnavailable, "We couldn't check this photo. Please try again."
		done(http.StatusServiceUnavailable)
		return
	case verdict != "FOOD":
		ans.Route, ans.NotFood, ans.Message = RouteNotFood, true, "That doesn't look like food. Try a photo of the food items."
		done(http.StatusOK)
		return
	}

	// 2. busy: the box has 1.9 GiB; a decoded 12 MP photo is about 32 MB
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	default:
		fail(http.StatusServiceUnavailable, RouteBusy, "busy: try again in a few seconds")
		return
	}

	// 3. the daily spend cap (OWASP API4:2023)
	var hold *Hold
	if c.Budget != nil {
		var ok bool
		if hold, ok = c.Budget.Reserve(); !ok {
			fail(http.StatusServiceUnavailable, RouteBudget, "the daily spend cap is reached; try again tomorrow (UTC)")
			return
		}
	}

	// 4. SAM + Muse
	inv, err := c.Engine.Run(ctx, img, mime, ModeAuto)
	if hold != nil {
		hold.Settle(inv.SpentUSD())
	}
	ev.fromInventory(inv)
	if err != nil {
		status, msg := http.StatusBadGateway, err.Error()
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
			msg = fmt.Sprintf("this photo took longer than %s; try a closer photo of fewer items", c.Timeout)
		}
		fail(status, RouteError, msg)
		return
	}
	ans.Items, ans.NotListed = nonNil(inv.Items), nonNil(inv.NotListed)
	ans.NotFood, ans.CloserPhoto = inv.NotFood, inv.CloserPhoto
	ans.Listings, ans.Counted = inv.Listings(), inv.Counted()
	ans.Width, ans.Height, ans.CostUSD = inv.Width, inv.Height, inv.SpentUSD()
	if top, ok := inv.TopLevel(); ok {
		ans.Result = top
	}
	ans.Route = RouteInventory
	if inv.NotFood {
		ans.Route, ans.Message = RouteNotFood, "No food found. The products are listed below; you can add one anyway."
	}
	done(http.StatusOK)
}

func nonNil(x []Item) []Item {
	if x == nil {
		return []Item{}
	}
	return x
}

type gateAnswer struct {
	Verdict string  `json:"verdict"`
	P       float64 `json:"p"`
}

// gate posts the raw image, as router/router.go does.
func (c *ServiceConfig) gate(ctx context.Context, trace, fwd string, img []byte, mime string) (string, float64, int, error) {
	if c.GateURL == "" {
		return "", 0, 0, errors.New("no gate configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.GateURL, "/")+"/v1/gate", bytes.NewReader(img))
	if err != nil {
		return "", 0, 0, err
	}
	req.Header.Set("Content-Type", mime)
	req.Header.Set("X-Trace-Id", trace)
	if fwd != "" {
		req.Header.Set("X-Forwarded-For", fwd)
	}
	resp, err := c.GateHTTP.Do(req)
	if err != nil {
		return "", 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", 0, resp.StatusCode, fmt.Errorf("status %d", resp.StatusCode)
	}
	var g gateAnswer
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&g); err != nil {
		return "", 0, resp.StatusCode, err
	}
	return g.Verdict, g.P, resp.StatusCode, nil
}
