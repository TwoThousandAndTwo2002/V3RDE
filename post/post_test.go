package post

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

// fakeAuth: cookie value "u1" is user u1.
type fakeAuth struct{}

func (fakeAuth) Who(r *http.Request) (User, bool) {
	ck, err := r.Cookie(SessionCookie)
	if err != nil || ck.Value == "" {
		return User{}, false
	}
	return User{ID: ck.Value, Email: ck.Value + "@example.org"}, true
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

func TestCallPrice(t *testing.T) {
	c := Call{Status: 200, InputTokens: 1_000_000, CachedTokens: 200_000, OutputTokens: 100_000}
	c.Price()
	want := (800_000*1.25 + 200_000*0.15 + 100_000*4.25) / 1e6
	if !near(c.CostUSD, want) {
		t.Fatalf("cost %v want %v", c.CostUSD, want)
	}
	im := Call{Status: 200, Images: 2}
	im.Price()
	if !near(im.CostUSD, 0.02) {
		t.Fatalf("image cost %v", im.CostUSD)
	}
	bad := Call{Status: 500, InputTokens: 10}
	bad.Price()
	if bad.CostUSD != 0 {
		t.Fatal("a failed call must be free")
	}
}

func TestBudgetReserveSettleAndRestore(t *testing.T) {
	dir := t.TempDir()
	ev := NewEvents(dir)
	ev.Record(Event{RequestID: "a", CostUSD: 0.5})
	b := NewBudget(1.0, 0.3, ev)
	if !near(b.Spent(), 0.5) {
		t.Fatalf("restored %v", b.Spent())
	}
	h, ok := b.Reserve()
	if !ok {
		t.Fatal("first reserve refused")
	}
	if _, ok := b.Reserve(); ok {
		t.Fatal("0.5 + 0.3 + 0.3 > 1.0 must be refused")
	}
	h.Settle(0.1)
	h.Settle(0.1) // once only
	if !near(b.Spent(), 0.6) {
		t.Fatalf("spent %v", b.Spent())
	}
	day := time.Now().Add(48 * time.Hour)
	b.now = func() time.Time { return day }
	if b.Spent() != 0 {
		t.Fatal("a new UTC day starts at 0")
	}
}

func TestIdentify(t *testing.T) {
	bearer := analyze.NewBearer("tok")
	r := httptest.NewRequest("POST", "/api/x", nil)
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: "u1"})
	c, ok := Identify(r, fakeAuth{}, bearer)
	if !ok || c.User.ID != "u1" || c.Bearer || c.RateKey != "u:u1" {
		t.Fatalf("session: %+v %v", c, ok)
	}
	r = httptest.NewRequest("POST", "/v1/x", nil)
	r.Header.Set("Authorization", "Bearer tok")
	c, ok = Identify(r, nil, bearer)
	if !ok || !c.Bearer || !strings.HasPrefix(c.RateKey, "ip:") {
		t.Fatalf("bearer: %+v %v", c, ok)
	}
	r.Header.Set("Authorization", "Bearer nope")
	if _, ok := Identify(r, nil, bearer); ok {
		t.Fatal("bad token accepted")
	}
	r = httptest.NewRequest("POST", "/api/x", nil)
	r.Header.Set("Authorization", "Bearer tok")
	if _, ok := Identify(r, fakeAuth{}, nil); ok {
		t.Fatal("bearer accepted where only the session is allowed")
	}
}

func listingsFake(t *testing.T, items []Listing) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie(SessionCookie)
		if err != nil {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.URL.Path == "/api/listings/mine":
			var mine []Listing
			for _, l := range items {
				if l.OwnerID == ck.Value {
					mine = append(mine, l)
				}
			}
			WriteJSON(w, 200, map[string]any{"items": mine})
		case strings.HasSuffix(r.URL.Path, "/photo"):
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write(tinyJPEG(t))
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestListingsClientMineIsOwnerOnly(t *testing.T) {
	ls := listingsFake(t, []Listing{{ID: "l_00000000000000aa", OwnerID: "u1", Title: "Rice"},
		{ID: "l_00000000000000bb", OwnerID: "u2", Title: "Soup"}})
	defer ls.Close()
	lc := &ListingsClient{URL: ls.URL}
	r := httptest.NewRequest("POST", "/api/x", nil)
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: "u1"})
	if l, err := lc.Mine(r, "l_00000000000000aa"); err != nil || l.Title != "Rice" {
		t.Fatalf("own listing: %v %v", l, err)
	}
	if _, err := lc.Mine(r, "l_00000000000000bb"); err != ErrNotOwned {
		t.Fatalf("other's listing: %v", err)
	}
	if _, err := lc.Mine(r, "../etc"); err != ErrNotOwned {
		t.Fatalf("bad id: %v", err)
	}
	if b, mime, err := lc.Photo(r, "l_00000000000000aa"); err != nil || mime != "image/jpeg" || len(b) == 0 {
		t.Fatalf("photo: %v %v", mime, err)
	}
}

