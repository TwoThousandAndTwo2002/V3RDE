package inference

// X32a: the one route. Every exit, the answer shape, the item photos, the
// dedup and the ledger line.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	_ "image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

// fakeGate answers /v1/gate with a fixed verdict (status 0 = 200).
func fakeGate(t *testing.T, verdict string, status int, calls *int) *httptest.Server {
	var mu sync.Mutex
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*calls++
		mu.Unlock()
		if r.URL.Path != "/v1/gate" || !strings.HasPrefix(r.Header.Get("Content-Type"), "image/") {
			t.Errorf("gate got %s %s", r.URL.Path, r.Header.Get("Content-Type"))
		}
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"verdict": verdict, "food": verdict == "FOOD", "p": 0.9})
	}))
	t.Cleanup(s.Close)
	return s
}

// threeBoxes: SAM finds 3 boxes; Muse names them names[0..2], box 2 the
// same product as box 1 (so "Soup" x2 + "Beans" is 2 items).
func threeBoxes(t *testing.T, names ...string) *fakeAPI {
	if len(names) == 0 {
		names = []string{"Soup", "Soup", "Beans"}
	}
	return &fakeAPI{sam: func(string) (int, string) {
		return 200, samLine(t, [4]int{0, 0, 199, 299}, [4]int{300, 0, 499, 299}, [4]int{600, 0, 799, 299})
	}, muse: func(string, map[string]any) map[string]any {
		return textResp("m", itemsText(ent(1, nil, names[0]), ent(2, 1.0, names[1]), ent(3, nil, names[2])))
	}}
}

// oneBox: SAM finds 1 box, so the engine makes analyze's whole-photo call.
func oneBox(t *testing.T, name string) *fakeAPI {
	return &fakeAPI{sam: func(string) (int, string) { return 200, samLine(t, [4]int{100, 100, 499, 499}) },
		muse: func(prompt string, _ map[string]any) map[string]any {
			if !strings.HasPrefix(prompt, "Identify the food") {
				t.Errorf("not the whole-photo prompt: %.40s", prompt)
			}
			c := contract(name)
			if name == "No food" {
				c["description"] = "No edible food visible in image."
			}
			b, _ := json.Marshal(c)
			return textResp("m", string(b))
		}}
}

// fakeSession: the cookie value is the user ID ("" = not signed in).
type fakeSession struct{}

func (fakeSession) Who(r *http.Request) (User, bool) {
	ck, err := r.Cookie(SessionCookie)
	if err != nil || ck.Value == "" {
		return User{}, false
	}
	return User{ID: ck.Value}, true
}

type svc struct {
	h       http.Handler
	dir     string
	photos  string
	gate    int
	budget  *Budget
	api     *fakeAPI
	listing map[string][]map[string]any // user -> live listings answer
	listErr bool
}

func newSvc(t *testing.T, f *fakeAPI, verdict string, gateStatus int, mod func(*ServiceConfig)) *svc {
	s := &svc{dir: t.TempDir(), photos: t.TempDir(), api: f, listing: map[string][]map[string]any{}}
	e, done := engine(t, f)
	t.Cleanup(done)
	e.Marks, e.PackageConcept, e.TrayConcept = true, "", ""
	ev := NewEvents(s.dir)
	s.budget = NewBudget(20, 0.10, ev)
	barcode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/barcode/078742370026" {
			_, _ = w.Write([]byte(`{"outcome":"HIT","gtin14":"00078742370026","admissible":true,"product":{"barcode":"00078742370026","title":"Great Value Marinara Pasta Sauce","description":"","is_non_veg":0,"number_of_serving":13,"serving_type":"bulk","meal_condition":"sealed","weight":0.68,"meal_cost":2}}`))
			return
		}
		_, _ = w.Write([]byte(`{"outcome":"MISS"}`))
	}))
	t.Cleanup(barcode.Close)
	listings := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ck, _ := r.Cookie(SessionCookie)
		if s.listErr || r.URL.Path != "/api/listings/mine" || ck == nil {
			w.WriteHeader(500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": s.listing[ck.Value]})
	}))
	t.Cleanup(listings.Close)
	c := ServiceConfig{Engine: e, Auth: analyze.NewBearer("tok"), Session: fakeSession{},
		Limiter: analyze.NewLimiter(100, time.Minute), Budget: s.budget, Events: ev,
		GateURL: fakeGate(t, verdict, gateStatus, &s.gate).URL, BarcodeURL: barcode.URL, ListingsURL: listings.URL,
		Photos: &PhotoStore{Dir: s.photos}, Build: "test"}
	if mod != nil {
		mod(&c)
	}
	s.h = NewService(c)
	return s
}

