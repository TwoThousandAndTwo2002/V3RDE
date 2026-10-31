package analyze

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// promptSHA256 is the sha256 of the prompt the eval and the app were
// measured with (the analyze server of the app before the Hackathon).
const promptSHA256 = "5b01039918cb2fd033b4d5896d6bee239fcf962f76254dfb9ecc396ef78ff4eb"

// A contract answer the way Muse Spark gives it (words around the object).
const answer = "Here it is:\n```json\n" + `{"name": "Vegetable Lasagna", "description": "Baked pasta layers with vegetables and cheese.", "is_non_veg": 0, "number_of_serving": 6, "serving_type": "bulk", "meal_condition": "ready_to_eat", "weight": 1.2, "meal_cost": 14.5}` + "\n```"

// metaFake answers like POST /v1/responses: a reasoning item, a message
// item with output_text, a usage block. It keeps the last body it got.
type metaFake struct {
	*httptest.Server
	mu     sync.Mutex
	status int
	text   string
	body   map[string]any
	calls  int
}

// set changes the next answers.
func (f *metaFake) set(status int, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status, f.text = status, text
}

// seen is the number of calls and the last body.
func (f *metaFake) seen() (int, map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.body
}

func newMetaFake(t *testing.T, status int, text string) *metaFake {
	t.Helper()
	f := &metaFake{status: status, text: text}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls++
		if r.Header.Get("Authorization") != "Bearer key-1" || r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.body = nil
		_ = json.NewDecoder(r.Body).Decode(&f.body)
		if f.status != http.StatusOK {
			w.WriteHeader(f.status)
			fmt.Fprint(w, `{"error":{"message":"slow down"}}`)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "resp_9", "status": "completed",
			"output": []any{
				map[string]any{"type": "reasoning", "content": []any{}},
				map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": f.text}}},
			},
			"usage": map[string]any{"input_tokens": 300, "output_tokens": 900,
				"output_tokens_details": map[string]any{"reasoning_tokens": 800}},
		})
	}))
	t.Cleanup(f.Close)
	return f
}

type rig struct {
	h    http.Handler
	meta *metaFake
	dir  string
}

func newRig(t *testing.T, status int, text string, burst int) rig {
	t.Helper()
	meta := newMetaFake(t, status, text)
	dir := t.TempDir()
	h := New(Config{
		Upstream: &Upstream{URL: meta.URL, Key: "key-1", Model: "muse-spark-1.3"},
		Auth:     NewBearer("app-token"),
		Limiter:  NewLimiter(burst, time.Minute),
		Recorder: NewRecorder(dir),
		Origin:   "https://app.example",
		MaxImage: 2048,
	})
	return rig{h: h, meta: meta, dir: dir}
}

// png8 is the start of a PNG file: http.DetectContentType finds image/png.
var png8 = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func formReq(t *testing.T, field string, img []byte, partType string) *http.Request {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	hdr := map[string][]string{"Content-Disposition": {fmt.Sprintf(`form-data; name=%q; filename="p.png"`, field)}}
	if partType != "" {
		hdr["Content-Type"] = []string{partType}
	}
	pw, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatal(err)
	}
	pw.Write(img)
	mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/v1/analyze", &b)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("Authorization", "Bearer app-token")
	r.RemoteAddr = "192.0.2.10:40000"
	return r
}

func jsonReq(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	r.Header.Set("Authorization", "Bearer app-token")
	r.RemoteAddr = "192.0.2.11:40000"
	return r
}

func serve(h http.Handler, r *http.Request) (*httptest.ResponseRecorder, map[string]any) {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return w, m
}

func metricLines(t *testing.T, dir string) []Line {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "metrics-*.jsonl"))
	var out []Line
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			var l Line
			if err := json.Unmarshal([]byte(s), &l); err != nil {
				t.Fatalf("bad line %q: %v", s, err)
			}
			out = append(out, l)
		}
	}
	return out
}

// ---- the contract ---------------------------------------------------------

// The prompt is the contract: its bytes must not change. The hash is of the
// text the eval and the app were measured with.
func TestPromptBytesArePinned(t *testing.T) {
	sum := sha256.Sum256([]byte(Prompt))
	if got := hex.EncodeToString(sum[:]); got != promptSHA256 {
		t.Fatalf("the prompt changed: sha256 %s", got)
	}
}

