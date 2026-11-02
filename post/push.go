package post

// WS1 (T9 2026-11-01: "lets move forward with aws socket"): the push service
// behind an API Gateway WebSocket API. API Gateway holds the WebSocket of
// the app; this service:
//
//   - POST /api/push/ticket (the app, signed in, through Caddy): a one-time
//     ticket (32 random bytes, 60 s) and the WebSocket URL. The WebSocket host
//     is not the app's host, so the browser does not send the session cookie
//     there; the ticket stands for it, once.
//   - POST /api/push/connect (the $connect route's HTTP integration): checks
//     the integration's shared secret, the Origin of the browser and the
//     ticket (the ?ticket= of the WebSocket URL), and keeps connection id ->
//     user. Any other answer than 200 makes API Gateway refuse the WebSocket.
//   - POST /api/push/disconnect (the $disconnect route): forgets the id.
//   - X57 (the SME's set-up, 2026-11-01): the $default route posts to
//     /api/push/pingOn (the same handler as /api/push/ping), and only
//     $connect maps X-Connection-Id. So the ticket answer also has a "key"
//     (16 random bytes) that $connect keeps with the connection, and the app
//     sends {"action":"ping","key":"<key>"}: the ping finds its connection by
//     the header when it is there, else by the key. A $disconnect with no id
//     cannot say which socket closed: a connection with no ping (by key or
//     by header) for StaleAfter since its connect or its last ping is
//     forgotten and closed at the next sweep, and a post that gets 410 Gone
//     forgets it at once.
//   - POST /api/push/ping (the $default route: the app sends "ping" every
//     minute): a known connection gets {"kind":"pong"}; an unknown one (the
//     service restarted and lost its list) is closed, so the app opens a new
//     one. The app counts the socket as working only while pongs come, so a
//     broken push (no credentials, a lost list) never makes its polls slow.
//   - The integrations carry the shared secret as "Authorization: Bearer
//     <secret>": Caddy's access log hides that header, not other ones.
//   - POST /v1/push/notify, on the unix socket only (the services of the box):
//     {"user_id", "kind", "id", "status"}: a message to every connection of
//     that user, through the management API (POST @connections/{id}, SigV4
//     with the box role). A message has only a kind, an id and a status: the
//     app then reads the thing through its usual signed-in route.
//   - every Recheck the session of each connection is asked again; a session
//     that ended (logout, expiry) closes the connection (DELETE @connections).
//
// The app keeps its polling: a message only makes it poll at once.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The push message kinds (what the app polls again).
var PushKinds = map[string]bool{"scan": true, "rescue": true, "touchup": true, "listing": true, "listings": true}

// PushConfig is the push service.
type PushConfig struct {
	Session Authenticator // auth /api/auth/me
	Secret  string        // the shared secret the API Gateway integrations send (Authorization: Bearer)
	Origins map[string]bool
	// Endpoint is the management API base: https://{api-id}.execute-api.{region}.amazonaws.com/{stage}
	Endpoint   string
	Region     string
	URL        string // the WebSocket URL the app opens (wss://...); "" = push off (the ticket answers 404)
	Creds      CredSource
	HTTP       *http.Client
	MaxPerUser int           // connections of one user (4); the oldest is closed
	Recheck    time.Duration // the session check of each connection (1 min)
	MaxAge     time.Duration // API Gateway closes a connection after 2 h
	StaleAfter time.Duration // X57: a connection with no ping this long is forgotten (3 min: the app pings each minute)
	Events     *Events
	Build      string
	now        func() time.Time

	stats pushStats // X62: the management API calls (each one is also an events line)

	mu      sync.Mutex
	conns   map[string]*pushConn // by connection id
	tickets map[string]pushTicket
	seq     int64
}

type pushTicket struct {
	user, cookie, key string
	exp               time.Time
}

// pushStats (X62, T9 2026-11-02: "rapidly allow us to trouble shoot connectivity related issues"; "for each push we
// want latency and other stats") counts the management API calls since the start: by status, the failures, the
// latency of the last LatencyWindow calls (p50, p95, max), the last failure, and the pings.
type pushStats struct {
	mu                        sync.Mutex
	sends, sendsOK, closes    int
	byStatus                  map[int]int // 0 = no answer
	lat                       []int64     // ms, a ring of the last LatencyWindow calls
	next                      int
	lastFail                  map[string]any
	pings, unknownKeys, pongs int
	lastUnknown               time.Time
	connects, refusedConnects int
}

