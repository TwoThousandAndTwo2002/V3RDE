package post

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const dirAnswer = `{"results":[
 {"id":"a1","name":"Grace Soup Kitchen","type":"soup_kitchen","address":"1 Main St, Town, NY 10001","phone":"(212) 555-0101","distance_miles":0.8},
 {"id":"a2","name":"City Food Pantry","type":"pantry","address":{"street":"2 Oak Ave","city":"Town","state":"NY","zip":"10001"},"phone":"212-555-0102","distance_miles":1.5,"email":"help@pantry.example.org","website":"https://pantry.example.org"},
 {"id":"a3","name":"North Food Bank","type":"food_bank","address":"3 Elm Rd","distance_miles":0.9,"email":"not an email","website":"javascript:alert(1)"},
 {"id":"a4","name":"Hope Shelter Meals","type":"meal_program","address":"4 Pine Rd","phone":"212 555 0104","distance_miles":2.0},
 {"name":""}
]}`

type dirFake struct {
	status int
	mu     sync.Mutex
	query  string
}

func (d *dirFake) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		d.query = r.URL.Path + "?" + r.URL.RawQuery
		d.mu.Unlock()
		if d.status != 0 {
			w.WriteHeader(d.status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(dirAnswer))
	}))
}

type rsEnv struct {
	h      http.Handler
	cfg    *RescueConfig
	dir    *dirFake
	muse   *[]map[string]any
	sent   *[]string
	closes []func()
	now    time.Time
}

func (e *rsEnv) stop() {
	for _, f := range e.closes {
		f()
	}
}

func newRS(t *testing.T, museText string, museStatus int, relay bool) *rsEnv {
	now := time.Date(2026, 10, 28, 15, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Minute).Format(time.RFC3339Nano), now.Add(time.Minute).Format(time.RFC3339Nano)
	ls := listingsFake(t, []Listing{
		{ID: "l_000000000000000a", OwnerID: "u1", Title: "Canned beans", NumberOfServing: 24, Status: "active", ExpiresAt: past},
		{ID: "l_000000000000000b", OwnerID: "u1", Title: "Salad", NumberOfServing: 2, Status: "active", ExpiresAt: future},
		{ID: "l_000000000000000c", OwnerID: "u1", Title: "Bread", NumberOfServing: 10, Status: "collected", ExpiresAt: past},
		{ID: "l_000000000000000d", OwnerID: "u2", Title: "Rice", NumberOfServing: 30, Status: "active", ExpiresAt: past},
		{ID: "l_000000000000000e", OwnerID: "u1", Title: "Soup", NumberOfServing: 2, Status: "active", ExpiresAt: past},
	})
	var got []map[string]any
	ms := museFake(t, museText, museStatus, &got)
	df := &dirFake{}
	ds := df.server()
	st, err := OpenRescues(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var sent []string
	cfg := &RescueConfig{
		Muse:      &Muse{Base: ms.URL, Key: "key", Effort: "low"},
		Directory: &Directory{Base: ds.URL},
		Listings:  &ListingsClient{URL: ls.URL},
		Session:   fakeAuth{},
		Limiter:   NewLimiter(100, time.Minute),
		Budget:    NewBudget(1, 0.02, nil),
		Events:    NewEvents(t.TempDir()),
		Store:     st,
		now:       func() time.Time { return now },
	}
	if relay {
		cfg.Mailer = &Mailer{Addr: "smtp.example.org:587", User: "u", Pass: "p", From: "icarus@example.org",
			send: func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
				sent = append(sent, strings.Join(to, ",")+"\n"+string(msg))
				return nil
			}}
	}
	return &rsEnv{h: NewRescue(cfg), cfg: cfg, dir: df, muse: &got, sent: &sent,
		closes: []func(){ls.Close, ms.Close, ds.Close}, now: now}
}

func jsonReq(method, path, body, user string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if user != "" {
		r.AddCookie(&http.Cookie{Name: SessionCookie, Value: user})
	}
	return r
}

const stableDraft = `{"shelf_stable": true, "reason": "sealed cans keep without a refrigerator",
 "subject": "Donation of 24 cans of beans", "body": "Hello, can you accept 24 sealed cans of beans? When can I drop them off?"}`