func live(title string, expires time.Time) map[string]any {
	return map[string]any{"id": "L-" + title, "title": title, "number_of_serving": 1, "status": "active",
		"expires_at": expires.UTC().Format(time.RFC3339Nano)}
}

// req builds a multipart POST to path. auth "bearer" = the token; any
// other non-empty value = that user's session cookie.
func req(t *testing.T, path, auth string, img []byte, barcode string) *http.Request {
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	h := map[string][]string{"Content-Disposition": {`form-data; name="image"; filename="p.jpg"`}, "Content-Type": {"image/jpeg"}}
	pw, _ := mw.CreatePart(h)
	_, _ = pw.Write(img)
	if barcode != "" {
		_ = mw.WriteField("barcode", barcode)
	}
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, path, &b)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	switch auth {
	case "":
	case "bearer":
		r.Header.Set("Authorization", "Bearer tok")
	default:
		r.AddCookie(&http.Cookie{Name: SessionCookie, Value: auth})
	}
	return r
}

func multipartReq(t *testing.T, img []byte) *http.Request {
	return req(t, "/v1/analyze", "bearer", img, "")
}

func (s *svc) do(r *http.Request) (*httptest.ResponseRecorder, map[string]any) {
	w := httptest.NewRecorder()
	s.h.ServeHTTP(w, r)
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return w, m
}

func (s *svc) events(t *testing.T) []Event {
	b, _ := os.ReadFile(filepath.Join(s.dir, "events-"+time.Now().UTC().Format("20060102")+".jsonl"))
	var out []Event
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatal(err, l)
		}
		out = append(out, e)
	}
	return out
}

func keys(m map[string]any) string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return strings.Join(k, ",")
}

func items(a map[string]any) []map[string]any {
	var out []map[string]any
	for _, x := range a["items"].([]any) {
		out = append(out, x.(map[string]any))
	}
	return out
}

// errorIs checks a non-200 answer: status, code, and the error body shape.
func errorIs(t *testing.T, w *httptest.ResponseRecorder, a map[string]any, status int, code string) {
	t.Helper()
	if w.Code != status || a["code"] != code || keys(a) != "code,error,request_id" || a["error"] == "" {
		t.Fatalf("want %d %s, got %d %s", status, code, w.Code, w.Body)
	}
}