// LatencyWindow is how many calls the latency figures cover.
const LatencyWindow = 256

func (s *pushStats) add(method string, status int, ms int64, fail map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byStatus == nil {
		s.byStatus = map[int]int{}
	}
	s.byStatus[status]++
	if method == http.MethodPost {
		s.sends++
		if status/100 == 2 {
			s.sendsOK++
		}
	} else {
		s.closes++
	}
	if len(s.lat) < LatencyWindow {
		s.lat = append(s.lat, ms)
	} else {
		s.lat[s.next] = ms
	}
	s.next = (s.next + 1) % LatencyWindow
	if fail != nil {
		s.lastFail = fail
	}
}

// unknownLine reports whether an unknown-key ping may be an events line now: at most one a minute (X62 review:
// a socket can send pings as fast as API Gateway lets it).
func (s *pushStats) unknownLine(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.lastUnknown.IsZero() && now.Sub(s.lastUnknown) < time.Minute {
		return false
	}
	s.lastUnknown = now
	return true
}

func (s *pushStats) count(f func(*pushStats)) {
	s.mu.Lock()
	f(s)
	s.mu.Unlock()
}

// snapshot is the healthz part: the counts, the latency figures (ms) and the last failure.
func (s *pushStats) snapshot() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	by := map[string]int{}
	for k, v := range s.byStatus {
		by[strconv.Itoa(k)] = v
	}
	lat := append([]int64(nil), s.lat...)
	sort.Slice(lat, func(a, b int) bool { return lat[a] < lat[b] })
	pct := func(p float64) int64 {
		if len(lat) == 0 {
			return 0
		}
		i := int(math.Ceil(p*float64(len(lat)))) - 1
		if i < 0 {
			i = 0
		}
		return lat[i]
	}
	var max int64
	if len(lat) > 0 {
		max = lat[len(lat)-1]
	}
	return map[string]any{"sends": s.sends, "sends_ok": s.sendsOK, "closes": s.closes, "by_status": by,
		"latency_ms":   map[string]any{"calls": len(lat), "p50": pct(0.5), "p95": pct(0.95), "max": max},
		"last_failure": s.lastFail, "pings": s.pings, "pongs": s.pongs, "unknown_keys": s.unknownKeys,
		"connects": s.connects, "refused_connects": s.refusedConnects}
}

// TicketTTL is how long a ticket is good for.
const TicketTTL = 60 * time.Second

type pushConn struct {
	id, user, cookie string
	key              string // X57: the ping key of the ticket (the app's pings carry it)
	at, checked      time.Time
	seen             time.Time // X57: the connect or the last ping
	seq              int64     // the order of the connects
}

var connIDRe = regexp.MustCompile(`^[A-Za-z0-9_=+/-]{8,128}$`)

func (c *PushConfig) init() {
	if c.now == nil {
		c.now = time.Now
	}
	if c.MaxPerUser <= 0 {
		c.MaxPerUser = 4
	}
	if c.Recheck <= 0 {
		c.Recheck = time.Minute
	}
	if c.MaxAge <= 0 {
		c.MaxAge = 2 * time.Hour
	}
	if c.StaleAfter <= 0 {
		c.StaleAfter = 3 * time.Minute
	}
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 5 * time.Second}
	}
	if c.conns == nil {
		c.conns = map[string]*pushConn{}
	}
	if c.tickets == nil {
		c.tickets = map[string]pushTicket{}
	}
}

// NewPush is the public handler (connect, disconnect, healthz) and the
// internal one (notify, healthz) for the unix socket.
func NewPush(c *PushConfig) (public, internal http.Handler) {
	c.init()
	health := func(w http.ResponseWriter, _ *http.Request) {
		c.mu.Lock()
		n := len(c.conns)
		c.mu.Unlock()
		WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "push", "build": c.Build,
			"connections": n, "endpoint": c.Endpoint != "", "stats": c.stats.snapshot()}) // X62
	}
	pub := http.NewServeMux()
	pub.HandleFunc("GET /healthz", health)
	pub.HandleFunc("POST /api/push/ticket", c.ticket)
	pub.HandleFunc("POST /api/push/connect", c.connect)
	pub.HandleFunc("POST /api/push/disconnect", c.disconnect)
	pub.HandleFunc("POST /api/push/ping", c.ping)
	pub.HandleFunc("POST /api/push/pingOn", c.ping) // X57: the $default route of the SME's set-up
	in := http.NewServeMux()
	in.HandleFunc("GET /healthz", health)
	in.HandleFunc("POST /v1/push/notify", c.notify)
	return pub, in
}