func TestRescueShelfStableGoesToFoodBanks(t *testing.T) {
	e := newRS(t, stableDraft, 200, false)
	defer e.stop()
	w := do(e.h, jsonReq("POST", "/api/rescue/l_000000000000000a", `{"zip":"10001"}`, "u1"))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var res Rescue
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Kind != KindFoodBank || !res.ShelfStable || res.Attribution != DirectoryAttribution || len(res.Places) != 3 {
		t.Fatalf("%+v", res)
	}
	// food banks and pantries first, nearest first; then the others
	if res.Places[0].ID != "a3" || res.Places[1].ID != "a2" {
		t.Fatalf("order %v %v %v", res.Places[0].ID, res.Places[1].ID, res.Places[2].ID)
	}
	p2 := res.Places[1]
	if p2.Address != "2 Oak Ave, Town, NY, 10001" || p2.Tel != "tel:2125550102" || p2.Email != "help@pantry.example.org" ||
		!strings.HasPrefix(p2.Mailto, "mailto:help%40pantry.example.org?subject=Donation%20of%2024") {
		t.Fatalf("place %+v", p2)
	}
	if res.Places[0].Email != "" || res.Places[0].Website != "" {
		t.Fatalf("bad email or website kept: %+v", res.Places[0])
	}
	if !strings.Contains(e.dir.query, "/api/resources/search?") || !strings.Contains(e.dir.query, "mode=free") ||
		!strings.Contains(e.dir.query, "zip=10001") {
		t.Fatalf("directory query %s", e.dir.query)
	}
	// Muse got the title, the servings and the listing photo
	content := (*e.muse)[0]["input"].([]any)[0].(map[string]any)["content"].([]any)
	if len(content) != 2 || !strings.Contains(content[0].(map[string]any)["text"].(string), "Title: Canned beans\nServings: 24") {
		t.Fatalf("muse content %v", content)
	}
	if got, ok := e.cfg.Store.Get(res.RescueID, "u1"); !ok || got.ListingID != "l_000000000000000a" {
		t.Fatal("not stored")
	}
	if _, ok := e.cfg.Store.Get(res.RescueID, "u2"); ok {
		t.Fatal("another user read the rescue")
	}
	b, _ := os.ReadFile(e.cfg.Events.Path(e.now))
	if !strings.Contains(string(b), `"route":"rescue.create"`) || !strings.Contains(string(b), `"purpose":"rescue:draft"`) {
		t.Fatalf("events %s", b)
	}
}

func TestRescuePerishableGoesToMealPrograms(t *testing.T) {
	e := newRS(t, `{"shelf_stable": false, "reason": "fresh soup needs a refrigerator", "subject": "Soup", "body": "Can you take soup?"}`, 200, false)
	defer e.stop()
	w := do(e.h, jsonReq("POST", "/api/rescue/l_000000000000000e", `{"lat":40.75,"lng":-73.99}`, "u1"))
	var res Rescue
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || res.Kind != KindMealProgram || res.Places[0].ID != "a1" || res.Places[1].ID != "a4" {
		t.Fatalf("%d %+v", w.Code, res)
	}
	if !strings.Contains(e.dir.query, "/api/resources/nearby?") || !strings.Contains(e.dir.query, "lat=40.75000") {
		t.Fatalf("directory query %s", e.dir.query)
	}
}

func TestRescueRefusals(t *testing.T) {
	e := newRS(t, stableDraft, 200, false)
	defer e.stop()
	cases := []struct {
		path, body, user string
		code             int
		want             string
	}{
		{"/api/rescue/l_000000000000000a", `{"zip":"10001"}`, "", 401, CodeUnauthorized},
		{"/api/rescue/l_000000000000000d", `{"zip":"10001"}`, "u1", 404, CodeNotFound},  // not the caller's
		{"/api/rescue/l_000000000000000b", `{"zip":"10001"}`, "u1", 409, "not_expired"}, // timer still running
		{"/api/rescue/l_000000000000000c", `{"zip":"10001"}`, "u1", 409, "collected"},
		{"/api/rescue/l_000000000000000a", `{"zip":"1000"}`, "u1", 400, CodeBadRequest},
		{"/api/rescue/l_000000000000000a", `{"lat":40}`, "u1", 400, CodeBadRequest},
		{"/api/rescue/l_000000000000000a", `nope`, "u1", 400, CodeBadRequest},
		{"/api/rescue/l_bad", `{"zip":"10001"}`, "u1", 404, CodeNotFound}, // not a listing id
	}
	for _, c := range cases {
		w := do(e.h, jsonReq("POST", c.path, c.body, c.user))
		if w.Code != c.code || !strings.Contains(w.Body.String(), c.want) {
			t.Fatalf("%s %s: %d %s", c.path, c.body, w.Code, w.Body)
		}
	}
	e.cfg.MinServings = 10
	if w := do(e.h, jsonReq("POST", "/api/rescue/l_000000000000000e", `{"zip":"10001"}`, "u1")); w.Code != 422 ||
		!strings.Contains(w.Body.String(), "not_bulk") {
		t.Fatalf("bulk rule: %d %s", w.Code, w.Body)
	}
	e.cfg.Budget = NewBudget(0.01, 0.02, nil)
	if w := do(e.h, jsonReq("POST", "/api/rescue/l_000000000000000a", `{"zip":"10001"}`, "u1")); w.Code != 503 {
		t.Fatalf("cap: %d", w.Code)
	}
	if len(*e.muse) != 0 {
		t.Fatal("a refused request called Muse")
	}
}