func TestExtractJSON(t *testing.T) {
	for _, tc := range []struct {
		in   string
		ok   bool
		name string
	}{
		{answer, true, "Vegetable Lasagna"},
		{`{"name":"a"}`, true, "a"},
		{"no object here", false, ""},
		{"} before {", false, ""},
		{"{ not json }", false, ""},
		{`{"a":1} and {"b":2}`, false, ""}, // first '{' to last '}' is not one object
		{"", false, ""},
	} {
		m, err := ExtractJSON(tc.in)
		if (err == nil) != tc.ok {
			t.Errorf("%q: err %v", tc.in, err)
			continue
		}
		if tc.ok && m["name"] != tc.name {
			t.Errorf("%q: name %v", tc.in, m["name"])
		}
	}
}

// TestExtractResultsList: the T6 answer shape (2026-10-31, a list of 3 objects
// on the Sosa drink + beans + coffee photo) gives 3 results in order.
func TestExtractResultsList(t *testing.T) {
	obj := func(name string) string {
		return `{"name": "` + name + `", "description": "Sealed can of red kidney beans.", "is_non_veg": 0, "number_of_serving": 2, "serving_type": "individual", "meal_condition": "sealed", "weight": 0.4, "meal_cost": 3}`
	}
	text := "[" + obj("Sosa Mixed Berries Fruit Drink") + ", " + obj("Sunripe Red Kidney Beans") + ", " + obj("Beanies Irish Coffee") + "]"
	if _, err := ExtractJSON(text); err == nil {
		t.Fatal("the old reader accepted the list; the test does not show the bug")
	}
	rs, err := ExtractResults("```json\n" + text + "\n```")
	if err != nil || len(rs) != 3 {
		t.Fatalf("got %d results, err %v", len(rs), err)
	}
	if rs[0].Name != "Sosa Mixed Berries Fruit Drink" || rs[2].Name != "Beanies Irish Coffee" || rs[1].MealCondition != "sealed" {
		t.Fatalf("results %+v", rs)
	}
	// one bad object is dropped, the good ones stay
	rs, err = ExtractResults("[" + obj("A") + `, {"name": "B"}, 7]`)
	if err != nil || len(rs) != 1 || rs[0].Name != "A" {
		t.Fatalf("mixed list: %d results, err %v", len(rs), err)
	}
	// no valid object: the first object's reason
	if _, err = ExtractResults(`[{"name": "B"}]`); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("all bad: err %v", err)
	}
	// X49r review: words with brackets around the list, a second value after it, an "items" wrapper
	two := "[" + obj("A") + ", " + obj("B") + "]"
	for _, in := range []string{
		"Here are the 2 items [see below]:\n" + two,
		two + "\nNote: weights are estimates [approx].",
		"```json\n" + two + "\n```\n```json\n" + two + "\n```",
		`{"items": ` + two + `}`,
	} {
		if rs, err := ExtractResults(in); err != nil || len(rs) != 2 || rs[0].Name != "A" || rs[1].Name != "B" {
			t.Errorf("%.40q: %d results, err %v", in, len(rs), err)
		}
	}
	for _, in := range []string{"[]", "[1, 2]", "[ not json ]", "[ no end", `{"items": []}`, `{"items": [{"name": "B"}]}`} {
		if _, err := ExtractResults(in); err == nil {
			t.Errorf("%q: no error", in)
		}
	}
	// at most MaxListItems
	many := make([]string, MaxListItems+3)
	for i := range many {
		many[i] = obj("x")
	}
	if rs, _ := ExtractResults("[" + strings.Join(many, ",") + "]"); len(rs) != MaxListItems {
		t.Fatalf("%d results, want %d", len(rs), MaxListItems)
	}
}