// The 200 shape (T9): {request_id, source, model, latency_ms, count, items:
// [{item, fields: the 8 contract fields, photo_id}]}; number_of_serving =
// the count; one crop per item; one event line with every box.
func TestAnalyzeMultiItemAnswer(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, nil)
	w, a := s.do(multipartReq(t, testPhoto(t)))
	if w.Code != 200 || keys(a) != "count,items,latency_ms,model,request_id,source" || a["source"] != SourceVision || a["count"] != 2.0 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	it := items(a)
	f0, f1 := it[0]["fields"].(map[string]any), it[1]["fields"].(map[string]any)
	if it[0]["item"] != 1.0 || f0["name"] != "Soup" || f0["number_of_serving"] != 2.0 || f1["name"] != "Beans" ||
		f1["number_of_serving"] != 1.0 || keys(f0) != "description,is_non_veg,meal_condition,meal_cost,name,number_of_serving,serving_type,weight" ||
		keys(it[0]) != "fields,item,photo_id" {
		t.Fatalf("%s", w.Body)
	}
	for _, x := range it {
		id := x["photo_id"].(string)
		b, m, err := ReadItem(filepath.Join(s.photos, "items"), id, "")
		if err != nil || m.Kind != "crop" || m.UserID != "" || len(m.Box) != 4 {
			t.Fatalf("%s %+v %v", id, m, err)
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
		if err != nil || cfg.Width >= 800 || cfg.Width < 200 {
			t.Fatalf("crop %+v %v", cfg, err)
		}
	}
	if _, err := os.Stat(filepath.Join(s.photos, a["request_id"].(string))); err == nil {
		t.Fatal("unexpected file")
	}
	ev := s.events(t)
	if len(ev) != 1 || ev[0].Status != 200 || ev[0].Caller != "bearer" || ev[0].Items != 2 || ev[0].Source != SourceVision ||
		ev[0].Dedup != "skipped" || len(ev[0].PhotoIDs) != 2 || !ev[0].PhotoStored || ev[0].CostUSD <= 0 || ev[0].FrameW != 800 {
		t.Fatalf("%+v", ev)
	}
	if bx := ev[0].Boxes; len(bx) != 3 || bx[0].Outcome != "item" || bx[2].Item == 0 || bx[0].Box == [4]int{} {
		t.Fatalf("%+v", bx)
	}
	if got := s.budget.Spent(); got != ev[0].CostUSD {
		t.Fatal("budget", got)
	}
	// the stored upload, under its sha256
	if _, err := os.Stat(filepath.Join(s.photos, ev[0].ImageSHA256+".jpg")); err != nil {
		t.Fatal(err)
	}
}

// R2: multipart and JSON bodies give the same fields; /api/analyze is the same route.
func TestAnalyzeJSONBodyAndAPIPath(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, nil)
	_, a := s.do(multipartReq(t, testPhoto(t)))
	jb, _ := json.Marshal(map[string]string{"image_base64": base64.StdEncoding.EncodeToString(testPhoto(t)), "mime": "image/jpeg"})
	r := httptest.NewRequest(http.MethodPost, "/api/analyze", bytes.NewReader(jb))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer tok")
	w, b := s.do(r)
	fa, _ := json.Marshal(items(a)[0]["fields"])
	fb, _ := json.Marshal(items(b)[0]["fields"])
	if w.Code != 200 || string(fa) != string(fb) || b["count"] != a["count"] {
		t.Fatalf("%d\n%s\n%s", w.Code, fa, fb)
	}
}

// One SAM box: analyze's whole-photo call; the item photo is the upload.
func TestAnalyzeSingleItem(t *testing.T) {
	s := newSvc(t, oneBox(t, "Romanesco"), "FOOD", 0, nil)
	w, a := s.do(req(t, "/api/analyze", "u1", testPhoto(t), ""))
	it := items(a)
	if w.Code != 200 || a["count"] != 1.0 || it[0]["fields"].(map[string]any)["name"] != "Romanesco" ||
		it[0]["fields"].(map[string]any)["number_of_serving"] != 1.0 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if _, m, err := ReadItem(filepath.Join(s.photos, "items"), it[0]["photo_id"].(string), "u1"); err != nil || m.Kind != "full" {
		t.Fatal(m, err)
	}
	if ev := s.events(t); ev[0].Caller != "session" || ev[0].UserID != "u1" || ev[0].Dedup != "ok" {
		t.Fatalf("%+v", ev)
	}
}