func (c *PushConfig) secretOK(r *http.Request) bool {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && c.Secret != "" && subtle.ConstantTimeCompare([]byte(got), []byte(c.Secret)) == 1
}

func (c *PushConfig) record(r *http.Request, route string, status int, user, code string, detail map[string]any) {
	ev := Event{TS: c.now().UTC().Format(time.RFC3339Nano), Service: "push", Build: c.Build, Route: route,
		UserID: user, Status: status, Code: code, Detail: detail}
	if r != nil {
		ev.From(r)
	}
	c.Events.Record(ev)
}

// ticket gives a signed-in app a one-time ticket for the WebSocket.
func (c *PushConfig) ticket(w http.ResponseWriter, r *http.Request) {
	if c.URL == "" || c.Endpoint == "" {
		http.NotFound(w, r)
		return
	}
	u, ok := c.Session.Who(r)
	ck, _ := r.Cookie(SessionCookie)
	if !ok || u.ID == "" || ck == nil {
		WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in", "code": CodeUnauthorized})
		return
	}
	t := newHex(32)
	now := c.now()
	c.mu.Lock()
	for k, x := range c.tickets {
		if now.After(x.exp) {
			delete(c.tickets, k)
		}
	}
	n := 0
	for _, x := range c.tickets {
		if x.user == u.ID {
			n++
		}
	}
	if n >= 2*c.MaxPerUser { // a loop in an app must not fill the memory
		c.mu.Unlock()
		WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many tickets", "code": "rate_limited"})
		return
	}
	key := newHex(16)
	c.tickets[t] = pushTicket{user: u.ID, cookie: ck.Value, key: key, exp: now.Add(TicketTTL)}
	c.mu.Unlock()
	WriteJSON(w, http.StatusOK, map[string]any{"ticket": t, "key": key, "url": c.URL, "expires_in": int(TicketTTL / time.Second)})
}

// connect is the $connect integration. 200 = API Gateway opens the socket.
func (c *PushConfig) connect(w http.ResponseWriter, r *http.Request) {
	id := r.Header.Get("X-Connection-Id")
	refuse := func(status int, code string) {
		c.stats.count(func(s *pushStats) { s.refusedConnects++ }) // X62
		c.record(r, "push.connect", status, "", code, nil)
		http.Error(w, code, status)
	}
	switch {
	case !c.secretOK(r):
		refuse(http.StatusForbidden, "bad_secret")
		return
	case !connIDRe.MatchString(id):
		refuse(http.StatusBadRequest, "bad_connection_id")
		return
	case !c.Origins[r.Header.Get("Origin")]: // OWASP: cross-site WebSocket hijacking
		refuse(http.StatusForbidden, "bad_origin")
		return
	}
	now := c.now()
	tk := r.Header.Get("X-Push-Ticket")
	c.mu.Lock()
	t, ok := c.tickets[tk]
	delete(c.tickets, tk) // one time, also when it is too old
	c.mu.Unlock()
	if !ok || tk == "" || now.After(t.exp) {
		refuse(http.StatusUnauthorized, CodeUnauthorized)
		return
	}
	u := User{ID: t.user}
	var close []*pushConn
	c.mu.Lock()
	if _, taken := c.conns[id]; taken { // a connection id is never given to a second user
		c.mu.Unlock()
		refuse(http.StatusConflict, "connection_id_taken")
		return
	}
	c.seq++
	c.conns[id] = &pushConn{id: id, user: u.ID, cookie: t.cookie, key: t.key, at: now, checked: now, seen: now, seq: c.seq}
	var mine []*pushConn
	for _, p := range c.conns {
		if p.user == u.ID {
			mine = append(mine, p)
		}
	}
	sort.Slice(mine, func(a, b int) bool { return mine[a].seq < mine[b].seq })
	for len(mine) > c.MaxPerUser {
		close = append(close, mine[0])
		delete(c.conns, mine[0].id)
		mine = mine[1:]
	}
	c.mu.Unlock()
	for _, x := range close {
		go c.closeConn(x.id, "max_per_user", x.user)
	}
	c.stats.count(func(s *pushStats) { s.connects++ }) // X62
	c.record(r, "push.connect", http.StatusOK, u.ID, "", nil)
	w.WriteHeader(http.StatusOK)
}

