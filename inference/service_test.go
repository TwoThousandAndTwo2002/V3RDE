package inference

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func twoItems(t *testing.T) *fakeAPI {
	return &fakeAPI{sam: func(string) (int, string) {
		return 200, samLine(t, [4]int{0, 0, 199, 299}, [4]int{300, 0, 499, 299}, [4]int{600, 0, 799, 299})
	}, muse: func(string, map[string]any) map[string]any {
		return textResp("m", itemsText(ent(1, nil, "Soup"), ent(2, 1.0, "Soup"), ent(3, nil, "Beans")))
	}}
}

type svc struct {
	h      http.Handler
	dir    string
	gate   int
	budget *Budget
	api    *fakeAPI
}

func newSvc(t *testing.T, f *fakeAPI, verdict string, gateStatus int, mod func(*ServiceConfig)) *svc {
	s := &svc{dir: t.TempDir(), api: f}
	e, done := engine(t, f)
	t.Cleanup(done)
	e.Marks, e.PackageConcept, e.TrayConcept = true, "", ""
	ev := NewEvents(s.dir)
	s.budget = NewBudget(20, 0.10, ev)
	c := ServiceConfig{Engine: e, Auth: analyze.NewBearer("tok"), Limiter: analyze.NewLimiter(100, time.Minute),
		Budget: s.budget, Events: ev, GateURL: fakeGate(t, verdict, gateStatus, &s.gate).URL, Build: "test"}
	if mod != nil {
		mod(&c)
	}
	s.h = NewService(c)
	return s
}