// Every refusal before the gate: no gate call, no API call, one event each.
func TestAnalyzeRefusals(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, func(c *ServiceConfig) {
		c.MaxImage = 1000
		c.Limiter = analyze.NewLimiter(3, time.Minute)
	})
	w, a := s.do(httptest.NewRequest(http.MethodGet, "/v1/analyze", nil))
	errorIs(t, w, a, 405, CodeMethod)
	r := multipartReq(t, testPhoto(t))
	r.Header.Set("Authorization", "Bearer wrong")
	w, a = s.do(r)
	errorIs(t, w, a, 401, CodeUnauthorized)
	w, a = s.do(req(t, "/v1/analyze", "", testPhoto(t), ""))
	errorIs(t, w, a, 401, CodeUnauthorized)
	w, a = s.do(multipartReq(t, testPhoto(t))) // counted 1: too large
	errorIs(t, w, a, 413, CodeTooLarge)
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	pw, _ := mw.CreatePart(map[string][]string{"Content-Disposition": {`form-data; name="image"; filename="a.txt"`}, "Content-Type": {"text/plain"}})
	_, _ = pw.Write([]byte("hello"))
	_ = mw.Close()
	r = httptest.NewRequest(http.MethodPost, "/v1/analyze", &b)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("Authorization", "Bearer tok")
	w, a = s.do(r) // counted 2
	errorIs(t, w, a, 415, CodeUnsupported)
	r = httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader("x"))
	r.Header.Set("Authorization", "Bearer tok")
	w, a = s.do(r) // counted 3
	errorIs(t, w, a, 415, CodeUnsupported)
	w, a = s.do(multipartReq(t, testPhoto(t)))
	errorIs(t, w, a, 429, CodeRateLimited)
	if len(s.api.bodies) != 0 || s.gate != 0 {
		t.Fatal("a refused request reached the gate or the API")
	}
	if ev := s.events(t); len(ev) != 7 || ev[1].Status != 401 || ev[6].Code != CodeRateLimited {
		t.Fatalf("%+v", ev)
	}
	// X51 review: a WebP that says 16384 x 16384 (a VP8X header, 30 bytes) is refused before it is decoded; the WebP
	// decoder this package registers lets the size check read it
	webp := []byte("RIFF\x16\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x00\x00\x00\x00\xff\x3f\x00\xff\x3f\x00")
	if _, _, code, msg := (analyze.Config{MaxImage: 5 << 20}).ReadImage(req(t, "/v1/analyze", "bearer", webp, "")); code != 413 {
		t.Fatalf("a WebP of 16384 x 16384: %d %s", code, msg)
	}
}

// T9: 6/min per user; another user has their own limit; bearer is per IP.
func TestAnalyzeRateLimitPerUser(t *testing.T) {
	s := newSvc(t, oneBox(t, "Soup"), "FOOD", 0, func(c *ServiceConfig) { c.Limiter = analyze.NewLimiter(1, time.Minute) })
	if w, _ := s.do(req(t, "/api/analyze", "u1", testPhoto(t), "")); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w, a := s.do(req(t, "/api/analyze", "u1", testPhoto(t), "")); w.Code != 429 || a["code"] != CodeRateLimited {
		t.Fatal(w.Code)
	}
	if w, _ := s.do(req(t, "/api/analyze", "u2", testPhoto(t), "")); w.Code != 200 {
		t.Fatal("u2", w.Code)
	}
}

// museBodies counts the API bodies that are not SAM calls. X55: a body with no model is not counted: it is a SAM
// call stopped while it was sent (the gate failed and the scan stopped its SAM call: "context canceled"), which the
// fake API keeps as an empty body; the test failed at times with "gate down".
func museBodies(f *fakeAPI) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, b := range f.bodies {
		if m, _ := b["model"].(string); m != "" && m != DefaultSAM {
			n++
		}
	}
	return n
}

// The gate: NOT_FOOD = 422 not_food, and no Muse call is made (X48a: SAM
// may have started at the same time as the gate); a gate that is down
// fails closed with 503 gate_unavailable.
func TestAnalyzeGateNotFoodAndDown(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "NOT_FOOD", 0, nil)
	w, a := s.do(multipartReq(t, testPhoto(t)))
	errorIs(t, w, a, 422, CodeNotFood)
	if museBodies(s.api) != 0 || s.events(t)[0].GateVerdict != "NOT_FOOD" {
		t.Fatal("Muse called on NOT_FOOD")
	}
	d := newSvc(t, threeBoxes(t), "", 500, nil)
	w, a = d.do(multipartReq(t, testPhoto(t)))
	errorIs(t, w, a, 503, CodeGateUnavailable)
	if museBodies(d.api) != 0 || d.events(t)[0].GateVerdict != "UNAVAILABLE" {
		t.Fatal("gate down")
	}
}