func (c *PushConfig) disconnect(w http.ResponseWriter, r *http.Request) {
	if !c.secretOK(r) {
		http.Error(w, "bad_secret", http.StatusForbidden)
		return
	}
	id := r.Header.Get("X-Connection-Id")
	c.mu.Lock()
	p := c.conns[id]
	delete(c.conns, id)
	c.mu.Unlock()
	if p != nil {
		c.record(r, "push.disconnect", http.StatusOK, p.user, "", nil)
	}
	w.WriteHeader(http.StatusOK)
}

// ping is the $default route: the app's "ping" message.
func (c *PushConfig) ping(w http.ResponseWriter, r *http.Request) {
	if !c.secretOK(r) {
		http.Error(w, "bad_secret", http.StatusForbidden)
		return
	}
	id, user := r.Header.Get("X-Connection-Id"), ""
	if id == "" { // X57: no id mapped on $default: the connection of the ping's key
		var m struct {
			Key string `json:"key"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 512)).Decode(&m)
		c.mu.Lock()
		if m.Key != "" {
			for _, p := range c.conns {
				if subtle.ConstantTimeCompare([]byte(p.key), []byte(m.Key)) == 1 {
					id, user = p.id, p.user
					p.seen = c.now()
					break
				}
			}
		}
		c.mu.Unlock()
		c.stats.count(func(s *pushStats) {
			s.pings++
			if id == "" {
				s.unknownKeys++
			}
		})
		if id != "" {
			c.post(r.Context(), id, []byte(`{"kind":"pong"}`), "pong", user)
		} else if c.stats.unknownLine(c.now()) { // X62: an unknown key (a restart, or a wrong key): at most one line a minute
			c.record(r, "push.ping", http.StatusOK, "", "unknown_key", nil)
		}
		// an unknown key (the service restarted): no pong, so the app does not count the socket as working and
		// polls as before; it opens a new socket at the next reopen
		w.WriteHeader(http.StatusOK)
		return
	}
	if !connIDRe.MatchString(id) {
		http.Error(w, "bad_connection_id", http.StatusBadRequest)
		return
	}
	c.mu.Lock()
	p, known := c.conns[id]
	if known {
		p.seen = c.now()
	}
	c.mu.Unlock()
	c.stats.count(func(s *pushStats) { s.pings++ })
	if known {
		c.post(r.Context(), id, []byte(`{"kind":"pong"}`), "pong", p.user)
	} else {
		c.record(r, "push.ping", http.StatusOK, "", "unknown_connection", nil)
		go c.closeConn(id, "unknown_connection", "")
	}
	w.WriteHeader(http.StatusOK)
}

// PushMessage is what a service sends and what the app gets (without user_id).
type PushMessage struct {
	UserID string `json:"user_id,omitempty"`
	Kind   string `json:"kind"`
	ID     string `json:"id,omitempty"`
	Status string `json:"status,omitempty"`
}

var pushIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{0,64}$`)

func (c *PushConfig) notify(w http.ResponseWriter, r *http.Request) {
	var m PushMessage
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&m); err != nil || m.UserID == "" ||
		!PushKinds[m.Kind] || !pushIDRe.MatchString(m.ID) || !pushIDRe.MatchString(m.Status) {
		http.Error(w, "bad message", http.StatusBadRequest)
		return
	}
	t0 := time.Now()
	c.mu.Lock()
	var ids []string
	for _, p := range c.conns {
		if p.user == m.UserID {
			ids = append(ids, p.id)
		}
	}
	c.mu.Unlock()
	body, _ := json.Marshal(PushMessage{Kind: m.Kind, ID: m.ID, Status: m.Status})
	sent := 0
	for _, id := range ids {
		if c.post(r.Context(), id, body, "notify:"+m.Kind, m.UserID) {
			sent++
		}
	}
	// X62: one line for the whole message: how many sockets the user has, how many got it, the time in all
	c.record(r, "push.notify", http.StatusOK, m.UserID, map[bool]string{true: "", false: "not_sent"}[sent > 0 || len(ids) == 0],
		map[string]any{"kind": m.Kind, "connections": len(ids), "sent": sent, "latency_ms": time.Since(t0).Milliseconds()})
	WriteJSON(w, http.StatusOK, map[string]any{"connections": len(ids), "sent": sent})
}

// manage sends one management API call; status 0 = no answer. X62: it returns the time of the call and, when
// it failed, why ("credentials", "request", "network: ..."; a status that is not 2xx is its own reason).
func (c *PushConfig) manage(ctx context.Context, method, id string, body []byte) (int, time.Duration, string) {
	if c.Endpoint == "" || c.Creds == nil {
		return 0, 0, "off"
	}
	t0 := time.Now()
	cr, err := c.Creds.Creds(ctx)
	if err != nil {
		return 0, time.Since(t0), "credentials: " + clip(err.Error(), 120)
	}
	u := strings.TrimRight(c.Endpoint, "/") + "/%40connections/" + awsEscape(id, true) // the AWS guide: "@" as %40
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return 0, time.Since(t0), "request"
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	SignV4(req, body, cr, c.Region, "execute-api", c.now())
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var ue *url.Error // X62 review: the error text starts with the URL, which has the connection id
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return 0, time.Since(t0), "network: " + clip(err.Error(), 120)
	}
	defer resp.Body.Close()
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	why := ""
	if resp.StatusCode/100 != 2 { // AWS says why in the body (e.g. 403 "... is not authorized to perform: execute-api:ManageConnections")
		why = awsReason(msg)
	}
	return resp.StatusCode, time.Since(t0), why
}

