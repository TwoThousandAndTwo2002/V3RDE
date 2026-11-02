package post

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// X62 (T9 2026-11-02: "rapidly allow us to trouble shoot connectivity related issues"; "for each push we want
// latency and other stats"): every management API call is an events line with its AWS status, a code, the
// latency, what it was for and AWS's reason when it failed; healthz has the counts, the latency figures and the
// last failure; an unknown ping key and each notify are events lines too. The raw connection id is never written.
func TestPushStatsAndEvents(t *testing.T) {
	var mu sync.Mutex
	answers := map[string]int{"connS0000001": 200, "connS0000002": 403}
	mgmt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		time.Sleep(30 * time.Millisecond)
		mu.Lock()
		st := answers[r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]]
		mu.Unlock()
		if st == 403 {
			w.WriteHeader(403)
			_, _ = io.WriteString(w, `{"message":"User: arn:aws:sts::1:assumed-role/box is not authorized to perform: execute-api:ManageConnections"}`)
			return
		}
		w.WriteHeader(st)
	}))
	defer mgmt.Close()
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	cfg := &PushConfig{Session: &pushSession{ended: map[string]bool{}}, Secret: "sec", Origins: map[string]bool{"https://app.example": true},
		Endpoint: mgmt.URL + "/prod", URL: "wss://x/prod", Region: "us-east-1", Creds: fixedCreds{},
		Events: NewEvents(t.TempDir()), now: func() time.Time { return now }}
	cfg.Events.now = func() time.Time { return now }
	pub, in := NewPush(cfg)
	connect := func(id string) string {
		r := httptest.NewRequest(http.MethodPost, "/api/push/ticket", nil)
		r.AddCookie(&http.Cookie{Name: SessionCookie, Value: "s-u1"})
		w := httptest.NewRecorder()
		pub.ServeHTTP(w, r)
		var m map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		r = httptest.NewRequest(http.MethodPost, "/api/push/connect", nil)
		r.Header.Set("Authorization", "Bearer sec")
		r.Header.Set("X-Connection-Id", id)
		r.Header.Set("Origin", "https://app.example")
		r.Header.Set("X-Push-Ticket", m["ticket"].(string))
		w = httptest.NewRecorder()
		pub.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("connect %s: %d", id, w.Code)
		}
		return m["key"].(string)
	}
	ping := func(key string) {
		r := httptest.NewRequest(http.MethodPost, "/api/push/pingOn", strings.NewReader(`{"action":"ping","key":"`+key+`"}`))
		r.Header.Set("Authorization", "Bearer sec")
		w := httptest.NewRecorder()
		pub.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("ping %d", w.Code)
		}
	}
	ka, kb := connect("connS0000001"), connect("connS0000002")
	ping(ka)
	ping(kb)
	ping(strings.Repeat("0", 32)) // an unknown key
	ping(strings.Repeat("1", 32)) // a second one in the same minute: counted, no second line
	w := httptest.NewRecorder()
	in.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/push/notify", strings.NewReader(`{"user_id":"u1","kind":"touchup","id":"t1","status":"done"}`)))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"connections":2`) || !strings.Contains(w.Body.String(), `"sent":1`) {
		t.Fatalf("notify %d %s", w.Code, w.Body)
	}

	// healthz: the counts, the latency figures and the last failure
	w = httptest.NewRecorder()
	pub.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	var h struct {
		Stats struct {
			Sends, SendsOK, Pings, Pongs, UnknownKeys, Connects int
			ByStatus                                            map[string]int                       `json:"by_status"`
			Latency                                             struct{ Calls, P50, P95, Max int64 } `json:"latency_ms"`
			LastFailure                                         map[string]any                       `json:"last_failure"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &h); err != nil {
		t.Fatal(err)
	}
	s := h.Stats
	var raw map[string]map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &raw)
	st := raw["stats"]
	if st["sends"] != float64(4) || st["sends_ok"] != float64(2) || st["pings"] != float64(4) || st["pongs"] != float64(1) ||
		st["unknown_keys"] != float64(2) || st["connects"] != float64(2) {
		t.Fatalf("stats %v", st)
	}
	if s.ByStatus["200"] != 2 || s.ByStatus["403"] != 2 || s.Latency.Calls != 4 || s.Latency.P50 < 30 || s.Latency.Max < s.Latency.P95 {
		t.Fatalf("by status %v, latency %+v", s.ByStatus, s.Latency)
	}
	if s.LastFailure["status"] != float64(403) || !strings.Contains(s.LastFailure["why"].(string), "not authorized to perform: execute-api:ManageConnections") {
		t.Fatalf("last failure %v", s.LastFailure)
	}

	// the events: one line per call, with the code, the latency and the reason; never the raw connection id
	ev := readAll(t, cfg.Events.Path(now))
	for _, want := range []string{`"route":"push.send"`, `"code":"forbidden"`, `"what":"pong"`, `"what":"notify:touchup"`,
		`"route":"push.ping"`, `"code":"unknown_key"`, `"route":"push.notify"`, `"conn":"` + connRef("connS0000002") + `"`} {
		if !strings.Contains(ev, want) {
			t.Errorf("events have no %s", want)
		}
	}
	if strings.Contains(ev, "connS0000001") || strings.Contains(ev, "connS0000002") {
		t.Error("a raw connection id is in the events")
	}
	n := 0
	for _, l := range strings.Split(strings.TrimSpace(ev), "\n") {
		var e map[string]any
		if json.Unmarshal([]byte(l), &e) == nil && e["route"] == "push.send" {
			n++
			if ms, _ := e["latency_ms"].(float64); ms < 30 {
				t.Errorf("a send with latency %v ms (the fake AWS waits 30 ms): %s", e["latency_ms"], l)
			}
		}
	}
	if n != 3 { // X62 review: a pong that works is counted, not written (one a minute for each socket)
		t.Fatalf("%d push.send lines, want 3 (the failed pong, 2 notify)", n)
	}
	if strings.Count(ev, `"code":"unknown_key"`) != 1 {
		t.Fatal("unknown keys: want one line a minute")
	}
	if !strings.Contains(ev, `"user_id":"u1"`) || strings.Contains(ev, `"route":"push.send","user_id":""`) {
		t.Error("the pong of the key path has no user")
	}
	if awsCode(410) != "gone" || awsCode(0) != "no_answer" || awsCode(500) != "aws_500" || awsCode(204) != "" || len(connRef("x")) != 12 {
		t.Fatal("awsCode / connRef")
	}
}

// X62 review: a 410 (the socket closed by itself) is not a failure; a network error keeps the connection id out;
// AWS's text is cut before a signed request and at the first line end; a close says its reason.
func TestPushCallFailures(t *testing.T) {
	var mu sync.Mutex
	status := 410
	mgmt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		st := status
		mu.Unlock()
		w.WriteHeader(st)
		if st == 403 {
			_, _ = io.WriteString(w, `{"message":"The request signature we calculated does not match.\n\nThe Canonical String for this request should have been\n'POST\n/prod/%40connections/connSECRET01\nx-amz-security-token:TOKEN'"}`)
		}
	}))
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	cfg := &PushConfig{Endpoint: mgmt.URL + "/prod", Region: "us-east-1", Creds: fixedCreds{}, Events: NewEvents(t.TempDir()),
		now: func() time.Time { return now }}
	cfg.Events.now = func() time.Time { return now }
	cfg.init()
	if cfg.post(context.Background(), "connSECRET01", []byte(`{"kind":"pong"}`), "pong", "u1") {
		t.Fatal("a 410 was sent")
	}
	if lf, _ := cfg.stats.snapshot()["last_failure"].(map[string]any); lf != nil {
		t.Fatalf("a 410 is a failure: %v", lf)
	}
	mu.Lock()
	status = 403
	mu.Unlock()
	cfg.post(context.Background(), "connSECRET01", []byte(`{"kind":"touchup"}`), "notify:touchup", "u1")
	f := cfg.stats.snapshot()["last_failure"].(map[string]any)
	if f["status"] != 403 || f["why"] != "The request signature we calculated does not match." {
		t.Fatalf("last failure %v", f)
	}
	cfg.closeConn("connSECRET02", "max_age", "u1")
	mgmt.Close() // now the network fails
	cfg.post(context.Background(), "connSECRET03", []byte(`{}`), "notify:scan", "u1")
	f = cfg.stats.snapshot()["last_failure"].(map[string]any)
	if f["status"] != 0 || !strings.HasPrefix(f["why"].(string), "network: ") {
		t.Fatalf("network failure %v", f)
	}
	ev := readAll(t, cfg.Events.Path(now))
	for _, bad := range []string{"connSECRET", "TOKEN", "Canonical"} {
		if strings.Contains(ev, bad) {
			t.Errorf("the events have %q: %s", bad, ev)
		}
	}
	for _, want := range []string{`"code":"forbidden"`, `"what":"close:max_age"`, `"gone":true`, `"code":"no_answer"`} {
		if !strings.Contains(ev, want) {
			t.Errorf("the events have no %s", want)
		}
	}
	if got := awsReason([]byte("line one\nline two")); got != "line one" {
		t.Fatalf("awsReason %q", got)
	}
	// push off: a close still writes its reason
	off := &PushConfig{Events: NewEvents(t.TempDir()), now: func() time.Time { return now }}
	off.Events.now = func() time.Time { return now }
	off.init()
	off.closeConn("connSECRET04", "session_ended", "u2")
	if l := readAll(t, off.Events.Path(now)); !strings.Contains(l, `"code":"session_ended"`) || strings.Contains(l, "connSECRET") {
		t.Fatalf("off close line %s", l)
	}
}
