package post

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// pushSession: the cookie value "s-<user>" is that user's session; ended
// sessions are in the map.
type pushSession struct {
	mu    sync.Mutex
	ended map[string]bool
}

func (p *pushSession) Who(r *http.Request) (User, bool) {
	ck, err := r.Cookie(SessionCookie)
	if err != nil || !strings.HasPrefix(ck.Value, "s-") {
		return User{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ended[ck.Value] {
		return User{}, false
	}
	return User{ID: strings.TrimPrefix(ck.Value, "s-")}, true
}

type fixedCreds struct{}

func (fixedCreds) Creds(context.Context) (AWSCreds, error) {
	return AWSCreds{AccessKeyID: "AKID", SecretAccessKey: "SK", SessionToken: "TK"}, nil
}

type mgmtCall struct{ method, path, body, auth string }

func TestPush(t *testing.T) {
	var mu sync.Mutex
	var calls []mgmtCall
	gone := map[string]bool{}
	mgmt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, mgmtCall{r.Method, r.URL.EscapedPath(), string(b), r.Header.Get("Authorization")})
		g := gone[r.URL.Path]
		mu.Unlock()
		if g {
			w.WriteHeader(http.StatusGone)
		}
	}))
	defer mgmt.Close()
	sess := &pushSession{ended: map[string]bool{}}
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	cfg := &PushConfig{Session: sess, Secret: "sec", Origins: map[string]bool{"https://app.example": true},
		Endpoint: mgmt.URL + "/prod", Region: "us-east-1", URL: "wss://push.example/prod", Creds: fixedCreds{}, MaxPerUser: 2,
		Events: NewEvents(t.TempDir()), now: func() time.Time { return now },
		StaleAfter: 24 * time.Hour} // X57: the session and age rules alone (the stale rule: TestPushPingByKey)
	cfg.Events.now = func() time.Time { return now } // X58 review: the events file is named by the fake date
	pub, in := NewPush(cfg)
	ticket := func(cookie string) (int, string) {
		r := httptest.NewRequest(http.MethodPost, "/api/push/ticket", nil)
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: SessionCookie, Value: cookie})
		}
		w := httptest.NewRecorder()
		pub.ServeHTTP(w, r)
		var m map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		tk, _ := m["ticket"].(string)
		if w.Code == 200 && (m["url"] != "wss://push.example/prod" || len(tk) != 64) {
			t.Fatalf("ticket answer %s", w.Body)
		}
		return w.Code, tk
	}
	connectT := func(id, secret, origin, tk string) int {
		r := httptest.NewRequest(http.MethodPost, "/api/push/connect", nil)
		r.Header.Set("Authorization", "Bearer "+secret)
		r.Header.Set("X-Connection-Id", id)
		r.Header.Set("Origin", origin)
		r.Header.Set("X-Push-Ticket", tk)
		w := httptest.NewRecorder()
		pub.ServeHTTP(w, r)
		return w.Code
	}
	connect := func(id, secret, origin, cookie string) int {
		tk := ""
		if cookie != "" {
			if code, x := ticket(cookie); code == 200 {
				tk = x
			}
		}
		return connectT(id, secret, origin, tk)
	}
	if code, _ := ticket(""); code != 401 {
		t.Fatalf("ticket without a session: %d", code)
	}
	if code, _ := ticket("nope"); code != 401 {
		t.Fatalf("ticket with a bad session: %d", code)
	}
	// a ticket is good once, and for TicketTTL
	_, tk := ticket("s-u9")
	if connectT("connT0000001", "sec", "https://app.example", tk) != 200 || connectT("connT0000002", "sec", "https://app.example", tk) != 401 {
		t.Fatal("a ticket used twice")
	}
	_, tk = ticket("s-u9")
	now = now.Add(TicketTTL + time.Second)
	if connectT("connT0000003", "sec", "https://app.example", tk) != 401 {
		t.Fatal("an old ticket")
	}
	_, tk = ticket("s-u9")
	if connectT("connT0000004", "sec", "https://evil.example", tk) != 403 || connectT("connT0000004", "sec", "https://app.example", tk) != 200 {
		t.Fatal("a connect refused for its Origin does not use the ticket (the Origin is checked first)")
	}
	for _, tc := range []struct {
		id, secret, origin, cookie string
		want                       int
	}{
		{"conn0000001=", "bad", "https://app.example", "s-u1", 403},
		{"conn0000001=", "", "https://app.example", "s-u1", 403},
		{"bad id!", "sec", "https://app.example", "s-u1", 400},
		{"conn0000001=", "sec", "https://evil.example", "s-u1", 403}, // cross-site WebSocket hijacking
		{"conn0000001=", "sec", "", "s-u1", 403},
		{"conn0000001=", "sec", "https://app.example", "", 401},
		{"conn0000001=", "sec", "https://app.example", "nope", 401},
	} {
		if got := connect(tc.id, tc.secret, tc.origin, tc.cookie); got != tc.want {
			t.Errorf("connect %+v: %d", tc, got)
		}
	}
	now = now.Add(TicketTTL + time.Second) // the tickets of the refused connects end (each user has at most 2 x MaxPerUser)
	if code, _ := ticket("s-u1"); code != 200 {
		t.Fatalf("a ticket after the old ones ended: %d", code)
	}
	if connect("connA0000001", "sec", "https://app.example", "s-u1") != 200 ||
		connect("connB0000001", "sec", "https://app.example", "s-u1") != 200 ||
		connect("connC0000001", "sec", "https://app.example", "s-u2") != 200 {
		t.Fatal("good connects")
	}
	// a connection id is never given to a second user
	if connect("connC0000001", "sec", "https://app.example", "s-u3") != 409 {
		t.Fatal("a taken connection id")
	}
	// the secret travels as Authorization, not in another header
	r0 := httptest.NewRequest(http.MethodPost, "/api/push/disconnect", nil)
	r0.Header.Set("X-Push-Secret", "sec")
	w0 := httptest.NewRecorder()
	pub.ServeHTTP(w0, r0)
	if w0.Code != 403 {
		t.Fatal("the secret in another header")
	}
	ping := func(id string) int {
		r := httptest.NewRequest(http.MethodPost, "/api/push/ping", nil)
		r.Header.Set("Authorization", "Bearer sec")
		r.Header.Set("X-Connection-Id", id)
		w := httptest.NewRecorder()
		pub.ServeHTTP(w, r)
		return w.Code
	}
	mu.Lock()
	calls = nil
	mu.Unlock()
	if ping("connC0000001") != 200 || ping("connZ0000001") != 200 {
		t.Fatal("ping")
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	var pong, closeUnknown bool
	for _, c := range calls {
		pong = pong || (c.method == "POST" && c.path == "/prod/%40connections/connC0000001" && c.body == `{"kind":"pong"}`)
		closeUnknown = closeUnknown || (c.method == "DELETE" && c.path == "/prod/%40connections/connZ0000001")
	}
	if !pong || !closeUnknown || len(calls) != 2 {
		t.Fatalf("ping calls %+v", calls)
	}
	calls = nil
	mu.Unlock()
	notify := func(h http.Handler, body string) (int, map[string]any) {
		r := httptest.NewRequest(http.MethodPost, "/v1/push/notify", strings.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var m map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		return w.Code, m
	}
	code, m := notify(in, `{"user_id":"u1","kind":"rescue","id":"r_abc","status":"done"}`)
	if code != 200 || m["connections"] != 2.0 || m["sent"] != 2.0 {
		t.Fatalf("notify %d %v", code, m)
	}
	mu.Lock()
	if len(calls) != 2 || calls[0].method != "POST" || !strings.HasPrefix(calls[0].path, "/prod/%40connections/conn") ||
		calls[0].body != `{"kind":"rescue","id":"r_abc","status":"done"}` || !strings.Contains(calls[0].auth, "/us-east-1/execute-api/aws4_request") {
		t.Fatalf("management calls %+v", calls)
	}
	calls = nil
	mu.Unlock()
	// the public handler has no notify; the internal one has no connect
	if code, _ := notify(pub, `{"user_id":"u1","kind":"rescue"}`); code == 200 {
		t.Error("notify on the public handler")
	}
	r := httptest.NewRequest(http.MethodPost, "/api/push/connect", nil)
	w := httptest.NewRecorder()
	in.ServeHTTP(w, r)
	if w.Code == 200 {
		t.Error("connect on the internal handler")
	}
	for _, bad := range []string{`{"kind":"rescue"}`, `{"user_id":"u1","kind":"other"}`, `{"user_id":"u1","kind":"scan","id":"../x"}`, `not json`} {
		if code, _ := notify(in, bad); code != 400 {
			t.Errorf("notify %s: %d", bad, code)
		}
	}
	// a third connection of u1 closes the oldest
	if connect("connD0000001", "sec", "https://app.example", "s-u1") != 200 {
		t.Fatal("third connect")
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if len(calls) != 1 || calls[0].method != "DELETE" || calls[0].path != "/prod/%40connections/connA0000001" {
		t.Fatalf("the oldest is closed: %+v", calls)
	}
	calls = nil
	gone["/prod/@connections/connB0000001"] = true
	mu.Unlock()
	// 410: the connection is forgotten
	if _, m := notify(in, `{"user_id":"u1","kind":"touchup","id":"t1","status":"done"}`); m["sent"] != 1.0 {
		t.Fatalf("410: %v", m)
	}
	if _, m := notify(in, `{"user_id":"u1","kind":"touchup"}`); m["connections"] != 1.0 {
		t.Fatalf("after 410: %v", m)
	}
	// $disconnect needs the secret
	dis := func(id, secret string) int {
		r := httptest.NewRequest(http.MethodPost, "/api/push/disconnect", nil)
		r.Header.Set("Authorization", "Bearer "+secret)
		r.Header.Set("X-Connection-Id", id)
		w := httptest.NewRecorder()
		pub.ServeHTTP(w, r)
		return w.Code
	}
	if dis("connD0000001", "x") != 403 || dis("connD0000001", "sec") != 200 {
		t.Fatal("disconnect")
	}
	if _, m := notify(in, `{"user_id":"u1","kind":"scan"}`); m["connections"] != 0.0 {
		t.Fatalf("after disconnect: %v", m)
	}
	// the sweep: u2 logs out -> its connection is closed; before Recheck nothing is asked
	mu.Lock()
	calls = nil
	mu.Unlock()
	if n := cfg.Sweep(context.Background()); n != 0 {
		t.Fatalf("sweep before Recheck closed %d", n)
	}
	sess.mu.Lock()
	sess.ended["s-u2"] = true
	sess.mu.Unlock()
	now = now.Add(2 * time.Minute)
	if n := cfg.Sweep(context.Background()); n != 1 {
		t.Fatalf("sweep closed %d", n)
	}
	mu.Lock()
	if len(calls) != 1 || calls[0].method != "DELETE" || calls[0].path != "/prod/%40connections/connC0000001" {
		t.Fatalf("sweep calls %+v", calls)
	}
	mu.Unlock()
	// MaxAge
	if connect("connE0000001", "sec", "https://app.example", "s-u3") != 200 {
		t.Fatal("connect u3")
	}
	now = now.Add(3 * time.Hour)
	if n := cfg.Sweep(context.Background()); n != 3 { // u3, and the two of u9 (older than MaxAge)
		t.Fatalf("max age: %d", n)
	}
	// the events never have the cookie
	b, _ := io.ReadAll(strings.NewReader(readAll(t, cfg.Events.Path(now.Add(-3*time.Hour)))))
	if strings.Contains(string(b), "s-u1") || !strings.Contains(string(b), `"route":"push.connect"`) || !strings.Contains(string(b), "bad_origin") {
		t.Fatalf("events %s", b)
	}
}

func readAll(t *testing.T, path string) string {
	t.Helper()
	b, err := readFileOK(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPushClient(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- r.URL.Path + " " + string(b)
	}))
	defer srv.Close()
	(&PushClient{URL: srv.URL}).Notify(PushMessage{UserID: "u1", Kind: "rescue", ID: "r1", Status: "done"})
	select {
	case s := <-got:
		if s != `/v1/push/notify {"user_id":"u1","kind":"rescue","id":"r1","status":"done"}` {
			t.Fatal(s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no notify")
	}
	var nilClient *PushClient
	nilClient.Notify(PushMessage{UserID: "u1", Kind: "scan"}) // off: no panic
}

func readFileOK(p string) (string, error) {
	b, err := os.ReadFile(p)
	return string(b), err
}

// errSession: WhoErr gives "no clear answer" while down is true.
type errSession struct {
	pushSession
	down bool
}

func (e *errSession) WhoErr(r *http.Request) (User, error) {
	if e.down {
		return User{}, io.ErrUnexpectedEOF
	}
	if u, ok := e.Who(r); ok {
		return u, nil
	}
	return User{}, ErrNoSession
}

// An auth that does not answer closes nothing; a clear "no session" does.
func TestPushSweepNeedsAClearAnswer(t *testing.T) {
	mgmt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer mgmt.Close()
	sess := &errSession{pushSession: pushSession{ended: map[string]bool{}}}
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	cfg := &PushConfig{Session: sess, Secret: "sec", Origins: map[string]bool{"https://app.example": true},
		Endpoint: mgmt.URL + "/prod", URL: "wss://x/prod", Region: "us-east-1", Creds: fixedCreds{},
		Events: NewEvents(t.TempDir()), now: func() time.Time { return now },
		StaleAfter: 24 * time.Hour} // X57: the session and age rules alone (the stale rule: TestPushPingByKey)
	pub, _ := NewPush(cfg)
	r := httptest.NewRequest(http.MethodPost, "/api/push/ticket", nil)
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: "s-u1"})
	w := httptest.NewRecorder()
	pub.ServeHTTP(w, r)
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	r = httptest.NewRequest(http.MethodPost, "/api/push/connect", nil)
	r.Header.Set("Authorization", "Bearer sec")
	r.Header.Set("X-Connection-Id", "connA0000001")
	r.Header.Set("Origin", "https://app.example")
	r.Header.Set("X-Push-Ticket", m["ticket"].(string))
	w = httptest.NewRecorder()
	pub.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("connect %d", w.Code)
	}
	sess.down = true
	now = now.Add(2 * time.Minute)
	if n := cfg.Sweep(context.Background()); n != 0 {
		t.Fatalf("auth down closed %d", n)
	}
	sess.down = false
	sess.ended["s-u1"] = true
	now = now.Add(2 * time.Minute)
	if n := cfg.Sweep(context.Background()); n != 1 {
		t.Fatalf("an ended session: closed %d", n)
	}
}