// awsReason is the safe part of an AWS error body (X62 review): the JSON "message" (or the text), up to the first
// line end and before "The Canonical String" (a signature error repeats the signed request there: the path with
// the connection id, the session token), with no control characters, at most 200 bytes.
func awsReason(body []byte) string {
	t := string(body)
	var m struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &m) == nil && m.Message != "" {
		t = m.Message
	}
	if i := strings.Index(t, "The Canonical String"); i >= 0 {
		t = t[:i]
	}
	if i := strings.IndexAny(t, "\r\n"); i >= 0 {
		t = t[:i]
	}
	t = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, t)
	return clip(strings.TrimSpace(t), 200)
}

// awsCode is the events code of a management API answer.
func awsCode(status int) string {
	switch {
	case status/100 == 2:
		return ""
	case status == 0:
		return "no_answer"
	case status == http.StatusGone:
		return "gone"
	case status == http.StatusForbidden:
		return "forbidden"
	}
	return "aws_" + strconv.Itoa(status)
}

// connRef is a short, stable name of a connection id for the events and the log (the first 12 hex of its sha256).
func connRef(id string) string {
	h := sha256.Sum256([]byte(id))
	return hex.EncodeToString(h[:6])
}

// call makes one management API call and records it (X62): an events line for each call (route push.send or
// push.close: the status, the code, the latency, what it was for, the bytes), the counts and latency figures of
// healthz, and a log line for each failure.
func (c *PushConfig) call(ctx context.Context, method, id string, body []byte, what, user string) int {
	route := "push.send"
	if method == http.MethodDelete {
		route = "push.close"
	}
	st, d, why := c.manage(ctx, method, id, body)
	if why == "off" { // push is off: a close still says why it happened (as before X62)
		if method == http.MethodDelete {
			c.record(nil, route, 0, user, strings.TrimPrefix(what, "close:"), map[string]any{"conn": connRef(id), "push": "off"})
		}
		return 0
	}
	ms := d.Milliseconds()
	ok := st/100 == 2 || st == http.StatusGone // X62 review: 410 = the socket closed by itself; normal, not a failure
	code := awsCode(st)
	if ok && method == http.MethodDelete {
		code = strings.TrimPrefix(what, "close:") // the reason of the close (session_ended, max_age, no_ping, ...)
	}
	detail := map[string]any{"what": what, "conn": connRef(id), "latency_ms": ms, "bytes": len(body)}
	if st == http.StatusGone {
		detail["gone"] = true
	}
	var fail map[string]any
	if !ok {
		detail["why"] = why
		fail = map[string]any{"at": c.now().UTC().Format(time.RFC3339), "route": route, "status": st, "what": what, "why": why}
		log.Printf("push: %s %s conn %s -> %d (%s) in %d ms", route, what, connRef(id), st, why, ms)
	}
	c.stats.add(method, st, ms, fail)
	if what == "pong" && st/100 == 2 { // X62 review: a pong each minute for each socket: counted, a line only when it fails
		c.stats.count(func(s *pushStats) { s.pongs++ })
		return st
	}
	ev := Event{TS: c.now().UTC().Format(time.RFC3339Nano), Service: "push", Build: c.Build, Route: route,
		UserID: user, Status: st, Code: code, LatencyMS: ms, Detail: detail}
	c.Events.Record(ev)
	return st
}

