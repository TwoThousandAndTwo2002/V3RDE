// Package post holds the Icarus services that work on a listing after the
// scan (hackathon features, X35):
//
//	touchup  (X35a)  Muse Image 1.0 makes a listing photo look better; Muse
//	                 Spark checks that the food did not change; the original
//	                 is always kept.
//	rescue   (X35b)  a listing that nobody claimed before its timer ran out:
//	                 the nearest food banks or meal programs (Feed America
//	                 directory) and a Muse Spark inquiry the donor sends.
//	fanout   (X35c)  a listing is posted to the Icarus Facebook Page (Muse
//	                 Spark writes the text); a claim edits the post.
//
// This file is what the three share. It copies the patterns of the analyze
// service (inference/): the bearer token and the rate limiter come from
// ../server, the session cookie is checked with the auth service, every
// request writes one fsync'd JSON line with its cost, and a daily cap stops
// spend. Each service has its own events folder and its own cap.
package post

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

// ---- error answers ---------------------------------------------------------

// Error codes shared by the three services (the "code" of a non-2xx answer).
const (
	CodeBadRequest   = "bad_request"
	CodeUnauthorized = "unauthorized"
	CodeMethod       = "method_not_allowed"
	CodeTooLarge     = "image_too_large"
	CodeUnsupported  = "unsupported_media"
	CodeNotFound     = "not_found"
	CodeConflict     = "conflict"
	CodeRateLimited  = "rate_limited"             // 429
	CodeUpstream     = "upstream_error"           // 502
	CodeDailyCap     = "daily_cap_reached"        // 503
	CodeCallerCap    = "caller_daily_cap_reached" // 429: X44a, this caller's share of the day is used
	CodeTimeout      = "timeout"                  // 504
	CodeInternal     = "internal_error"
	CodeDisabled     = "disabled" // 503: a needed setting is not configured
)

// ErrorAnswer is every non-2xx body (the analyze service's shape).
type ErrorAnswer struct {
	RequestID string `json:"request_id"`
	Error     string `json:"error"`
	Code      string `json:"code"`
}

// WriteJSON writes v with status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ---- who is calling --------------------------------------------------------

// SessionCookie is the app's session cookie (auth/auth.go; the same name as
// inference.SessionCookie).
const SessionCookie = "morpheus_session"

// User is whoever auth's /api/auth/me says owns the session cookie.
type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// Authenticator finds the signed-in caller.
type Authenticator interface {
	Who(r *http.Request) (User, bool)
}

// AuthClient asks the auth service (inference/clients.go, same rules).
type AuthClient struct {
	URL  string // http://auth:8081
	HTTP *http.Client
}

func (a *AuthClient) Who(r *http.Request) (User, bool) {
	ck, err := r.Cookie(SessionCookie)
	if err != nil || ck.Value == "" {
		return User{}, false
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, strings.TrimRight(a.URL, "/")+"/api/auth/me", nil)
	if err != nil {
		return User{}, false
	}
	req.AddCookie(ck)
	hc := a.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		log.Printf("auth lookup: %v", err)
		return User{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return User{}, false
	}
	var u User
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&u); err != nil || u.ID == "" {
		return User{}, false
	}
	return u, true
}

// Caller is the result of the auth step of a request.
type Caller struct {
	User    User   // empty for a bearer caller
	Bearer  bool   // the shared bearer token (plan A callers)
	RateKey string // the user id, or the client IP for a bearer caller
}

// Identify accepts the session cookie (preferred) or, when bearer is not
// nil, the shared bearer token. The second result is false when neither is
// valid.
func Identify(r *http.Request, session Authenticator, bearer *analyze.Bearer) (Caller, bool) {
	if session != nil {
		if u, ok := session.Who(r); ok {
			return Caller{User: u, RateKey: "u:" + u.ID}, true
		}
	}
	if bearer != nil && bearer.OK(r) {
		return Caller{Bearer: true, RateKey: "ip:" + ClientKey(r)}, true
	}
	return Caller{}, false
}