// TestExtractResultsObject: one object (also with a '[' inside a string after
// the first '{') reads as before.
func TestExtractResultsObject(t *testing.T) {
	rs, err := ExtractResults(answer)
	if err != nil || len(rs) != 1 || rs[0].Name != "Vegetable Lasagna" {
		t.Fatalf("got %+v, err %v", rs, err)
	}
	in := `{"name": "Rice [brown]", "description": "d", "is_non_veg": 0, "number_of_serving": 1, "serving_type": "bulk", "meal_condition": "sealed", "weight": 1, "meal_cost": 2}`
	if rs, err := ExtractResults(in); err != nil || rs[0].Name != "Rice [brown]" {
		t.Fatalf("bracket in a string: %+v, err %v", rs, err)
	}
	if rs, err := ExtractResults("Item [1]: " + in); err != nil || len(rs) != 1 {
		t.Fatalf("a '[' before the object: %+v, err %v", rs, err)
	}
	if _, err := ExtractResults(`{"name": "a"}`); err == nil {
		t.Fatal("a half object passed")
	}
	if _, err := ExtractResults("no json"); err == nil {
		t.Fatal("no JSON passed")
	}
}

func good() map[string]any {
	m, _ := ExtractJSON(answer)
	return m
}

func TestValidateGood(t *testing.T) {
	r, err := Validate(good())
	if err != nil {
		t.Fatal(err)
	}
	want := Result{Name: "Vegetable Lasagna", Description: "Baked pasta layers with vegetables and cheese.",
		IsNonVeg: 0, NumberOfServing: 6, ServingType: "bulk", MealCondition: "ready_to_eat", Weight: 1.2, MealCost: 14.5}
	if r != want {
		t.Fatalf("got %+v", r)
	}
}

func TestValidateRefusals(t *testing.T) {
	for _, tc := range []struct {
		key  string
		val  any
		want string
	}{
		{"name", 3.0, "contract: name is not a string"},
		{"description", nil, "contract: description is not a string"},
		{"is_non_veg", 2.0, "contract: is_non_veg is not 0 or 1"},
		{"is_non_veg", true, "contract: is_non_veg is not 0 or 1"}, // a JSON bool is never a number
		{"number_of_serving", "4", "contract: number_of_serving is not a number"},
		{"serving_type", "family", "contract: serving_type is not individual|bulk"},
		{"serving_type", 1.0, "contract: serving_type is not individual|bulk"},
		{"meal_condition", "frozen", "contract: meal_condition is not ready_to_eat|cooking_required|sealed"},
		{"weight", false, "contract: weight is not a number"},
		{"meal_cost", "free", "contract: meal_cost is not a number"},
	} {
		m := good()
		m[tc.key] = tc.val
		if _, err := Validate(m); err == nil || err.Error() != tc.want {
			t.Errorf("%s=%v: got %v, want %q", tc.key, tc.val, err, tc.want)
		}
	}
}

func TestValidateMissingKeysInContractOrder(t *testing.T) {
	m := good()
	delete(m, "weight")
	delete(m, "name")
	delete(m, "meal_condition")
	_, err := Validate(m)
	if err == nil || err.Error() != "contract: missing name,meal_condition,weight" {
		t.Fatalf("got %v", err)
	}
	if _, err := Validate(map[string]any{}); err == nil || !strings.HasPrefix(err.Error(), "contract: missing name,description,") {
		t.Fatalf("empty object: %v", err)
	}
}

// ---- access ---------------------------------------------------------------

func TestBearer(t *testing.T) {
	req := func(h string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		if h != "" {
			r.Header.Set("Authorization", h)
		}
		return r
	}
	b := NewBearer("  s3cret \n")
	for h, want := range map[string]bool{"Bearer s3cret": true, "Bearer  s3cret ": true, "Bearer s3cre": false,
		"bearer s3cret": false, "s3cret": false, "": false, "Basic s3cret": false} {
		if b.OK(req(h)) != want {
			t.Errorf("%q: want %v", h, want)
		}
	}
	if NewBearer("").OK(req("Bearer ")) || NewBearer(" ").OK(req("Bearer  ")) {
		t.Fatal("an empty token must never pass")
	}
	var none *Bearer
	if none.OK(req("Bearer s3cret")) {
		t.Fatal("a nil bearer must never pass")
	}
}