func TestAuthClientWhoErr(t *testing.T) {
	status := 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		if status == 200 {
			_, _ = w.Write([]byte(`{"id":"u1","email":"a@b.c"}`))
		}
	}))
	defer srv.Close()
	a := &AuthClient{URL: srv.URL}
	req := func(cookie bool) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if cookie {
			r.AddCookie(&http.Cookie{Name: SessionCookie, Value: "x"})
		}
		return r
	}
	if u, err := a.WhoErr(req(true)); err != nil || u.ID != "u1" {
		t.Fatalf("200: %v %v", u, err)
	}
	if _, err := a.WhoErr(req(false)); err != ErrNoSession {
		t.Fatalf("no cookie: %v", err)
	}
	status = 401
	if _, err := a.WhoErr(req(true)); err != ErrNoSession {
		t.Fatalf("401: %v", err)
	}
	status = 503
	if _, err := a.WhoErr(req(true)); err == nil || err == ErrNoSession {
		t.Fatalf("503: %v", err)
	}
	if _, ok := a.Who(req(true)); ok {
		t.Fatal("Who on 503")
	}
	srv.Close()
	if _, err := a.WhoErr(req(true)); err == nil || err == ErrNoSession {
		t.Fatalf("no answer: %v", err)
	}
}

// X57 (the SME's set-up): $default posts to /api/push/pingOn with no X-Connection-Id; the ping's key finds the
// connection; a wrong key gets no pong; pings by key that stop are forgotten after StaleAfter.
func TestPushPingByKey(t *testing.T) {
	var mu sync.Mutex
	var calls []mgmtCall
	mgmt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, mgmtCall{method: r.Method, path: r.URL.EscapedPath(), body: string(b)})
		mu.Unlock()
	}))
	defer mgmt.Close()
	sess := &pushSession{ended: map[string]bool{}}
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	cfg := &PushConfig{Session: sess, Secret: "sec", Origins: map[string]bool{"https://app.example": true},
		Endpoint: mgmt.URL + "/prod", URL: "wss://x/prod", Region: "us-east-1", Creds: fixedCreds{},
		Events: NewEvents(t.TempDir()), now: func() time.Time { return now }}
	cfg.Events.now = func() time.Time { return now } // X58 review: the events file is named by the fake date
	pub, _ := NewPush(cfg)
	r := httptest.NewRequest(http.MethodPost, "/api/push/ticket", nil)
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: "s-u1"})
	w := httptest.NewRecorder()
	pub.ServeHTTP(w, r)
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	key, _ := m["key"].(string)
	if w.Code != 200 || len(key) != 32 || key == m["ticket"] {
		t.Fatalf("ticket %d %v", w.Code, m)
	}
	r = httptest.NewRequest(http.MethodPost, "/api/push/connect", nil)
	r.Header.Set("Authorization", "Bearer sec")
	r.Header.Set("X-Connection-Id", "connK0000001")
	r.Header.Set("Origin", "https://app.example")
	r.Header.Set("X-Push-Ticket", m["ticket"].(string))
	w = httptest.NewRecorder()
	pub.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("connect %d", w.Code)
	}
	ping := func(path, auth, body string) int {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+auth)
		w := httptest.NewRecorder()
		pub.ServeHTTP(w, r)
		return w.Code
	}
	pongs := func() (n int) {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range calls {
			if c.method == "POST" && c.path == "/prod/%40connections/connK0000001" && c.body == `{"kind":"pong"}` {
				n++
			}
		}
		return n
	}
	if ping("/api/push/pingOn", "wrong", `{"action":"ping","key":"`+key+`"}`) != 403 || pongs() != 0 {
		t.Fatal("a ping with a wrong secret")
	}
	if ping("/api/push/pingOn", "sec", `{"action":"ping","key":"`+strings.Repeat("0", 32)+`"}`) != 200 || pongs() != 0 {
		t.Fatal("a wrong key got a pong")
	}
	if ping("/api/push/pingOn", "sec", `{"action":"ping","key":"`+key+`"}`) != 200 || pongs() != 1 {
		t.Fatal("pingOn by key: no pong")
	}
	if ping("/api/push/ping", "sec", `{"action":"ping","key":"`+key+`"}`) != 200 || pongs() != 2 {
		t.Fatal("ping by key: no pong")
	}
	now = now.Add(2 * time.Minute) // a ping each minute keeps it
	cfg.Sweep(context.Background())
	if ping("/api/push/pingOn", "sec", `{"action":"ping","key":"`+key+`"}`) != 200 || pongs() != 3 {
		t.Fatal("kept connection: no pong")
	}
	// a second socket of the user that never pings (it closed at once): forgotten 3 min after its connect
	r = httptest.NewRequest(http.MethodPost, "/api/push/ticket", nil)
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: "s-u1"})
	w = httptest.NewRecorder()
	pub.ServeHTTP(w, r)
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	r = httptest.NewRequest(http.MethodPost, "/api/push/connect", nil)
	r.Header.Set("Authorization", "Bearer sec")
	r.Header.Set("X-Connection-Id", "connK0000002")
	r.Header.Set("Origin", "https://app.example")
	r.Header.Set("X-Push-Ticket", m["ticket"].(string))
	pub.ServeHTTP(httptest.NewRecorder(), r)
	now = now.Add(3 * time.Minute) // the sockets closed ($disconnect with no id): no more pings
	cfg.Sweep(context.Background())
	cfg.mu.Lock()
	n := len(cfg.conns)
	cfg.mu.Unlock()
	if n != 0 {
		t.Fatalf("a connection with no ping for 3 min is kept (%d)", n)
	}
	mu.Lock()
	deletes := 0
	for _, x := range calls {
		if x.method == "DELETE" {
			deletes++
		}
	}
	mu.Unlock()
	if deletes != 2 {
		t.Fatalf("the stale sockets were not closed: %d DELETE", deletes)
	}
	if !strings.Contains(readAll(t, cfg.Events.Path(now)), `"route":"push.stale"`) {
		t.Fatal("no push.stale event")
	}
}
