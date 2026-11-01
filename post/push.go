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
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
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
	Events     *Events
	Build      string
	now        func() time.Time

	mu      sync.Mutex
	conns   map[string]*pushConn // by connection id
	tickets map[string]pushTicket
	seq     int64
}

type pushTicket struct {
	user, cookie string
	exp          time.Time
}

// TicketTTL is how long a ticket is good for.
const TicketTTL = 60 * time.Second

type pushConn struct {
	id, user, cookie string
	at, checked      time.Time
	seq              int64 // the order of the connects
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
			"connections": n, "endpoint": c.Endpoint != ""})
	}
	pub := http.NewServeMux()
	pub.HandleFunc("GET /healthz", health)
	pub.HandleFunc("POST /api/push/ticket", c.ticket)
	pub.HandleFunc("POST /api/push/connect", c.connect)
	pub.HandleFunc("POST /api/push/disconnect", c.disconnect)
	pub.HandleFunc("POST /api/push/ping", c.ping)
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
	c.tickets[t] = pushTicket{user: u.ID, cookie: ck.Value, exp: now.Add(TicketTTL)}
	c.mu.Unlock()
	WriteJSON(w, http.StatusOK, map[string]any{"ticket": t, "url": c.URL, "expires_in": int(TicketTTL / time.Second)})
}

// connect is the $connect integration. 200 = API Gateway opens the socket.
func (c *PushConfig) connect(w http.ResponseWriter, r *http.Request) {
	id := r.Header.Get("X-Connection-Id")
	refuse := func(status int, code string) {
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
	var close []string
	c.mu.Lock()
	if _, taken := c.conns[id]; taken { // a connection id is never given to a second user
		c.mu.Unlock()
		refuse(http.StatusConflict, "connection_id_taken")
		return
	}
	c.seq++
	c.conns[id] = &pushConn{id: id, user: u.ID, cookie: t.cookie, at: now, checked: now, seq: c.seq}
	var mine []*pushConn
	for _, p := range c.conns {
		if p.user == u.ID {
			mine = append(mine, p)
		}
	}
	sort.Slice(mine, func(a, b int) bool { return mine[a].seq < mine[b].seq })
	for len(mine) > c.MaxPerUser {
		close = append(close, mine[0].id)
		delete(c.conns, mine[0].id)
		mine = mine[1:]
	}
	c.mu.Unlock()
	for _, x := range close {
		go c.closeConn(x)
	}
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
	id := r.Header.Get("X-Connection-Id")
	if !connIDRe.MatchString(id) {
		http.Error(w, "bad_connection_id", http.StatusBadRequest)
		return
	}
	c.mu.Lock()
	_, known := c.conns[id]
	c.mu.Unlock()
	if known {
		c.post(r.Context(), id, []byte(`{"kind":"pong"}`))
	} else {
		go c.closeConn(id)
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
		if c.post(r.Context(), id, body) {
			sent++
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"connections": len(ids), "sent": sent})
}

// manage sends one management API call; status 0 = no answer.
func (c *PushConfig) manage(ctx context.Context, method, id string, body []byte) int {
	if c.Endpoint == "" || c.Creds == nil {
		return 0
	}
	cr, err := c.Creds.Creds(ctx)
	if err != nil {
		log.Printf("push: credentials: %v", err)
		return 0
	}
	u := strings.TrimRight(c.Endpoint, "/") + "/%40connections/" + awsEscape(id, true) // the AWS guide: "@" as %40
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return 0
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	SignV4(req, body, cr, c.Region, "execute-api", c.now())
	resp, err := c.HTTP.Do(req)
	if err != nil {
		log.Printf("push: %s @connections: %v", method, err)
		return 0
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode
}

// post sends a message; 410 Gone forgets the connection.
func (c *PushConfig) post(ctx context.Context, id string, body []byte) bool {
	st := c.manage(ctx, http.MethodPost, id, body)
	if st == http.StatusGone {
		c.mu.Lock()
		delete(c.conns, id)
		c.mu.Unlock()
	}
	return st/100 == 2
}

func (c *PushConfig) closeConn(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c.manage(ctx, http.MethodDelete, id, nil)
}

// Sweep asks again the session of each connection checked Recheck ago or
// earlier, and closes those whose session ended or that are older than
// MaxAge. It returns the number closed.
func (c *PushConfig) Sweep(ctx context.Context) int {
	c.init()
	now := c.now()
	c.mu.Lock()
	var due []*pushConn
	for _, p := range c.conns {
		if now.Sub(p.checked) >= c.Recheck || now.Sub(p.at) >= c.MaxAge {
			cp := *p
			due = append(due, &cp)
		}
	}
	c.mu.Unlock()
	byCookie := map[string]string{} // cookie -> user now ("" = ended)
	closed := 0
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
			c.closeConn(p.id)
			c.record(nil, "push.close", http.StatusOK, p.user, "session_ended", nil)
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