func TestLimiterSlidingWindow(t *testing.T) {
	l := NewLimiter(2, time.Minute)
	now := time.Unix(5000, 0)
	l.now = func() time.Time { return now }
	if !l.Allow("a") || !l.Allow("a") || l.Allow("a") {
		t.Fatal("the third hit in the window must be refused")
	}
	if !l.Allow("b") {
		t.Fatal("keys are counted apart")
	}
	now = now.Add(59 * time.Second)
	if l.Allow("a") {
		t.Fatal("still in the window")
	}
	now = now.Add(2 * time.Second)
	if !l.Allow("a") {
		t.Fatal("the window must slide")
	}
}

func TestLimiterPrunesIdleKeys(t *testing.T) {
	l := NewLimiter(1, time.Minute)
	now := time.Unix(5000, 0)
	l.now = func() time.Time { return now }
	for i := 0; i < pruneAt; i++ {
		l.Allow(fmt.Sprintf("10.0.%d.%d", i/256, i%256))
	}
	if l.keys() != pruneAt {
		t.Fatalf("keys %d", l.keys())
	}
	now = now.Add(2 * time.Minute)
	l.Allow("10.9.9.9") // a new key at the limit: the idle keys go
	if l.keys() != 1 {
		t.Fatalf("idle keys kept: %d", l.keys())
	}
	if !l.Allow("10.0.0.0") {
		t.Fatal("a pruned key starts again")
	}
}

// The review of X38a: a key with old and recent hits keeps only its recent
// hits after a prune, each one time.
func TestLimiterPruneKeepsLiveHits(t *testing.T) {
	l := NewLimiter(2, time.Minute)
	now := time.Unix(5000, 0)
	l.now = func() time.Time { return now }
	l.Allow("a") // t=0
	now = now.Add(50 * time.Second)
	l.Allow("a") // t=50s
	for i := 1; i < pruneAt; i++ {
		l.Allow(fmt.Sprintf("k%d", i))
	}
	now = now.Add(11 * time.Second) // t=61s: the first hit of a is old
	l.Allow("new")                  // a new key at the limit: prune
	if !l.Allow("a") {
		t.Fatal("a has one hit in the window: a second one is allowed")
	}
	if l.Allow("a") {
		t.Fatal("a now has two hits in the window")
	}
}

// Many live keys: the map is scanned at most one time per window.
func TestLimiterPrunesAtMostOncePerWindow(t *testing.T) {
	l := NewLimiter(1, time.Minute)
	now := time.Unix(5000, 0)
	l.now = func() time.Time { return now }
	for i := 0; i < pruneAt; i++ {
		l.Allow(fmt.Sprintf("k%d", i))
	}
	l.Allow("first") // prune 1: nothing to remove (all keys live)
	first := l.pruned
	now = now.Add(time.Second)
	for i := 0; i < 100; i++ {
		l.Allow(fmt.Sprintf("more%d", i))
	}
	if !l.pruned.Equal(first) {
		t.Fatal("pruned again inside the window")
	}
	now = now.Add(time.Minute)
	l.Allow("later")
	if l.pruned.Equal(first) || l.keys() != 1 {
		t.Fatalf("no prune after the window: keys %d", l.keys())
	}
}

func TestClientIPs(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = "172.18.0.5:51234"
	if a, b := ClientIPs(r); a != "172.18.0.5" || b != "" {
		t.Fatalf("plain: %q %q", a, b)
	}
	r.Header.Set("X-Forwarded-For", " 198.51.100.7 , 172.18.0.1")
	if _, b := ClientIPs(r); b != "198.51.100.7" {
		t.Fatalf("forwarded: %q", b)
	}
	r.Header.Set("CF-Connecting-IP", "203.0.113.4")
	if _, b := ClientIPs(r); b != "203.0.113.4" {
		t.Fatalf("CF-Connecting-IP comes first: %q", b)
	}
	r.RemoteAddr = "pipe"
	if a, _ := ClientIPs(r); a != "pipe" {
		t.Fatalf("no port: %q", a)
	}
}

func TestRequestIDs(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id := NewRequestID()
		if _, err := hex.DecodeString(id); err != nil || len(id) != 32 || seen[id] {
			t.Fatalf("id %q", id)
		}
		seen[id] = true
	}
}

// ---- the Meta call --------------------------------------------------------