func TestRescueUpstreamFailures(t *testing.T) {
	for _, c := range []struct {
		text   string
		status int
		dir    int
	}{
		{"no JSON here", 200, 0},
		{`{"shelf_stable": "yes", "subject": "s", "body": "b"}`, 200, 0},
		{`{"shelf_stable": true, "subject": "", "body": "b"}`, 200, 0},
		{"", 500, 0},
		{stableDraft, 200, 500},
	} {
		e := newRS(t, c.text, c.status, false)
		e.dir.status = c.dir
		w := do(e.h, jsonReq("POST", "/api/rescue/l_000000000000000a", `{"zip":"10001"}`, "u1"))
		if w.Code != 502 {
			t.Fatalf("%q dir %d: %d %s", c.text, c.dir, w.Code, w.Body)
		}
		e.stop()
	}
}

func TestRescueSendRelay(t *testing.T) {
	e := newRS(t, stableDraft, 200, true)
	defer e.stop()
	w := do(e.h, jsonReq("POST", "/api/rescue/l_000000000000000a", `{"zip":"10001"}`, "u1"))
	var res Rescue
	json.Unmarshal(w.Body.Bytes(), &res)
	send := func(body, user string) *httptest.ResponseRecorder {
		return do(e.h, jsonReq("POST", "/api/rescue/"+res.RescueID+"/send", body, user))
	}
	if w := send(`{"place_id":"a2"}`, "u2"); w.Code != 404 {
		t.Fatalf("other user: %d", w.Code)
	}
	if w := send(`{"place_id":"zz","to_email":"a@b.org"}`, "u1"); w.Code != 400 {
		t.Fatalf("unknown place: %d", w.Code)
	}
	if w := send(`{"place_id":"a3"}`, "u1"); w.Code != 400 {
		t.Fatalf("no address: %d", w.Code)
	}
	if w := send(`{"place_id":"a3","to_email":"a@b.org, c@d.org"}`, "u1"); w.Code != 400 {
		t.Fatalf("two addresses: %d", w.Code)
	}
	// the directory's own address, with the draft; header injection is removed
	if w := send(`{"place_id":"a2","subject":"Beans\r\nBcc: x@evil.example"}`, "u1"); w.Code != 200 {
		t.Fatalf("send: %d %s", w.Code, w.Body)
	}
	msg := (*e.sent)[0]
	if !strings.HasPrefix(msg, "help@pantry.example.org\n") || !strings.Contains(msg, "Reply-To: u1@example.org\r\n") ||
		!strings.Contains(msg, "Subject: Beans Bcc: x@evil.example\r\n") || strings.Contains(msg, "\r\nBcc:") ||
		!strings.Contains(msg, "can you accept 24 sealed cans") {
		t.Fatalf("message %q", msg)
	}
	if w := send(`{"place_id":"a2"}`, "u1"); w.Code != 409 {
		t.Fatalf("second send to the same place: %d", w.Code)
	}
	if w := send(`{"place_id":"a3","to_email":"bank@example.org"}`, "u1"); w.Code != 200 {
		t.Fatalf("donor-given address: %d %s", w.Code, w.Body)
	}
	e.cfg.SendPerDay = 2
	if w := send(`{"place_id":"a1","to_email":"k@example.org"}`, "u1"); w.Code != 429 {
		t.Fatalf("per-day limit: %d", w.Code)
	}
	got, _ := e.cfg.Store.Get(res.RescueID, "u1")
	if len(got.Sent) != 2 {
		t.Fatalf("sent %v", got.Sent)
	}
}

