package inference

// The one route (X32a, T9 2026-10-27): POST /v1/analyze and POST /api/analyze.
// One photo in, every food item in it out, the same answer shape for one
// item and for many. Design: docs/icarus_x32_mockup_2026-10-27.md (project)
// and docs/sources/X32a.md.
//
//	caller (bearer token, or the app's session cookie) -> per-user/IP rate
//	limit -> read the image (analyze's formats, 5 MB) -> store the photo ->
//	barcode (the phone's read) -> food gate -> busy -> daily cap ->
//	SAM 3.1 + Muse Spark (Engine.Run auto) -> drop the model's "no food"
//	items -> dedup against the caller's live listings -> item photos ->
//	answer + one event line (every exit)
//
// GET /v1/analyze/photo/{id} and /api/analyze/photo/{id} serve an item
// photo to its owner (the app's review list).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

// Error codes: the "code" of every non-200 answer.
const (
	CodeBadRequest      = "bad_request"
	CodeUnauthorized    = "unauthorized"
	CodeMethod          = "method_not_allowed"
	CodeTooLarge        = "image_too_large"
	CodeUnsupported     = "unsupported_media"
	CodeNotFood         = "not_food"             // 422: the gate said NOT_FOOD
	CodeNoFoodSingle    = "model_no_food_single" // 422: gate FOOD, 1 item, the model found no food
	CodeNoFoodMulti     = "model_no_food_multi"  // 422: gate FOOD, 2+ items, the model dropped all
	CodeAlreadyListed   = "already_listed"       // 409: the one item is a live listing of the caller
	CodeRateLimited     = "rate_limited"         // 429
	CodeUpstream        = "upstream_error"       // 502
	CodeGateUnavailable = "gate_unavailable"     // 503: the gate could not check (fails closed)
	CodeBusy            = "busy"                 // 503
	CodeDailyCap        = "daily_cap_reached"    // 503: production-halting (T9)
	CodeTimeout         = "timeout"              // 504
	CodeNotFound        = "not_found"
	CodeInternal        = "internal_error"
)

// Sources of a 200 answer.
const (
	SourceBarcode = "barcode"
	SourceVision  = "vision"
)

// CropMargin is the margin around a box in an item photo (12% of its size).
const CropMargin = 0.12

// ServiceConfig is everything the handler needs; cmd/inventory fills it.
type ServiceConfig struct {
	Engine      *Engine
	Auth        *analyze.Bearer // the shared bearer token (plan A)
	Session     Authenticator   // the app's session cookie; nil = bearer only
	Limiter     *analyze.Limiter
	Budget      *Budget
	Events      *Events
	Photos      *PhotoStore
	GateURL     string // http://gate:8084
	GateHTTP    *http.Client
	BarcodeURL  string // http://barcode:8083; "" = no barcode step
	ListingsURL string // http://listings:8082; "" = no dedup
	FastHTTP    *http.Client
	MaxImage    int64         // bytes (5 MB, T9 2026-10-27: the lower of the two limits)
	MaxInFlight int           // photos at the same time; more get 503 busy
	Timeout     time.Duration // per photo, gate and every API call included (55 s: under the LB's 60 s)
	Origin      string        // CORS allow-origin; "" = no CORS headers
	Build       string
	now         func() time.Time
	slots       chan struct{}
}

// AnswerItem is one item: the 8 contract fields unchanged, nested.
type AnswerItem struct {
	Item    int            `json:"item"`
	Fields  analyze.Result `json:"fields"`
	PhotoID string         `json:"photo_id"`
}

// Answer is the 200 body (the same shape for 1 item and for many).
type Answer struct {
	RequestID string       `json:"request_id"`
	Source    string       `json:"source"`
	Model     string       `json:"model"`
	LatencyMS int64        `json:"latency_ms"`
	Count     int          `json:"count"`
	Items     []AnswerItem `json:"items"`
}

// ErrorAnswer is every non-200 body.
type ErrorAnswer struct {
	RequestID string `json:"request_id"`
	Error     string `json:"error"`
	Code      string `json:"code"`
}

// NewService builds the mux: GET /healthz, POST /v1/analyze + /api/analyze,
// GET /v1/analyze/photo/{id} + /api/analyze/photo/{id}.
func NewService(c ServiceConfig) http.Handler {
	if c.MaxImage == 0 {
		c.MaxImage = 5 << 20
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
	if c.FastHTTP == nil {
		c.FastHTTP = &http.Client{Timeout: 5 * time.Second}
	}
	if c.now == nil {
		c.now = time.Now
	}
	c.slots = make(chan struct{}, c.MaxInFlight)
	c.Engine.defaults() // once, before any request: Run then only reads the fields
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{"ok": true, "service": "analyze", "build": c.Build,
			"in_flight": len(c.slots), "max_in_flight": c.MaxInFlight}
		if c.Budget != nil {
			body["spent_today_usd"], body["cap_usd"] = c.Budget.Spent(), c.Budget.Cap
		}
		writeJSONResp(w, http.StatusOK, body)
	})
	for _, p := range []string{"/v1/analyze", "/api/analyze"} {
		mux.HandleFunc(p, c.analyze)
		mux.HandleFunc("GET "+p+"/photo/{id}", c.photo)
	}
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