// Gate FOOD, 2+ boxes, the model calls every product not food: 422
// model_no_food_multi; the boxes are logged as not_food.
func TestAnalyzeModelNoFoodMulti(t *testing.T) {
	f := &fakeAPI{sam: func(string) (int, string) { return 200, samLine(t, [4]int{0, 0, 199, 299}, [4]int{300, 0, 499, 299}) },
		muse: func(string, map[string]any) map[string]any {
			a, b := ent(1, nil, "Dog food"), ent(2, nil, "Cat food")
			a["food"], b["food"] = false, false
			return textResp("m", itemsText(a, b))
		}}
	s := newSvc(t, f, "FOOD", 0, nil)
	w, a := s.do(multipartReq(t, testPhoto(t)))
	errorIs(t, w, a, 422, CodeNoFoodMulti)
	ev := s.events(t)[0]
	if ev.Code != CodeNoFoodMulti || len(ev.Boxes) != 2 || ev.Boxes[0].Outcome != "not_food" || ev.CostUSD <= 0 {
		t.Fatalf("%+v", ev)
	}
}

// Gate FOOD, 1 box, the model's answer says no food: 422 model_no_food_single.
func TestAnalyzeModelNoFoodSingle(t *testing.T) {
	s := newSvc(t, oneBox(t, "No food"), "FOOD", 0, nil)
	w, a := s.do(multipartReq(t, testPhoto(t)))
	errorIs(t, w, a, 422, CodeNoFoodSingle)
	if ev := s.events(t)[0]; ev.DroppedNoFood != 1 || ev.Boxes[0].Outcome != "model_no_food" {
		t.Fatalf("%+v", ev)
	}
}

// 2+ items, one named "Unknown": that one is dropped, the rest answer.
func TestAnalyzeDropsModelNoFoodItem(t *testing.T) {
	s := newSvc(t, threeBoxes(t, "Soup", "Soup", "Unknown"), "FOOD", 0, nil)
	w, a := s.do(multipartReq(t, testPhoto(t)))
	if w.Code != 200 || a["count"] != 1.0 || items(a)[0]["fields"].(map[string]any)["name"] != "Soup" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if ev := s.events(t)[0]; ev.DroppedNoFood != 1 || ev.Boxes[2].Outcome != "model_no_food" {
		t.Fatalf("%+v", ev.Boxes)
	}
}

// The barcode fast path: no gate, no model, no cap; the photo is the item photo.
func TestAnalyzeBarcode(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, func(c *ServiceConfig) { c.Budget.Cap = 0 })
	w, a := s.do(req(t, "/api/analyze", "u1", testPhoto(t), "078742370026"))
	it := items(a)
	if w.Code != 200 || a["source"] != SourceBarcode || a["model"] != "catalog" || a["count"] != 1.0 ||
		it[0]["fields"].(map[string]any)["name"] != "Great Value Marinara Pasta Sauce" || it[0]["fields"].(map[string]any)["number_of_serving"] != 1.0 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if s.gate != 0 || len(s.api.bodies) != 0 {
		t.Fatal("barcode hit reached the gate or the API")
	}
	ev := s.events(t)[0]
	if ev.BarcodeOutcome != "HIT" || ev.CatalogServings != 13 || ev.Barcode != "078742370026" {
		t.Fatalf("%+v", ev)
	}
	// a miss goes on; the cap of 0 refuses it before the gate and SAM (X48a)
	w, a = s.do(req(t, "/api/analyze", "u1", testPhoto(t), "000000000000"))
	errorIs(t, w, a, 503, CodeDailyCap)
	if s.gate != 0 || len(s.api.bodies) != 0 {
		t.Fatal(s.gate, len(s.api.bodies))
	}
}