func TestRescueSendDisabledAndRelayError(t *testing.T) {
	e := newRS(t, stableDraft, 200, false)
	defer e.stop()
	w := do(e.h, jsonReq("POST", "/api/rescue/l_000000000000000a", `{"zip":"10001"}`, "u1"))
	var res Rescue
	json.Unmarshal(w.Body.Bytes(), &res)
	if w := do(e.h, jsonReq("POST", "/api/rescue/"+res.RescueID+"/send", `{"place_id":"a2"}`, "u1")); w.Code != 503 ||
		!strings.Contains(w.Body.String(), CodeDisabled) {
		t.Fatalf("no relay: %d %s", w.Code, w.Body)
	}
	e.cfg.Mailer = &Mailer{Addr: "x:587", From: "i@example.org",
		send: func(string, smtp.Auth, string, []string, []byte) error { return errors.New("refused") }}
	if w := do(e.h, jsonReq("POST", "/api/rescue/"+res.RescueID+"/send", `{"place_id":"a2"}`, "u1")); w.Code != 502 {
		t.Fatalf("relay error: %d", w.Code)
	}
	if got, _ := e.cfg.Store.Get(res.RescueID, "u1"); len(got.Sent) != 0 {
		t.Fatal("a refused email was recorded as sent")
	}
	if w := do(e.h, jsonReq("GET", "/api/rescue/"+res.RescueID, "", "u1")); w.Code != 200 {
		t.Fatalf("GET: %d", w.Code)
	}
}

func TestParsePlacesAndLinks(t *testing.T) {
	if _, err := ParsePlaces([]byte(`{"nothing":[]}`)); err == nil {
		t.Fatal("no results array accepted")
	}
	ps, err := ParsePlaces([]byte(`{"items":[{"id":7,"name":"X","street":"1 A St","city":"B"}]}`))
	if err != nil || len(ps) != 1 || ps[0].ID != "7" || ps[0].Address != "1 A St, B" {
		t.Fatalf("%v %v", ps, err)
	}
	if telLink("12") != "" || telLink("+1 (212) 555-0100") != "tel:+12125550100" {
		t.Fatal("tel")
	}
	if m := mailtoLink("", Draft{Subject: "a b", Body: "c&d"}); m != "mailto:?subject=a%20b&body=c%26d" {
		t.Fatalf("mailto %s", m)
	}
}

func TestRescueStorePersists(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenRescues(dir)
	s.Put(Rescue{RescueID: "r1", OwnerID: "u1", Sent: []Sent{{PlaceID: "a", At: "2026-10-28T10:00:00Z"}}})
	s2, err := OpenRescues(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s2.Get("r1", "u1"); !ok {
		t.Fatal("not reloaded")
	}
	if n := s2.SentToday("u1", time.Date(2026, 10, 28, 23, 0, 0, 0, time.UTC)); n != 1 {
		t.Fatalf("sent today %d", n)
	}
}

func TestRescueOnePerListing(t *testing.T) {
	e := newRS(t, stableDraft, 200, false)
	defer e.stop()
	var ids []string
	for i := 0; i < 2; i++ {
		w := do(e.h, jsonReq("POST", "/api/rescue/l_000000000000000a", `{"zip":"10001"}`, "u1"))
		var res Rescue
		json.Unmarshal(w.Body.Bytes(), &res)
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		ids = append(ids, res.RescueID)
	}
	if ids[0] != ids[1] || len(*e.muse) != 1 {
		t.Fatalf("ids %v, %d Muse calls", ids, len(*e.muse))
	}
}

func TestRescueSendKnownAddressAndLimiter(t *testing.T) {
	e := newRS(t, stableDraft, 200, true)
	defer e.stop()
	w := do(e.h, jsonReq("POST", "/api/rescue/l_000000000000000a", `{"zip":"10001"}`, "u1"))
	var res Rescue
	json.Unmarshal(w.Body.Bytes(), &res)
	path := "/api/rescue/" + res.RescueID + "/send"
	if w := do(e.h, jsonReq("POST", path, `{"place_id":"a2","to_email":"spam@target.example"}`, "u1")); w.Code != 400 || len(*e.sent) != 0 {
		t.Fatalf("other address for a place with a known one: %d", w.Code)
	}
	e.cfg.SendLimiter = NewLimiter(1, time.Minute)
	if w := do(e.h, jsonReq("POST", path, `{"place_id":"a2","to_email":"HELP@pantry.example.org"}`, "u1")); w.Code != 200 {
		t.Fatalf("same address, other case: %d %s", w.Code, w.Body)
	}
	if w := do(e.h, jsonReq("POST", path, `{"place_id":"a3","to_email":"x@example.org"}`, "u1")); w.Code != 429 {
		t.Fatalf("send limiter: %d", w.Code)
	}
}

func TestMailerTimesOutOnASilentRelay(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close() // accepts, never answers
		}
	}()
	m := &Mailer{Addr: ln.Addr().String(), From: "i@example.org", Timeout: 200 * time.Millisecond}
	t0 := time.Now()
	if err := m.Send("a@example.org", "", "s", "b"); err == nil {
		t.Fatal("a silent relay did not fail")
	}
	if d := time.Since(t0); d > 2*time.Second {
		t.Fatalf("took %s", d)
	}
}