func multipartReq(t *testing.T, img []byte) *http.Request {
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	h := map[string][]string{"Content-Disposition": {`form-data; name="image"; filename="p.jpg"`}, "Content-Type": {"image/jpeg"}}
	pw, _ := mw.CreatePart(h)
	_, _ = pw.Write(img)
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/v1/inventory", &b)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("Authorization", "Bearer tok")
	return r
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

// R2 + R4: multipart and JSON give the same answer; the top level is the
// largest group; every request writes one event line with its cost.
func TestServiceMultipartAndJSONGiveTheSameAnswer(t *testing.T) {
	s := newSvc(t, twoItems(t), "FOOD", 0, nil)
	img := testPhoto(t)
	r := multipartReq(t, img)
	r.Header.Set("X-Trace-Id", "trace-from-router")
	w, a := s.do(r)
	if w.Code != 200 || a["route"] != RouteInventory || a["name"] != "Soup" || a["number_of_serving"] != 2.0 ||
		a["listings"] != 2.0 || a["counted"] != 3.0 || a["trace_id"] != "trace-from-router" || a["gate_verdict"] != "FOOD" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if items := a["items"].([]any); len(items) != 2 || a["cost_usd"].(float64) <= 0 {
		t.Fatalf("%s", w.Body)
	}
	jb, _ := json.Marshal(map[string]string{"image_base64": base64.StdEncoding.EncodeToString(img), "mime": "image/jpeg"})
	r2 := httptest.NewRequest(http.MethodPost, "/v1/inventory", bytes.NewReader(jb))
	r2.Header.Set("Content-Type", "application/json")
	r2.Header.Set("Authorization", "Bearer tok")
	w2, b := s.do(r2)
	ia, _ := json.Marshal(a["items"])
	ib, _ := json.Marshal(b["items"])
	if w2.Code != 200 || string(ia) != string(ib) || a["image_sha256"] != b["image_sha256"] {
		t.Fatalf("%d\n%s\n%s", w2.Code, ia, ib)
	}
	ev := s.events(t)
	if len(ev) != 2 || ev[0].TraceID != "trace-from-router" || ev[0].Status != 200 || ev[0].Listings != 2 ||
		len(ev[0].Calls) != 2 || ev[0].CostUSD != a["cost_usd"].(float64) || ev[0].GateVerdict != "FOOD" {
		t.Fatalf("%+v", ev)
	}
	if got := s.budget.Spent(); got != ev[0].CostUSD+ev[1].CostUSD {
		t.Fatal("budget", got)
	}
}

func TestServiceAuthMethodSizeAndRate(t *testing.T) {
	s := newSvc(t, twoItems(t), "FOOD", 0, func(c *ServiceConfig) {
		c.MaxImage = 1000
		c.Limiter = analyze.NewLimiter(2, time.Minute)
	})
	if w, _ := s.do(httptest.NewRequest(http.MethodGet, "/v1/inventory", nil)); w.Code != 405 {
		t.Fatal(w.Code)
	}
	r := multipartReq(t, testPhoto(t))
	r.Header.Set("Authorization", "Bearer wrong")
	if w, _ := s.do(r); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w, _ := s.do(multipartReq(t, testPhoto(t))); w.Code != 413 { // 1st counted request: too large
		t.Fatal(w.Code, w.Body)
	}
	_, _ = s.do(multipartReq(t, testPhoto(t)))
	if w, _ := s.do(multipartReq(t, testPhoto(t))); w.Code != 429 {
		t.Fatal(w.Code)
	}
	if len(s.api.bodies) != 0 || s.gate != 0 {
		t.Fatal("a refused request reached the gate or the API")
	}
	if ev := s.events(t); len(ev) != 5 || ev[1].Status != 401 || ev[4].Status != 429 {
		t.Fatalf("%+v", ev)
	}
}

// The gate first: NOT_FOOD answers at $0; a gate that is down fails closed.
func TestServiceGateNotFoodAndDown(t *testing.T) {
	s := newSvc(t, twoItems(t), "NOT_FOOD", 0, nil)
	w, a := s.do(multipartReq(t, testPhoto(t)))
	if w.Code != 200 || a["route"] != RouteNotFood || a["not_food"] != true || a["cost_usd"] != 0.0 || len(s.api.bodies) != 0 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	d := newSvc(t, twoItems(t), "", 500, nil)
	w, a = d.do(multipartReq(t, testPhoto(t)))
	if w.Code != 503 || a["route"] != RouteUnavailable || a["gate_verdict"] != "UNAVAILABLE" || len(d.api.bodies) != 0 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

// Every product is not food: 200 not_food with the boxes in not_listed.
func TestServiceAllProductsNotFood(t *testing.T) {
	f := &fakeAPI{sam: func(string) (int, string) { return 200, samLine(t, [4]int{0, 0, 199, 299}, [4]int{300, 0, 499, 299}) },
		muse: func(string, map[string]any) map[string]any {
			a, b := ent(1, nil, "Dog food"), ent(2, nil, "Cat food")
			a["food"], b["food"] = false, false
			return textResp("m", itemsText(a, b))
		}}
	s := newSvc(t, f, "FOOD", 0, nil)
	w, a := s.do(multipartReq(t, testPhoto(t)))
	if w.Code != 200 || a["route"] != RouteNotFood || a["not_food"] != true || len(a["not_listed"].([]any)) != 2 ||
		len(a["items"].([]any)) != 0 || a["name"] != "" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

// MaxInFlight photos run; one more gets 503 busy, and no API call.
func TestServiceBusy(t *testing.T) {
	release, started := make(chan struct{}), make(chan struct{}, 4)
	f := twoItems(t)
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
	if w.Code != 503 || a["route"] != RouteBusy {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

// The cap: spent + held + estimate over the cap is refused before any API
// call; the spend in the events file survives a restart.
func TestServiceBudgetCap(t *testing.T) {
	s := newSvc(t, twoItems(t), "FOOD", 0, func(c *ServiceConfig) { c.Budget.Cap = 0.05 })
	if w, a := s.do(multipartReq(t, testPhoto(t))); w.Code != 503 || a["route"] != RouteBudget || len(s.api.bodies) != 0 {
		t.Fatalf("%d %s", w.Code, w.Body)
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

// X31c: a photo over the timeout gets 504 with a clear message, not the
// load balancer's own 504 at 60 s.
func TestServiceTimeoutIsClear(t *testing.T) {
	f := twoItems(t)
	inner := f.sam
	f.sam = func(c string) (int, string) {
		time.Sleep(300 * time.Millisecond)
		return inner(c)
	}
	s := newSvc(t, f, "FOOD", 0, func(c *ServiceConfig) { c.Timeout = 100 * time.Millisecond })
	w, a := s.do(multipartReq(t, testPhoto(t)))
	if w.Code != 504 || a["route"] != RouteError || !strings.Contains(a["error"].(string), "longer than 100ms") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if ev := s.events(t); len(ev) != 1 || ev[0].Status != 504 {
		t.Fatalf("%+v", ev)
	}
}