// Dedup (T9): several items = a listed one is dropped and logged; one
// item = 409 already_listed; expired listings do not count; listings down
// = the answer goes out (dedup unavailable).
func TestAnalyzeDedup(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, nil)
	s.listing["u1"] = []map[string]any{live("Beans", time.Now().Add(time.Hour)), live("Soups", time.Now().Add(-time.Hour))}
	w, a := s.do(req(t, "/api/analyze", "u1", testPhoto(t), ""))
	if w.Code != 200 || a["count"] != 1.0 || items(a)[0]["fields"].(map[string]any)["name"] != "Soup" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	ev := s.events(t)[0]
	if ev.DroppedListed != 1 || ev.Dedup != "ok" || ev.Boxes[2].Outcome != "already_listed" {
		t.Fatalf("%+v", ev)
	}
	// bearer: no user, so no dedup
	if w, a := s.do(multipartReq(t, testPhoto(t))); w.Code != 200 || a["count"] != 2.0 {
		t.Fatal(w.Code, w.Body)
	}
	// every item listed: 409
	s.listing["u1"] = append(s.listing["u1"], live("Soup", time.Now().Add(time.Hour)))
	w, a = s.do(req(t, "/api/analyze", "u1", testPhoto(t), ""))
	errorIs(t, w, a, 409, CodeAlreadyListed)
	// one item listed: 409
	o := newSvc(t, oneBox(t, "Romanesco"), "FOOD", 0, nil)
	o.listing["u1"] = []map[string]any{live("Romanesco", time.Now().Add(time.Hour))}
	w, a = o.do(req(t, "/api/analyze", "u1", testPhoto(t), ""))
	errorIs(t, w, a, 409, CodeAlreadyListed)
	// a barcode hit that is listed: 409
	o.listing["u1"] = []map[string]any{live("Great Value Marinara Pasta Sauce", time.Now().Add(time.Hour))}
	w, a = o.do(req(t, "/api/analyze", "u1", testPhoto(t), "078742370026"))
	errorIs(t, w, a, 409, CodeAlreadyListed)
	// listings down: the answer goes out
	o.listErr = true
	o.listing["u1"] = nil
	if w, _ := o.do(req(t, "/api/analyze", "u1", testPhoto(t), "")); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	evs := o.events(t)
	if last := evs[len(evs)-1]; last.Dedup != "unavailable" {
		t.Fatalf("%+v", last)
	}
}

// Item photos are read by their owner only (OWASP API1:2023); 404 for the rest.
func TestAnalyzePhotoOwnerOnly(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, nil)
	_, a := s.do(req(t, "/api/analyze", "u1", testPhoto(t), ""))
	id := items(a)[0]["photo_id"].(string)
	get := func(path, auth string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		switch auth {
		case "":
		case "bearer":
			r.Header.Set("Authorization", "Bearer tok")
		default:
			r.AddCookie(&http.Cookie{Name: SessionCookie, Value: auth})
		}
		w := httptest.NewRecorder()
		s.h.ServeHTTP(w, r)
		return w
	}
	if w := get("/api/analyze/photo/"+id, "u1"); w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" || w.Body.Len() == 0 {
		t.Fatal(w.Code)
	}
	for _, c := range []struct{ path, auth string }{{"/api/analyze/photo/" + id, "u2"}, {"/v1/analyze/photo/" + id, "bearer"},
		{"/api/analyze/photo/..%2F..%2Fetc", "u1"}, {"/api/analyze/photo/ABC", "u1"}} {
		if w := get(c.path, c.auth); w.Code != 404 {
			t.Fatal(c, w.Code)
		}
	}
	if w := get("/api/analyze/photo/"+id, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	_, b := s.do(multipartReq(t, testPhoto(t)))
	if w := get("/v1/analyze/photo/"+items(b)[0]["photo_id"].(string), "bearer"); w.Code != 200 {
		t.Fatal("bearer reads its own photo", w.Code)
	}
}