func TestListingExpired(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	l := Listing{ExpiresAt: now.Add(-time.Second).Format(time.RFC3339Nano)}
	if !l.Expired(now) {
		t.Fatal("past expires_at must be expired")
	}
	l.ExpiresAt = now.Add(time.Minute).Format(time.RFC3339Nano)
	if l.Expired(now) {
		t.Fatal("future expires_at is not expired")
	}
}

// museFake answers /responses with text and records the request bodies.
func museFake(t *testing.T, text string, status int, got *[]map[string]any) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer key" {
			w.WriteHeader(404)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if got != nil {
			*got = append(*got, body)
		}
		if status != 200 {
			w.WriteHeader(status)
			io.WriteString(w, `{"error":"x"}`)
			return
		}
		WriteJSON(w, 200, map[string]any{"id": "resp_1",
			"output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": text}}}},
			"usage": map[string]any{"input_tokens": 1000, "output_tokens": 100,
				"input_tokens_details": map[string]any{"cached_tokens": 0}}})
	}))
}

func TestMuseAskJSON(t *testing.T) {
	var got []map[string]any
	srv := museFake(t, "Sure: {\"a\": 1}", 200, &got)
	defer srv.Close()
	m := &Muse{Base: srv.URL, Key: "key", Effort: "low"}
	obj, call, err := m.AskJSON(t.Context(), "p", "hello", Image{Bytes: []byte("x"), Mime: "image/jpeg"})
	if err != nil || obj["a"].(float64) != 1 {
		t.Fatalf("%v %v", obj, err)
	}
	if call.Model != DefaultModel || call.Status != 200 || call.UpstreamID != "resp_1" || call.CostUSD <= 0 {
		t.Fatalf("call %+v", call)
	}
	body := got[0]
	if body["reasoning"].(map[string]any)["effort"] != "low" {
		t.Fatalf("effort not sent: %v", body)
	}
	content := body["input"].([]any)[0].(map[string]any)["content"].([]any)
	if len(content) != 2 || !strings.HasPrefix(content[1].(map[string]any)["image_url"].(string), "data:image/jpeg;base64,") {
		t.Fatalf("content %v", content)
	}
	bad := museFake(t, "", 500, nil)
	defer bad.Close()
	m.Base = bad.URL
	if _, call, err := m.AskJSON(t.Context(), "p", "x"); err == nil || call.CostUSD != 0 {
		t.Fatal("HTTP 500 must fail and be free")
	}
}

func TestEventsFileIsDaily(t *testing.T) {
	dir := t.TempDir()
	ev := NewEvents(filepath.Join(dir, "sub"))
	ev.Record(Event{RequestID: "r1", CostUSD: 0.25})
	b, err := os.ReadFile(ev.Path(time.Now()))
	if err != nil || !strings.Contains(string(b), `"request_id":"r1"`) {
		t.Fatalf("%s %v", b, err)
	}
	if !near(ev.SpentOn(time.Now()), 0.25) {
		t.Fatal("SpentOn")
	}
}

func TestLimiterPrunesAndForwardedIsTrustedOnlyWhenSet(t *testing.T) {
	l := NewLimiter(2, time.Minute)
	base := time.Now()
	l.now = func() time.Time { return base }
	for i := 0; i < 1000; i++ {
		l.Allow("k" + strings.Repeat("x", i%50) + string(rune('a'+i%26)))
	}
	if !l.Allow("same") || !l.Allow("same") || l.Allow("same") {
		t.Fatal("burst 2")
	}
	l.now = func() time.Time { return base.Add(2 * time.Minute) }
	for i := 0; i < 1100; i++ {
		l.Allow("fresh")
	}
	if l.Keys() > 2 {
		t.Fatalf("stale keys kept: %d", l.Keys())
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.9:5555"
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	if ClientKey(r) != "10.0.0.9" {
		t.Fatal("forwarded header trusted by default")
	}
	TrustForwarded = true
	defer func() { TrustForwarded = false }()
	if ClientKey(r) != "203.0.113.7" {
		t.Fatal("forwarded header not used when trusted")
	}
}