// TrustForwarded makes ClientKey use X-Forwarded-For. Set it (flag
// -trust-forwarded) only when the service is reached through Caddy and Caddy
// sets that header itself ({client_ip}, X31); a client can set it otherwise.
var TrustForwarded = false

// ClientKey is the socket peer, or the forwarded address when TrustForwarded.
func ClientKey(r *http.Request) string {
	remote, fwd := analyze.ClientIPs(r)
	if TrustForwarded && fwd != "" && r.Header.Get("X-Forwarded-For") != "" {
		return fwd
	}
	return remote
}

// ---- rate limit -----------------------------------------------------------

// Limiter is a fixed window per key (the same rule as analyze.Limiter), and
// it drops keys with no hit in the window, so random keys cannot grow it.
type Limiter struct {
	Burst  int
	Window time.Duration
	mu     sync.Mutex
	hits   map[string][]time.Time
	now    func() time.Time
	calls  int
}

func NewLimiter(burst int, window time.Duration) *Limiter {
	return &Limiter{Burst: burst, Window: window, hits: map[string][]time.Time{}, now: time.Now}
}

// Allow records one hit for key and reports whether it is within budget.
func (l *Limiter) Allow(key string) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if l.calls++; l.calls%1024 == 0 {
		for k, ts := range l.hits {
			if len(ts) == 0 || now.Sub(ts[len(ts)-1]) >= l.Window {
				delete(l.hits, k)
			}
		}
	}
	keep := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.Window {
			keep = append(keep, t)
		}
	}
	if len(keep) >= l.Burst {
		l.hits[key] = keep
		return false
	}
	l.hits[key] = append(keep, now)
	return true
}

// Keys is the number of keys held (tests).
func (l *Limiter) Keys() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.hits)
}

// ForwardCookie copies the caller's session cookie onto an internal request
// (the listings service learns the caller the same way).
func ForwardCookie(from, to *http.Request) {
	if ck, err := from.Cookie(SessionCookie); err == nil {
		to.AddCookie(ck)
	}
}

// ---- the listings service --------------------------------------------------

// Listing is the listings service's shape (listings/listing.go, frozen W0;
// R1 adds the optional details of X48d).
type Listing struct {
	ID              string  `json:"id"`
	OwnerID         string  `json:"owner_id"`
	Title           string  `json:"title"`
	NumberOfServing int     `json:"number_of_serving"`
	PhotoURL        string  `json:"photo_url"`
	PickupText      string  `json:"pickup_text"`
	Status          string  `json:"status"`
	CreatedAt       string  `json:"created_at"`
	ExpiresAt       string  `json:"expires_at"`
	CollectedBy     *string `json:"collected_by"`
	CollectedAt     *string `json:"collected_at"`
	RequestID       string  `json:"request_id"`
	// Details (R1): the scanner's fields that the listings service keeps with
	// the listing (X48d); absent for a listing posted without them.
	Details *ListingDetails `json:"details,omitempty"`
}

// ListingDetails (R1) are the two scanner fields of a listing (listings/details.go)
// that rescue reads; the other four are not decoded here.
type ListingDetails struct {
	Description   string `json:"description,omitempty"`
	MealCondition string `json:"meal_condition,omitempty"`
}

// Expired is true when the listing is still "active" but its timer ran out
// (the listings service's own rule: active means expires_at > now).
func (l Listing) Expired(now time.Time) bool {
	t, err := time.Parse(time.RFC3339Nano, l.ExpiresAt)
	return err == nil && !now.Before(t)
}

// ListingsClient reads the caller's own listings with the caller's cookie,
// so the listings service makes the ownership decision (no new route there).
type ListingsClient struct {
	URL  string // http://listings:8082
	HTTP *http.Client
}

var listingIDRe = regexp.MustCompile(`^l_[0-9a-f]{16}$`)