func TestRequestBody(t *testing.T) {
	u := &Upstream{Model: "muse-spark-1.3"}
	b := u.Request([]byte("abc"), "image/png")
	raw, _ := json.Marshal(b)
	for _, s := range []string{`"model":"muse-spark-1.3"`, `"role":"user"`, `"type":"input_text"`,
		`"image_url":"data:image/png;base64,` + base64.StdEncoding.EncodeToString([]byte("abc")) + `"`} {
		if !strings.Contains(string(raw), s) {
			t.Errorf("body lacks %s", s)
		}
	}
	if _, ok := b["reasoning"]; ok {
		t.Fatal("no effort = no reasoning block")
	}
	u.Effort = "low"
	if r, _ := u.Request(nil, "image/png")["reasoning"].(map[string]any); r["effort"] != "low" {
		t.Fatal("effort not sent")
	}
}

func TestResponseText(t *testing.T) {
	if s := ResponseText(map[string]any{"output_text": "a", "output": []any{}}); s != "a" {
		t.Fatalf("output_text: %q", s)
	}
	resp := map[string]any{"output": []any{
		"not an item",
		map[string]any{"content": []any{map[string]any{"text": "one"}, map[string]any{"type": "x"}}},
		map[string]any{"content": []any{map[string]any{"text": "two"}}},
	}}
	if s := ResponseText(resp); s != "one\ntwo" {
		t.Fatalf("joined: %q", s)
	}
	if s := ResponseText(map[string]any{}); s != "" {
		t.Fatalf("empty: %q", s)
	}
}

func TestAnalyzeCall(t *testing.T) {
	meta := newMetaFake(t, http.StatusOK, answer)
	u := &Upstream{URL: meta.URL, Key: "key-1", Model: "muse-spark-1.3"}
	a, err := u.Analyze(context.Background(), png8, "image/png")
	if err != nil || !a.Valid || a.Result.Name != "Vegetable Lasagna" || a.RequestID != "resp_9" || a.Status != 200 {
		t.Fatalf("answer %+v err %v", a, err)
	}
	if a.Usage != (Usage{InputTokens: 300, OutputTokens: 900, ReasoningTokens: 800}) {
		t.Fatalf("usage %+v", a.Usage)
	}

	meta.set(http.StatusTooManyRequests, answer)
	if _, err := u.Analyze(context.Background(), png8, "image/png"); err == nil ||
		!strings.HasPrefix(err.Error(), "upstream: HTTP 429 ") {
		t.Fatalf("429: %v", err)
	}
	meta.set(http.StatusOK, "I cannot see food.")
	a, err = u.Analyze(context.Background(), png8, "image/png")
	if err == nil || err.Error() != "contract: contract: not a JSON object" || a.Valid {
		t.Fatalf("no object: %v", err)
	}
	meta.set(http.StatusOK, `{"name":"x"}`)
	if _, err := u.Analyze(context.Background(), png8, "image/png"); err == nil ||
		!strings.HasPrefix(err.Error(), "contract: missing description,") {
		t.Fatalf("missing keys: %v", err)
	}
	u.Key = "wrong"
	if _, err := u.Analyze(context.Background(), png8, "image/png"); err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("bad key: %v", err)
	}
}