// MaxInFlight photos run; one more gets 503 busy, and no API call.
func TestAnalyzeBusy(t *testing.T) {
	release, started := make(chan struct{}), make(chan struct{}, 4)
	f := threeBoxes(t)
	inner := f.sam
	f.sam = func(c string) (int, string) {
		started <- struct{}{}
		<-release
		return inner(c)
	}
	s := newSvc(t, f, "FOOD", 0, func(c *ServiceConfig) { c.MaxInFlight = 1 })
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if w, _ := s.do(multipartReq(t, testPhoto(t))); w.Code != 200 {
			t.Error("first", w.Code)
		}
	}()
	<-started
	w, a := s.do(multipartReq(t, testPhoto(t)))
	close(release)
	wg.Wait()
	errorIs(t, w, a, 503, CodeBusy)
}

// The cap: spent + held + estimate over the cap is refused before any API
// call (503 daily_cap_reached); the spend in the events file survives a restart.
func TestAnalyzeDailyCap(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, func(c *ServiceConfig) { c.Budget.Cap = 0.05 })
	w, a := s.do(multipartReq(t, testPhoto(t)))
	errorIs(t, w, a, 503, CodeDailyCap)
	if len(s.api.bodies) != 0 {
		t.Fatal("API called over the cap")
	}
	b := NewBudget(1, 0.4, nil)
	h1, ok1 := b.Reserve()
	h2, ok2 := b.Reserve()
	_, ok3 := b.Reserve() // 0.8 held + 0.4 > 1
	if !ok1 || !ok2 || ok3 {
		t.Fatal(ok1, ok2, ok3)
	}
	h1.Settle(0.03)
	h1.Settle(0.03) // once only
	h2.Settle(0.02)
	if b.Spent() != 0.05 {
		t.Fatal(b.Spent())
	}
	dir := t.TempDir()
	ev := NewEvents(dir)
	ev.Record(Event{RequestID: "a", CostUSD: 0.25})
	ev.Record(Event{RequestID: "b", CostUSD: 0.5})
	if r := NewBudget(20, 0.1, ev); r.Spent() != 0.75 {
		t.Fatal("restart", r.Spent())
	}
}

// X31c: a photo over the timeout gets 504 timeout with a clear message,
// not the load balancer's own 504 at 60 s.
func TestAnalyzeTimeoutIsClear(t *testing.T) {
	f := threeBoxes(t)
	inner := f.sam
	f.sam = func(c string) (int, string) {
		time.Sleep(300 * time.Millisecond)
		return inner(c)
	}
	s := newSvc(t, f, "FOOD", 0, func(c *ServiceConfig) { c.Timeout = 100 * time.Millisecond })
	w, a := s.do(multipartReq(t, testPhoto(t)))
	errorIs(t, w, a, 504, CodeTimeout)
	if !strings.Contains(a["error"].(string), "longer than 100ms") {
		t.Fatal(a["error"])
	}
	if ev := s.events(t); len(ev) != 1 || ev[0].Status != 504 {
		t.Fatalf("%+v", ev)
	}
}

// X32e: the app's "capture" field (how the photo was taken) is logged as
// sent when it is one small JSON object, and dropped otherwise; it never
// changes the answer.
func TestCaptureFieldIsLogged(t *testing.T) {
	s := newSvc(t, oneBox(t, "Romanesco"), "FOOD", 0, nil)
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	h := map[string][]string{"Content-Disposition": {`form-data; name="image"; filename="p.jpg"`}, "Content-Type": {"image/jpeg"}}
	pw, _ := mw.CreatePart(h)
	_, _ = pw.Write(testPhoto(t))
	_ = mw.WriteField("capture", `{"source":"photo","ms":812,"video_t":12.5,"paused":false}`)
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/analyze", &b)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: "u1"})
	if w, a := s.do(r); w.Code != 200 || a["count"] != 1.0 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	ev := s.events(t)
	var c map[string]any
	if err := json.Unmarshal(ev[0].Capture, &c); err != nil || c["source"] != "photo" || c["ms"] != 812.0 {
		t.Fatalf("%s %v", ev[0].Capture, err)
	}
	for _, bad := range []string{"", "not json", "[1,2]", `{"a":` + strings.Repeat("1", 600) + `}`} {
		if CaptureField(bad) != nil {
			t.Fatalf("kept %q", bad)
		}
	}
}