// caller is the bearer token (no user) or the session's user.
func (c *ServiceConfig) caller(r *http.Request) (kind string, u User, ok bool) {
	if c.Auth != nil && c.Auth.OK(r) {
		return "bearer", User{}, true
	}
	if c.Session != nil {
		if u, ok := c.Session.Who(r); ok {
			return "session", u, true
		}
	}
	return "", User{}, false
}

// codeFor maps analyze's input refusals to codes.
func codeFor(status int) string {
	switch status {
	case http.StatusRequestEntityTooLarge:
		return CodeTooLarge
	case http.StatusUnsupportedMediaType:
		return CodeUnsupported
	}
	return CodeBadRequest
}

func (c *ServiceConfig) analyze(w http.ResponseWriter, r *http.Request) {
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
	ev := Event{TS: t0.UTC().Format(time.RFC3339Nano), Service: "analyze", Step: "analyze", TraceID: trace,
		RequestID: id, RemoteIP: remote, ForwardedIP: fwd, Build: c.Build, Model: c.Engine.Muse}
	finish := func(status int, route string) {
		ev.Status, ev.Route, ev.TotalMS = status, route, time.Since(t0).Milliseconds()
		if c.Budget != nil {
			ev.SpentTodayUSD = c.Budget.Spent()
		}
		c.Events.Record(ev)
	}
	fail := func(status int, code, msg string) {
		ev.Code = code
		if ev.Error == "" {
			ev.Error = msg
		}
		finish(status, "error")
		writeJSONResp(w, status, ErrorAnswer{RequestID: id, Error: msg, Code: code})
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		fail(http.StatusMethodNotAllowed, CodeMethod, "POST only")
		return
	}
	kind, user, ok := c.caller(r)
	if !ok {
		fail(http.StatusUnauthorized, CodeUnauthorized, "sign in, or send the bearer token")
		return
	}
	ev.Caller, ev.UserID = kind, user.ID
	key := "user:" + user.ID // T9: 6/min per user; bearer callers per IP
	if kind == "bearer" {
		key = remote
		if fwd != "" {
			key = fwd
		}
	}
	if c.Limiter != nil && !c.Limiter.Allow(key) {
		fail(http.StatusTooManyRequests, CodeRateLimited, "too many photos this minute; wait a minute")
		return
	}
	img, mime, st, msg := analyze.Config{MaxImage: c.MaxImage}.ReadImage(r)
	ev.ImageBytes, ev.Mime = len(img), mime
	if st != 0 {
		fail(st, codeFor(st), msg)
		return
	}
	sum := sha256.Sum256(img)
	ev.ImageSHA256 = hex.EncodeToString(sum[:])
	stored, err := c.Photos.SaveOriginal(ev.ImageSHA256, img)
	ev.PhotoStored = stored
	if err != nil {
		fail(http.StatusInternalServerError, CodeInternal, "photo store: "+err.Error())
		return
	}
	code := strings.TrimSpace(r.FormValue("barcode"))
	if len(code) > 64 {
		code = code[:64]
	}
	ev.Barcode = code
	ans := Answer{RequestID: id, Model: c.Engine.Muse, Items: []AnswerItem{}}
	done := func() {
		ans.Count = len(ans.Items)
		ans.LatencyMS = time.Since(t0).Milliseconds()
		ev.Source, ev.Items = ans.Source, ans.Count
		for _, it := range ans.Items {
			ev.PhotoIDs = append(ev.PhotoIDs, it.PhotoID)
		}
		finish(http.StatusOK, ans.Source)
		writeJSONResp(w, http.StatusOK, ans)
	}
	full := func(item int) (string, error) { // the uploaded photo as an item photo
		return c.Photos.SaveItem(img, ItemPhotoMeta{UserID: user.ID, RequestID: id, ImageSHA256: ev.ImageSHA256,
			Item: item, Kind: "full", TS: t0.UTC().Format(time.RFC3339)})
	}
	var listed map[string]ListedRef // nil = no dedup
	dedup := func() {
		if kind != "session" || c.ListingsURL == "" {
			ev.Dedup = "skipped"
			return
		}
		td := time.Now()
		live, err := liveListings(r, c.FastHTTP, c.ListingsURL, c.now().UTC())
		ev.DedupMS = time.Since(td).Milliseconds()
		if err != nil {
			ev.Dedup, ev.Error = "unavailable", "dedup: "+err.Error()
			return
		}
		ev.Dedup, listed = "ok", listedByName(live)
	}

	// 1. barcode (read on the phone): the fast path, no model, no cap
	if code != "" && c.BarcodeURL != "" {
		tb := time.Now()
		bc, bst, berr := lookupBarcode(r, c.FastHTTP, c.BarcodeURL, trace, code)
		ev.BarcodeMS, ev.BarcodeStatus, ev.BarcodeOutcome = time.Since(tb).Milliseconds(), bst, bc.Outcome
		if berr != nil {
			ev.Error = "barcode: " + berr.Error() // answers like a miss
		}
		if berr == nil && bc.Outcome == "HIT" && bc.Admissible && bc.Product != nil {
			p := bc.Product
			ev.CatalogServings = p.NumberOfServing // a label serving: logged, not shown
			res := analyze.Result{Name: p.Title, Description: p.Description, IsNonVeg: p.IsNonVeg,
				NumberOfServing: analyze.DefaultServings, ServingType: p.ServingType, MealCondition: p.MealCondition,
				Weight: p.Weight, MealCost: p.MealCost}
			if dedup(); listed != nil {
				if _, hit := listed[normName(res.Name)]; hit {
					ev.DroppedListed = 1
					fail(http.StatusConflict, CodeAlreadyListed, "this item is already live in your listings")
					return
				}
			}
			pid, err := full(1)
			if err != nil {
				fail(http.StatusInternalServerError, CodeInternal, "photo store: "+err.Error())
				return
			}
			ans.Source, ans.Model = SourceBarcode, "catalog"
			ans.Items = append(ans.Items, AnswerItem{Item: 1, Fields: res, PhotoID: pid})
			done()
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), c.Timeout)
	defer cancel()

	// 2. the food gate: non-food never reaches the API ($0)
	tg := time.Now()
	verdict, p, gst, gerr := c.gate(ctx, trace, fwd, img, mime)
	ev.GateMS, ev.GateStatus, ev.GateVerdict, ev.GateP = time.Since(tg).Milliseconds(), gst, verdict, p
	switch {
	case gerr != nil || verdict == "" || verdict == "UNAVAILABLE":
		ev.GateVerdict = "UNAVAILABLE"
		if gerr != nil {
			ev.Error = "gate: " + gerr.Error()
		}
		fail(http.StatusServiceUnavailable, CodeGateUnavailable, "the food check is not available right now; try again")
		return
	case verdict != "FOOD":
		fail(http.StatusUnprocessableEntity, CodeNotFood, "no food detected in the image")
		return
	}

	// 3. busy: the box has 1.9 GiB; a decoded 12 MP photo is about 32 MB
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	default:
		fail(http.StatusServiceUnavailable, CodeBusy, "the scanner is busy; try again in a few seconds")
		return
	}

	// 4. the daily spend cap (OWASP API4:2023)
	var hold *Hold
	if c.Budget != nil {
		var ok bool
		if hold, ok = c.Budget.Reserve(); !ok {
			fail(http.StatusServiceUnavailable, CodeDailyCap, "the daily limit is reached; try again after 00:00 UTC")
			return
		}
	}

	// 5. SAM + Muse
	inv, err := c.Engine.Run(ctx, img, mime, ModeAuto)
	if hold != nil {
		hold.Settle(inv.SpentUSD())
	}
	ev.fromInventory(inv)
	ev.FrameW, ev.FrameH = inv.FrameW, inv.FrameH
	ev.NotNamed = inv.Unnamed + inv.Small
	multi := inv.Kept >= 2
	if err != nil {
		ledgerBoxes(&ev, inv, nil, nil, nil)
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			fail(http.StatusGatewayTimeout, CodeTimeout,
				fmt.Sprintf("this photo took longer than %s; try a closer photo of fewer items", c.Timeout))
			return
		}
		fail(http.StatusBadGateway, CodeUpstream, "the scanner could not read this photo: "+err.Error())
		return
	}

	// 6. the model's own "no food" answers are dropped (T9)
	noFood := map[int]bool{} // index into inv.Items
	var items []int
	for i, it := range inv.Items {
		if ModelSaysNoFood(it.Result) {
			noFood[i] = true
			continue
		}
		items = append(items, i)
	}
	ev.DroppedNoFood = len(noFood)
	if len(items) == 0 {
		ledgerBoxes(&ev, inv, nil, noFood, nil)
		if multi || inv.NotFood {
			fail(http.StatusUnprocessableEntity, CodeNoFoodMulti, "the scanner found no food it can name in these items")
		} else {
			fail(http.StatusUnprocessableEntity, CodeNoFoodSingle, "the scanner found no food it can name in this photo")
		}
		return
	}

	// 7. dedup against the caller's live listings (T9: one item = 409;
	// several = the listed ones are dropped and logged)
	listedIdx := map[int]bool{}
	if dedup(); listed != nil {
		for _, i := range items {
			if _, hit := listed[normName(inv.Items[i].Name)]; hit {
				listedIdx[i] = true
			}
		}
	}
	ev.DroppedListed = len(listedIdx)
	var keep []int
	for _, i := range items {
		if !listedIdx[i] {
			keep = append(keep, i)
		}
	}
	if len(keep) == 0 {
		ledgerBoxes(&ev, inv, nil, noFood, listedIdx)
		fail(http.StatusConflict, CodeAlreadyListed, "this item is already live in your listings")
		return
	}

	// 8. item photos: a crop of the largest box when the photo had 2+
	// boxes, else the uploaded photo
	var src image.Image
	if multi {
		if s, _, err := image.Decode(bytes.NewReader(img)); err == nil {
			src = Upright(s, Orientation(img))
		}
	}
	answerOf := map[int]int{}
	for _, i := range keep {
		it := inv.Items[i]
		n := len(ans.Items) + 1
		var pid string
		var err error
		if b, ok := inv.LargestBox(it); ok && src != nil {
			if jb, cerr := CropJPEG(src, b, CropMargin); cerr == nil {
				pid, err = c.Photos.SaveItem(jb, ItemPhotoMeta{UserID: user.ID, RequestID: id, ImageSHA256: ev.ImageSHA256,
					Item: n, Kind: "crop", Box: boxSlice(b), FrameW: b.W, FrameH: b.H, TS: t0.UTC().Format(time.RFC3339)})
			}
		}
		if pid == "" && err == nil {
			pid, err = full(n)
		}
		if err != nil {
			fail(http.StatusInternalServerError, CodeInternal, "photo store: "+err.Error())
			return
		}
		answerOf[i] = n
		ans.Items = append(ans.Items, AnswerItem{Item: n, Fields: it.Result, PhotoID: pid})
	}
	ledgerBoxes(&ev, inv, answerOf, noFood, listedIdx)
	ans.Source = SourceVision
	done()
}