func TestAnalyzeCallTransportError(t *testing.T) {
	u := &Upstream{URL: "http://127.0.0.1:1/v1/responses", Key: "key-1", Model: "m"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := u.Analyze(ctx, png8, "image/png"); err == nil || !strings.HasPrefix(err.Error(), "upstream: ") {
		t.Fatalf("got %v", err)
	}
}

// ---- the handler, end to end against the fake Meta API --------------------

func TestAnalyzeAnswersWithOneServing(t *testing.T) {
	g := newRig(t, http.StatusOK, answer, 5)
	w, body := serve(g.h, formReq(t, "image", png8, "image/png"))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d %s", w.Code, w.Body)
	}
	if body["number_of_serving"] != 1.0 || body["name"] != "Vegetable Lasagna" || body["serving_type"] != "bulk" ||
		body["model"] != "muse-spark-1.3" || body["request_id"] != w.Header().Get("X-Request-Id") {
		t.Fatalf("body %v", body)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "https://app.example" {
		t.Fatal("no CORS header")
	}
	if _, mb := g.meta.seen(); mb["model"] != "muse-spark-1.3" {
		t.Fatalf("Meta body %v", mb)
	}
	ls := metricLines(t, g.dir)
	if len(ls) != 1 {
		t.Fatalf("lines %d", len(ls))
	}
	l := ls[0]
	if l.Status != 200 || !l.SchemaValid || l.Name != "Vegetable Lasagna" || l.UpstreamID != "resp_9" ||
		l.ImageBytes != len(png8) || l.Mime != "image/png" || l.RemoteIP != "192.0.2.10" || l.ReasoningTokens != 800 {
		t.Fatalf("line %+v", l)
	}
}

func TestAnalyzeJSONBodyAndDetectedType(t *testing.T) {
	g := newRig(t, http.StatusOK, answer, 5)
	w, body := serve(g.h, jsonReq(`{"image_base64":"`+base64.StdEncoding.EncodeToString(png8)+`"}`))
	if w.Code != http.StatusOK || body["name"] != "Vegetable Lasagna" {
		t.Fatalf("status %d %v", w.Code, body)
	}
	if l := metricLines(t, g.dir)[0]; l.Mime != "image/png" {
		t.Fatalf("detected type %q", l.Mime)
	}
}

func TestAnalyzeRefusals(t *testing.T) {
	g := newRig(t, http.StatusOK, answer, 100)
	big := append(append([]byte{}, png8...), make([]byte, 2048)...)
	noAuth := formReq(t, "image", png8, "image/png")
	noAuth.Header.Del("Authorization")
	get := httptest.NewRequest(http.MethodGet, "/v1/analyze", nil)
	text := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader("x"))
	text.Header.Set("Content-Type", "text/plain")
	text.Header.Set("Authorization", "Bearer app-token")
	for _, tc := range []struct {
		name string
		r    *http.Request
		code int
		msg  string
	}{
		{"get", get, 405, "POST only"},
		{"no token", noAuth, 401, "missing or bad bearer token"},
		{"text body", text, 415, "send multipart/form-data or application/json"},
		{"no image field", formReq(t, "photo", png8, "image/png"), 400, "multipart field 'image' missing"},
		{"pdf", formReq(t, "image", []byte("%PDF-1.7"), "application/pdf"), 415, "unsupported image type application/pdf"},
		{"too large", formReq(t, "image", big, "image/png"), 413, "image too large"},
		{"empty", jsonReq(`{"image_base64":"","mime":"image/jpeg"}`), 400, "empty image"},
		{"bad base64", jsonReq(`{"image_base64":"%%%","mime":"image/png"}`), 400, "image_base64 is not base64"},
		{"bad json", jsonReq(`{"image_base64":`), 400, "bad JSON body"},
	} {
		w, body := serve(g.h, tc.r)
		if id, _ := body["request_id"].(string); w.Code != tc.code || body["error"] != tc.msg || len(id) != 32 {
			t.Errorf("%s: %d %v", tc.name, w.Code, body)
		}
	}
	if n, _ := g.meta.seen(); n != 0 {
		t.Fatalf("a refused photo must not reach Meta (%d calls)", n)
	}
	if n := len(metricLines(t, g.dir)); n != 9 {
		t.Fatalf("every refusal writes a line: %d", n)
	}
}

func TestAnalyzePreflight(t *testing.T) {
	g := newRig(t, http.StatusOK, answer, 5)
	w, _ := serve(g.h, httptest.NewRequest(http.MethodOptions, "/v1/analyze", nil))
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Methods") != "POST, OPTIONS" {
		t.Fatalf("preflight %d %v", w.Code, w.Header())
	}
	if len(metricLines(t, g.dir)) != 0 {
		t.Fatal("a preflight writes no line")
	}
}

func TestAnalyzeRateLimitUsesForwardedAddress(t *testing.T) {
	g := newRig(t, http.StatusOK, answer, 2)
	send := func(fwd string) int {
		r := formReq(t, "image", png8, "image/png")
		r.Header.Set("X-Forwarded-For", fwd)
		w, _ := serve(g.h, r)
		return w.Code
	}
	if send("198.51.100.1") != 200 || send("198.51.100.1") != 200 || send("198.51.100.1") != 429 {
		t.Fatal("the third request of one address must be 429")
	}
	if send("198.51.100.2") != 200 {
		t.Fatal("another address behind the same proxy has its own budget")
	}
}