// X44a (audit S1 item 2): a caller over its own share of the day gets 429
// caller_daily_cap_reached before any API call; another user still scans;
// every line has the caller's cap_key, so a restart restores the shares.
func TestAnalyzeCallerCap(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, func(c *ServiceConfig) { c.Budget.PerCaller = 0.05 }) // under one 0.10 hold
	w, a := s.do(req(t, "/api/analyze", "u1", testPhoto(t), ""))
	errorIs(t, w, a, 429, CodeCallerCap)
	if len(s.api.bodies) != 0 {
		t.Fatal("API called over the caller cap")
	}
	s.budget.PerCaller = 0.15
	if w, _ := s.do(req(t, "/api/analyze", "u2", testPhoto(t), "")); w.Code != 200 {
		t.Fatalf("u2: %d %s", w.Code, w.Body)
	}
	evs := s.events(t)
	if len(evs) != 2 || evs[0].CapKey != "user:u1" || evs[1].CapKey != "user:u2" || evs[1].CostUSD <= 0 {
		t.Fatalf("cap keys: %+v", evs)
	}
	if !nearly(s.budget.SpentBy("user:u2"), evs[1].CostUSD) || s.budget.SpentBy("user:u1") != 0 {
		t.Fatal(s.budget.SpentBy("user:u2"), evs[1].CostUSD)
	}
	_, by := NewEvents(s.dir).SpentByKey(time.Now())
	if !nearly(by["user:u2"], evs[1].CostUSD) {
		t.Fatalf("restart: %v", by)
	}
}

func nearly(a, b float64) bool { d := a - b; return d < 1e-12 && d > -1e-12 }

// X48a: SAM runs while the gate checks the photo; the Muse calls wait for
// the gate. A slow gate (300 ms): every SAM call starts before the gate
// ends, every Muse call starts after it, with its wait logged; with
// NOT_FOOD the slow gate stops the run and no Muse call is made.
func TestGateRunsWithSAM(t *testing.T) {
	slowGate := func(verdict string) func(*ServiceConfig) {
		return func(c *ServiceConfig) {
			g := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(300 * time.Millisecond)
				_ = json.NewEncoder(w).Encode(map[string]any{"verdict": verdict, "food": verdict == "FOOD", "p": 0.9})
			}))
			t.Cleanup(g.Close)
			c.GateURL = g.URL
		}
	}
	s := newSvc(t, threeBoxes(t), "FOOD", 0, slowGate("FOOD"))
	w, _ := s.do(multipartReq(t, testPhoto(t)))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	ev := s.events(t)[0]
	gateEnd := ev.GateAtMS + ev.GateMS
	if ev.GateMS < 290 {
		t.Fatalf("gate %d ms", ev.GateMS)
	}
	var sam, muse int
	for _, c := range ev.Calls {
		if c.AtMS < 0 {
			t.Fatalf("no at_ms: %+v", c)
		}
		if strings.HasPrefix(c.Purpose, "sam:") {
			sam++
			if c.AtMS >= gateEnd {
				t.Fatalf("SAM started after the gate: %+v (gate end %d)", c, gateEnd)
			}
		} else {
			muse++
			if c.AtMS < gateEnd-5 || (c.AtMS < 250 && c.WaitMS < 200) {
				t.Fatalf("Muse did not wait for the gate: %+v (gate end %d)", c, gateEnd)
			}
		}
	}
	if sam == 0 || muse == 0 {
		t.Fatalf("calls %+v", ev.Calls)
	}
	n := newSvc(t, threeBoxes(t), "NOT_FOOD", 0, slowGate("NOT_FOOD"))
	w, a := n.do(multipartReq(t, testPhoto(t)))
	errorIs(t, w, a, 422, CodeNotFood)
	if museBodies(n.api) != 0 {
		t.Fatal("Muse called on NOT_FOOD")
	}
	if ev := n.events(t)[0]; ev.CostUSD <= 0 || len(ev.Calls) == 0 {
		t.Fatalf("the SAM spend of a NOT_FOOD photo is not logged: %+v", ev)
	}
}