// ValidListingID is the listings service's id form (l_ + 16 hex).
func ValidListingID(id string) bool { return listingIDRe.MatchString(id) }

// ErrNotOwned is one message for "no such listing" and "not yours", so an
// id cannot be probed (OWASP API1:2023).
var ErrNotOwned = errors.New("listing not found")

func (c *ListingsClient) http() *http.Client {
	if c.HTTP == nil {
		return &http.Client{Timeout: 5 * time.Second}
	}
	return c.HTTP
}

// Mine finds listing id among the caller's own listings.
func (c *ListingsClient) Mine(r *http.Request, id string) (Listing, error) {
	if !ValidListingID(id) {
		return Listing{}, ErrNotOwned
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, strings.TrimRight(c.URL, "/")+"/api/listings/mine", nil)
	if err != nil {
		return Listing{}, err
	}
	ForwardCookie(r, req)
	resp, err := c.http().Do(req)
	if err != nil {
		return Listing{}, fmt.Errorf("listings: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Listing{}, fmt.Errorf("listings: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Items []Listing `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return Listing{}, fmt.Errorf("listings: %w", err)
	}
	for _, l := range body.Items {
		if l.ID == id {
			return l, nil
		}
	}
	return Listing{}, ErrNotOwned
}

// Photo reads the listing's photo with the caller's cookie.
func (c *ListingsClient) Photo(r *http.Request, id string) ([]byte, string, error) {
	if !ValidListingID(id) {
		return nil, "", ErrNotOwned
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, strings.TrimRight(c.URL, "/")+"/api/listings/"+id+"/photo", nil)
	if err != nil {
		return nil, "", err
	}
	ForwardCookie(r, req)
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("listings photo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("listings photo: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, MaxImage+1))
	if err != nil || len(b) == 0 || len(b) > MaxImage {
		return nil, "", errors.New("listings photo: unreadable or too large")
	}
	mime := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
	if mime == "" {
		mime = http.DetectContentType(b)
	}
	return b, mime, nil
}

// MaxImage is the largest photo the services read (the analyze limit, 5 MB).
const MaxImage = 5 << 20

// ---- Muse Spark (text and image in, JSON text out) -------------------------

// Meta Model API defaults (dev.meta.ai/docs, the same as inference/).
const (
	DefaultBase  = "https://api.meta.ai/v1"
	DefaultModel = "muse-spark-1.3"
	DefaultImage = "muse-image-1.0"
)

// Prices in USD (dev.meta.ai/docs/pricing-rate-limits, READ 2026-10-26, the
// same values as inference/pricing.go; Muse Image: $0.01 per image, Meta's
// Muse Image launch post, READ 2026-10-28).
const (
	PriceMuseInputPerM  = 1.25
	PriceMuseOutputPerM = 4.25
	PriceMuseCachedPerM = 0.15
	PriceImagePerImage  = 0.01
)

// Call is one paid API call, as the events line keeps it.
type Call struct {
	Purpose      string  `json:"purpose"`
	Model        string  `json:"model"`
	Status       int     `json:"status"`
	MS           int64   `json:"ms"`
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	CachedTokens int     `json:"cached_tokens,omitempty"`
	Images       int     `json:"images,omitempty"`
	AudioMS      int     `json:"audio_ms,omitempty"` // speech: billed per whole second
	CostUSD      float64 `json:"cost_usd"`
	UpstreamID   string  `json:"upstream_id,omitempty"`
	Error        string  `json:"error,omitempty"`
}

// Price sets c.CostUSD from its usage. A call that did not answer 2xx is free.
func (c *Call) Price() {
	if c.Status/100 != 2 {
		c.CostUSD = 0
		return
	}
	if c.Images > 0 {
		c.CostUSD = float64(c.Images) * PriceImagePerImage
		return
	}
	if c.AudioMS > 0 {
		// whole seconds, rounded UP (X36a): a part second is billed as one,
		// so the ledger and the cap never count less than the bill.
		c.CostUSD = float64((c.AudioMS+999)/1000) * PriceASRPerHour / 3600
		return
	}
	fresh := c.InputTokens - c.CachedTokens
	if fresh < 0 {
		fresh = 0
	}
	c.CostUSD = (float64(fresh)*PriceMuseInputPerM + float64(c.CachedTokens)*PriceMuseCachedPerM +
		float64(c.OutputTokens)*PriceMuseOutputPerM) / 1e6
}

// Muse calls POST <Base>/responses (dev.meta.ai/docs/protocols/responses).
type Muse struct {
	Base   string // https://api.meta.ai/v1
	Key    string
	Model  string
	Effort string // reasoning.effort; "low" (inference D12)
	HTTP   *http.Client
}

// Image is one input image for a Muse Spark call.
type Image struct {
	Bytes []byte
	Mime  string
}

// AskJSON sends prompt (and images, in order) and returns the first JSON
// object of the answer. The call is always returned, for the events line.
func (m *Muse) AskJSON(ctx context.Context, purpose, prompt string, images ...Image) (map[string]any, Call, error) {
	model := m.Model
	if model == "" {
		model = DefaultModel
	}
	call := Call{Purpose: purpose, Model: model}
	content := []any{map[string]any{"type": "input_text", "text": prompt}}
	for _, im := range images {
		content = append(content, map[string]any{"type": "input_image",
			"image_url": "data:" + im.Mime + ";base64," + b64(im.Bytes)})
	}
	body := map[string]any{"model": model,
		"input": []any{map[string]any{"type": "message", "role": "user", "content": content}}}
	if m.Effort != "" {
		body["reasoning"] = map[string]any{"effort": m.Effort}
	}
	b, _ := json.Marshal(body)
	base := m.Base
	if base == "" {
		base = DefaultBase
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/responses", bytes.NewReader(b))
	if err != nil {
		return nil, call, err
	}
	req.Header.Set("Authorization", "Bearer "+m.Key)
	req.Header.Set("Content-Type", "application/json")
	hc := m.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	t0 := time.Now()
	resp, err := hc.Do(req)
	if err != nil {
		call.MS, call.Error = time.Since(t0).Milliseconds(), err.Error()
		return nil, call, fmt.Errorf("muse: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	call.MS, call.Status = time.Since(t0).Milliseconds(), resp.StatusCode
	if err != nil {
		call.Error = err.Error()
		return nil, call, fmt.Errorf("muse read: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		call.Error = fmt.Sprintf("HTTP %d %s", resp.StatusCode, clip(string(raw), 300))
		return nil, call, fmt.Errorf("muse: %s", call.Error)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		call.Error = "bad JSON"
		return nil, call, fmt.Errorf("muse: bad JSON: %w", err)
	}
	call.UpstreamID, _ = out["id"].(string)
	usage(out, &call)
	call.Price()
	obj, err := analyze.ExtractJSON(analyze.ResponseText(out))
	if err != nil {
		call.Error = "no JSON object in the answer"
		return nil, call, fmt.Errorf("muse: %w", err)
	}
	return obj, call, nil
}

func usage(resp map[string]any, c *Call) {
	um, _ := resp["usage"].(map[string]any)
	if v, ok := um["input_tokens"].(float64); ok {
		c.InputTokens = int(v)
	}
	if v, ok := um["output_tokens"].(float64); ok {
		c.OutputTokens = int(v)
	}
	if d, ok := um["input_tokens_details"].(map[string]any); ok {
		if v, ok := d["cached_tokens"].(float64); ok {
			c.CachedTokens = int(v)
		}
	}
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// Str returns obj[k] as a trimmed string ("" when absent or not a string).
func Str(obj map[string]any, k string) string {
	s, _ := obj[k].(string)
	return strings.TrimSpace(s)
}

// ---- events and the daily cap ----------------------------------------------

// Event is one request (every exit), as one JSON line.
type Event struct {
	RequestID     string         `json:"request_id"`
	TS            string         `json:"ts"`
	Service       string         `json:"service"`
	Build         string         `json:"build"`
	Route         string         `json:"route"`
	UserID        string         `json:"user_id,omitempty"`
	Bearer        bool           `json:"bearer,omitempty"`
	RemoteIP      string         `json:"remote_ip,omitempty"`    // the socket peer (inference/events.go)
	ForwardedIP   string         `json:"forwarded_ip,omitempty"` // CF-Connecting-IP or X-Forwarded-For[0]
	ListingID     string         `json:"listing_id,omitempty"`
	Status        int            `json:"status"`
	Code          string         `json:"code,omitempty"`
	Error         string         `json:"error,omitempty"`
	LatencyMS     int64          `json:"latency_ms"`
	CostUSD       float64        `json:"cost_usd"`
	SpentTodayUSD float64        `json:"spent_today_usd"`
	CapKey        string         `json:"cap_key,omitempty"` // X44a: the caller's daily-cap key (Caller.RateKey)
	Calls         []Call         `json:"calls,omitempty"`
	Detail        map[string]any `json:"detail,omitempty"`
}

// From sets the caller's addresses from r (server ClientIPs, as inference).
func (ev *Event) From(r *http.Request) {
	ev.RemoteIP, ev.ForwardedIP = analyze.ClientIPs(r)
}

// Events appends one line per request with fsync, so the spend in the file
// is the spend the budget restores after a restart (inference/events.go).
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
	if err := os.MkdirAll(e.Dir, 0o755); err != nil {
		return err
	}
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

// SpentOn sums cost_usd over the lines of day t.
func (e *Events) SpentOn(t time.Time) float64 {
	s, _ := e.SpentByKey(t)
	return s
}

// SpentByKey sums cost_usd over the lines of day t: the total, and the part
// of each cap_key (X44a). A line written before X44a has no cap_key: it
// counts in the total only.
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

// Budget is a daily spend cap per UTC day (inference/events.go Budget: a
// request reserves Estimate before it runs and settles at its real cost).
//
// X44a (security audit S1 item 2): PerCaller is a second cap for each caller
// key (Caller.RateKey: one signed-in user, or one bearer address), so one
// caller cannot use the whole day of a feature for every caller. 0 = off.
type Budget struct {
	Cap       float64
	Estimate  float64
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
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollLocked()
	return b.byKey[key]
}

// Spent is today's settled spend.
func (b *Budget) Spent() float64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollLocked()
	return b.spent
}

// Hold is one request's reservation.
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
// and why: CodeDailyCap (the service's day is used) or CodeCallerCap (this
// key's share is used: key's spent + held + Estimate > PerCaller).
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

// CapRefusal is the answer to a refused reservation: 503 for the service's
// cap (the feature is off for everyone until 00:00 UTC), 429 for a caller's
// own cap (RFC 6585 4: too many requests from this caller).
func CapRefusal(why string) (status int, code, msg string) {
	if why == CodeCallerCap {
		return http.StatusTooManyRequests, CodeCallerCap, "your daily limit for this feature is reached; try again after 00:00 UTC"
	}
	return http.StatusServiceUnavailable, CodeDailyCap, "daily spend cap reached"
}

// Settle frees the hold and adds the real cost (once).
func (h *Hold) Settle(cost float64) {
	if h == nil {
		return
	}
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

// Sum is the total cost of calls.
func Sum(calls []Call) float64 {
	s := 0.0
	for _, c := range calls {
		s += c.CostUSD
	}
	return s
}

// ---- small helpers ---------------------------------------------------------

// ReadSecret reads a secret file (a read-only mount), never a flag or env.
func ReadSecret(path, what string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", what, err)
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", fmt.Errorf("%s: %s is empty", what, path)
	}
	return s, nil
}

// WriteFileSync writes data to path through a temporary file, fsync, rename.
func WriteFileSync(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