func TestAnalyzeMetaFailureIs502(t *testing.T) {
	g := newRig(t, http.StatusInternalServerError, answer, 5)
	w, body := serve(g.h, formReq(t, "image", png8, "image/png"))
	if w.Code != http.StatusBadGateway || !strings.HasPrefix(body["error"].(string), "upstream: HTTP 500") {
		t.Fatalf("%d %v", w.Code, body)
	}
	g.meta.set(http.StatusOK, `{"name":"Soup","description":"Hot.","is_non_veg":0}`)
	w, body = serve(g.h, formReq(t, "image", png8, "image/png"))
	if w.Code != http.StatusBadGateway || !strings.HasPrefix(body["error"].(string), "contract: missing number_of_serving") {
		t.Fatalf("%d %v", w.Code, body)
	}
	if _, half := body["name"]; half {
		t.Fatal("a failed contract returns no fields")
	}
	ls := metricLines(t, g.dir)
	if l := ls[len(ls)-1]; l.Status != 502 || l.SchemaValid || l.UpstreamStatus != 200 {
		t.Fatalf("line %+v", l)
	}
}

func TestAnalyzeTimeout(t *testing.T) {
	done := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-done:
		}
	}))
	defer slow.Close()
	defer close(done) // runs first: the fake handler ends, then the fake closes
	h := New(Config{Upstream: &Upstream{URL: slow.URL, Key: "k", Model: "m"}, Auth: NewBearer("app-token"),
		Timeout: 100 * time.Millisecond})
	start := time.Now()
	w, body := serve(h, formReq(t, "image", png8, "image/png"))
	if w.Code != http.StatusBadGateway || !strings.Contains(body["error"].(string), "deadline exceeded") {
		t.Fatalf("%d %v", w.Code, body)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("the request did not stop at the timeout")
	}
}

func TestHealthzNeedsNoToken(t *testing.T) {
	g := newRig(t, http.StatusOK, answer, 5)
	w, body := serve(g.h, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Code != 200 || body["ok"] != true || body["model"] != "muse-spark-1.3" {
		t.Fatalf("%d %v", w.Code, body)
	}
}

func TestMetricsHaveNoSecrets(t *testing.T) {
	g := newRig(t, http.StatusOK, answer, 5)
	r := formReq(t, "image", png8, "image/png")
	r.Header.Set("X-Forwarded-For", "198.51.100.3")
	if w, _ := serve(g.h, r); w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	files, err := filepath.Glob(filepath.Join(g.dir, "metrics-*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("files %v %v", files, err)
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"forwarded_ip":"198.51.100.3"`)) {
		t.Fatalf("the line lacks the forwarded address: %s", b)
	}
	for _, s := range []string{"key-1", "app-token", "Bearer"} {
		if bytes.Contains(b, []byte(s)) {
			t.Fatalf("the metrics line has %q", s)
		}
	}
}

func TestRecorderOneFilePerUTCDay(t *testing.T) {
	dir := t.TempDir()
	rc := NewRecorder(filepath.Join(dir, "new"))
	now := time.Date(2026, 10, 28, 23, 59, 0, 0, time.FixedZone("ET", -4*3600)) // 03:59 UTC on the 29th
	rc.now = func() time.Time { return now }
	if err := rc.Write(Line{RequestID: "a"}); err != nil {
		t.Fatal(err)
	}
	if filepath.Base(rc.Path()) != "metrics-20261029.jsonl" {
		t.Fatalf("path %s", rc.Path())
	}
	now = now.Add(24 * time.Hour)
	for _, id := range []string{"b", "c"} {
		if err := rc.Write(Line{RequestID: id}); err != nil {
			t.Fatal(err)
		}
	}
	ls := metricLines(t, filepath.Join(dir, "new"))
	if len(ls) != 3 {
		t.Fatalf("lines %d", len(ls))
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "new", "*.jsonl")); len(files) != 2 {
		t.Fatalf("files %v", files)
	}
	bad := NewRecorder(filepath.Join(dir, "new", "metrics-20261029.jsonl", "x")) // a file is in the way
	if err := bad.Write(Line{}); err == nil {
		t.Fatal("a write that cannot happen must return an error")
	}
}