func boxSlice(b Instance) []int { x := b.Box(); return x[:] }

// ledgerBoxes logs every kept box and what became of it.
func ledgerBoxes(ev *Event, inv *Inventory, answerOf map[int]int, noFood, listed map[int]bool) {
	if inv == nil {
		return
	}
	type fate struct {
		outcome string
		item    int
		name    string
	}
	byBox := map[[4]int]fate{}
	for i, it := range inv.Items {
		f := fate{outcome: "item", item: answerOf[i], name: it.Name}
		switch {
		case noFood[i]:
			f = fate{outcome: "model_no_food", name: it.Name}
		case listed[i]:
			f = fate{outcome: "already_listed", name: it.Name}
		case answerOf[i] == 0:
			f = fate{outcome: "not_returned", name: it.Name}
		}
		for _, b := range it.Boxes {
			byBox[b] = f
		}
	}
	for _, it := range inv.NotListed {
		for _, b := range it.Boxes {
			byBox[b] = fate{outcome: "not_food", name: it.Name}
		}
	}
	for n, b := range inv.KeptBoxes {
		f, ok := byBox[b]
		if !ok {
			f = fate{outcome: "not_named"}
		}
		ev.Boxes = append(ev.Boxes, EventBox{N: n + 1, Box: b, Outcome: f.outcome, Item: f.item, Name: f.name})
	}
}

// photo serves an item photo to its owner (OWASP API1:2023); 404 for
// anything else, so a photo_id cannot be probed.
func (c *ServiceConfig) photo(w http.ResponseWriter, r *http.Request) {
	_, u, ok := c.caller(r)
	if !ok {
		writeJSONResp(w, http.StatusUnauthorized, ErrorAnswer{Error: "sign in, or send the bearer token", Code: CodeUnauthorized})
		return
	}
	if c.Photos == nil {
		writeJSONResp(w, http.StatusNotFound, ErrorAnswer{Error: "no such photo", Code: CodeNotFound})
		return
	}
	b, _, err := ReadItem(filepath.Join(c.Photos.Dir, "items"), r.PathValue("id"), u.ID)
	if err != nil {
		writeJSONResp(w, http.StatusNotFound, ErrorAnswer{Error: "no such photo", Code: CodeNotFound})
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(b)
}

type gateAnswer struct {
	Verdict string  `json:"verdict"`
	P       float64 `json:"p"`
}

// gate posts the raw image, as router/router.go did.
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