// post sends a message; 410 Gone forgets the connection.
func (c *PushConfig) post(ctx context.Context, id string, body []byte, what, user string) bool {
	st := c.call(ctx, http.MethodPost, id, body, what, user)
	if st == http.StatusGone {
		c.mu.Lock()
		delete(c.conns, id)
		c.mu.Unlock()
	}
	return st/100 == 2
}

func (c *PushConfig) closeConn(id, why, user string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c.call(ctx, http.MethodDelete, id, nil, "close:"+why, user)
}

// Sweep asks again the session of each connection checked Recheck ago or
// earlier, and closes those whose session ended or that are older than
// MaxAge. It returns the number closed.
func (c *PushConfig) Sweep(ctx context.Context) int {
	c.init()
	now := c.now()
	c.mu.Lock()
	var due []*pushConn
	var stale []*pushConn
	for id, p := range c.conns {
		if now.Sub(p.seen) >= c.StaleAfter { // X57: no ping (a $disconnect may have no id): forget it and close it
			delete(c.conns, id)
			stale = append(stale, p)
			continue
		}
		if now.Sub(p.checked) >= c.Recheck || now.Sub(p.at) >= c.MaxAge {
			cp := *p
			due = append(due, &cp)
		}
	}
	c.mu.Unlock()
	for _, id := range stale {
		c.closeConn(id.id, "no_ping", id.user)
	}
	if len(stale) > 0 {
		c.record(nil, "push.stale", http.StatusOK, "", "no_ping", map[string]any{"forgotten": len(stale)})
	}
	byCookie := map[string]string{} // cookie -> user now ("" = ended)
	closed := len(stale)
	for _, p := range due {
		user, seen := byCookie[p.cookie]
		if !seen {
			r, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://push/", nil)
			r.AddCookie(&http.Cookie{Name: SessionCookie, Value: p.cookie})
			user = c.sessionUser(r, p.user)
			byCookie[p.cookie] = user
		}
		if user != p.user || now.Sub(p.at) >= c.MaxAge {
			c.mu.Lock()
			delete(c.conns, p.id)
			c.mu.Unlock()
			why := "session_ended"
			if now.Sub(p.at) >= c.MaxAge {
				why = "max_age"
			}
			c.closeConn(p.id, why, p.user)
			closed++
			continue
		}
		c.mu.Lock()
		if q := c.conns[p.id]; q != nil {
			q.checked = now
		}
		c.mu.Unlock()
	}
	return closed
}

// sessionUser is the user of the session now: "" when auth says it ended.
// When auth gives no clear answer (a restart, a timeout), the connection
// keeps its user (old): it is asked again at the next sweep.
func (c *PushConfig) sessionUser(r *http.Request, old string) string {
	if ec, ok := c.Session.(interface {
		WhoErr(*http.Request) (User, error)
	}); ok {
		u, err := ec.WhoErr(r)
		switch {
		case err == nil:
			return u.ID
		case errors.Is(err, ErrNoSession):
			return ""
		}
		return old
	}
	if u, ok := c.Session.Who(r); ok {
		return u.ID
	}
	return ""
}

// PushClient sends messages to the push service (the other services).
type PushClient struct {
	URL  string // http+unix://push.sock
	HTTP *http.Client
}

// Notify sends m in the background with a short timeout; nil c or URL = off.
// A failure is only logged: the app's polling still finds the change.
func (p *PushClient) Notify(m PushMessage) {
	if p == nil || p.URL == "" || m.UserID == "" {
		return
	}
	go func() {
		b, _ := json.Marshal(m)
		hc := p.HTTP
		if hc == nil {
			hc = &http.Client{Timeout: 3 * time.Second}
		}
		resp, err := hc.Post(strings.TrimRight(p.URL, "/")+"/v1/push/notify", "application/json", bytes.NewReader(b))
		if err != nil {
			log.Printf("push notify %s: %v", m.Kind, err)
			return
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			log.Printf("push notify %s: HTTP %d", m.Kind, resp.StatusCode)
		}
	}()
}

// String hides the cookie.
func (p pushConn) String() string { return fmt.Sprintf("conn %s user %s", p.id, p.user) }
