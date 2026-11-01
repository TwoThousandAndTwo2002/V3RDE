package auth

// Password reset by email link, written new for V3RDE (X49o, NEW_WORK.md 83; the contract is
// docs/v3rde_reset_password_brief_2026-10-29.md, the app side is UI 9). T9 2026-10-31: "reset password does not
// work" (the app had the screens, this service had no routes) and the sender is Google Workspace SMTP.
//
//	POST /api/auth/reset          {email}            -> 202 {} (also for an unknown email) | 400 | 429 | 503 off
//	POST /api/auth/reset/confirm  {token, password}  -> 200 user + Set-Cookie | 400 "reset link expired" | 400 short
//
// The token: 32 random bytes, unpadded base64url (43 characters). The data file keeps only its SHA-256, the user
// and the end time (30 minutes); a new request of the user replaces the old token. The link carries the token
// after "#" (<base>/reset#token=...), so the browser does not send it to the web server, its logs or a Referer.
// A used token is gone; a reset ends every other session of the user. The mail goes out from one worker, after
// the answer (the lookup and the token too), so the answer time does not depend on whether the account exists
// or how slow the relay is; at most DailyMails mails go out in one day. Logs
// have the user id only: never the email, the token or the password.

import (
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"regexp"
	"strings"
	"time"
)

// ResetTTL is how long a reset link works.
const ResetTTL = 30 * time.Minute

// ErrResetExpired is the one answer for a token that is unknown, used or too old (the app shows "This link has
// expired or was used." for this text).
var ErrResetExpired = errors.New("reset link expired")

// ErrResetOff is the answer when no mail sender is set up.
var ErrResetOff = errors.New("password reset is not set up")

type resetRec struct {
	TokenHash string `json:"token_hash"` // sha256 hex of the token
	UserID    string `json:"user_id"`
	ExpiresAt string `json:"expires_at"`
}

var tokenForm = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func resetOver(r resetRec, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, r.ExpiresAt)
	return err != nil || !now.Before(t)
}

// NewReset makes the reset token of the account of email. ok is false (and no error) when no account has the
// email. A new token replaces every earlier token of that user; tokens that are over are removed.
func (s *Store) NewReset(email string) (token string, u User, ok bool, err error) {
	email = NormalizeEmail(email)
	if !emailLoose.MatchString(email) { // X49w review: an older account gets its link too
		return "", User{}, false, ErrBadEmail
	}
	var b [32]byte
	_, _ = rand.Read(b[:]) // never fails since Go 1.24
	token = base64.RawURLEncoding.EncodeToString(b[:])
	key := hashToken(token)
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	id, found := s.data.Emails[email]
	rec, known := s.data.Users[id]
	if !found || !known {
		return "", User{}, false, nil
	}
	gone := map[string]resetRec{}
	for k, r := range s.data.Resets {
		if r.UserID == id || resetOver(r, now) {
			gone[k] = r
			delete(s.data.Resets, k)
		}
	}
	s.data.Resets[key] = resetRec{TokenHash: key, UserID: id, ExpiresAt: now.Add(ResetTTL).Format(time.RFC3339)}
	if err := s.saveLocked(); err != nil { // the file did not change: the memory does not either
		delete(s.data.Resets, key)
		for k, r := range gone {
			s.data.Resets[k] = r
		}
		return "", User{}, false, err
	}
	return token, rec.public(), true, nil
}

// ConfirmReset sets the new password of the token's user, removes the token and ends every session of that
// user. The password is hashed outside the lock (it takes the PBKDF2 time); the token is checked again after.
func (s *Store) ConfirmReset(token, password string) (User, error) {
	if !tokenForm.MatchString(token) {
		return User{}, ErrResetExpired
	}
	key := hashToken(token)
	check := func(now time.Time) (resetRec, bool) {
		r, ok := s.data.Resets[key]
		if !ok || resetOver(r, now) {
			return resetRec{}, false
		}
		_, known := s.data.Users[r.UserID]
		return r, known
	}
	s.mu.Lock()
	_, ok := check(s.now().UTC())
	s.mu.Unlock()
	if !ok {
		return User{}, ErrResetExpired
	}
	if len(password) < MinPassword {
		return User{}, ErrShortPass
	}
	hash, err := hashPassword(password, pbkdfIter)
	if err != nil {
		return User{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := check(s.now().UTC())
	if !ok { // used or over while the hash was made
		return User{}, ErrResetExpired
	}
	before := s.data.Users[r.UserID]
	u := before
	u.PwHash = hash
	s.data.Users[r.UserID] = u
	delete(s.data.Resets, key)
	gone := map[string]sessionRec{}
	for k, sess := range s.data.Sessions {
		if sess.UserID == r.UserID {
			gone[k] = sess
			delete(s.data.Sessions, k)
		}
	}
	if err := s.saveLocked(); err != nil { // the file did not change: the memory does not either
		s.data.Users[r.UserID] = before
		s.data.Resets[key] = r
		for k, sess := range gone {
			s.data.Sessions[k] = sess
		}
		return User{}, err
	}
	return u.public(), nil
}

// ---- the mail ---------------------------------------------------------------

// ResetSubject and resetBody are the words of the email (the U9 brief, ASD-STE100).
const ResetSubject = "Reset your V3RDE password"

func resetBody(link string) string {
	return "Open this link to set a new password:\r\n\r\n" + link + "\r\n\r\n" +
		"The link works for 30 minutes, one time. If you did not ask for this, do nothing: your password stays the same.\r\n"
}

// ResetLink is the link of a token: the token after "#".
func ResetLink(base, token string) string {
	return strings.TrimRight(base, "/") + "/reset#token=" + token
}

var crlf = regexp.MustCompile(`[\r\n]+`)

// resetMessage is the RFC 5322 message: header values lose CR and LF (no header injection); Date and Message-ID
// are set here (https://www.rfc-editor.org/rfc/rfc5322#section-3.6).
func resetMessage(from, to, link string, now time.Time) []byte {
	h := func(s string) string { return strings.TrimSpace(crlf.ReplaceAllString(s, " ")) }
	domain := "localhost"
	if i := strings.LastIndex(from, "@"); i >= 0 && i+1 < len(from) {
		domain = h(from[i+1:])
	}
	var id [12]byte
	_, _ = rand.Read(id[:])
	var b strings.Builder
	b.WriteString("From: V3RDE <" + h(from) + ">\r\n")
	b.WriteString("To: " + h(to) + "\r\n")
	b.WriteString("Subject: " + ResetSubject + "\r\n")
	b.WriteString("Date: " + now.UTC().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("Message-ID: <" + hex.EncodeToString(id[:]) + "@" + domain + ">\r\n")
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(resetBody(link))
	return []byte(b.String())
}

// SMTP sends one message through a relay with PLAIN auth (Google Workspace: smtp.gmail.com with an app password).
// ImplicitTLS (port 465, "SSL"): TLS from the first byte; else STARTTLS (port 587). X49o, T9 2026-10-31: the
// network of the setup Mac gives no answer on 587, so the deploy uses 465. smtp.SendMail has no deadline, so the
// exchange has one deadline on the connection.
type SMTP struct {
	Addr        string // host:port
	User        string // the mailbox; also the From address
	Pass        string
	Timeout     time.Duration
	ImplicitTLS bool
	roots       *x509.CertPool // tests only: the fake relay's certificate (nil = the system roots)
}

// ImplicitTLSPort is the port of SMTP over TLS from the first byte (RFC 8314 "submissions").
const ImplicitTLSPort = "465"

// Send sends msg to one address. TLS 1.2 or later is required before the password goes out.
func (m SMTP) Send(to string, msg []byte) error {
	t := m.Timeout
	if t <= 0 {
		t = 20 * time.Second
	}
	host, _, err := net.SplitHostPort(m.Addr)
	if err != nil {
		return err
	}
	cfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, RootCAs: m.roots}
	dl := time.Now().Add(t) // one deadline: the dial, the TLS handshake and the whole exchange
	d := &net.Dialer{Deadline: dl}
	var conn net.Conn
	if m.ImplicitTLS {
		conn, err = tls.DialWithDialer(d, "tcp", m.Addr, cfg)
	} else {
		conn, err = d.Dial("tcp", m.Addr)
	}
	if err != nil {
		return err
	}
	if err := conn.SetDeadline(dl); err != nil {
		conn.Close()
		return err
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if !m.ImplicitTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("smtp: the relay does not offer STARTTLS")
		}
		if err := c.StartTLS(cfg); err != nil {
			return err
		}
	}
	if err := c.Auth(smtp.PlainAuth("", m.User, m.Pass, host)); err != nil {
		return err
	}
	if err := c.Mail(m.User); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	wc, err := c.Data() // the DataWriter dot-stuffs the lines
	if err != nil {
		return err
	}
	if _, err := wc.Write(msg); err != nil {
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// DailyMails is the most reset mails the service sends in one UTC day, for all users (X49o review: the Workspace
// mailbox may send about 2,000 a day; free sign-ups must not use it up or make it bounce).
const DailyMails = 300

// Mailer does the reset work after the answer, in one worker: it looks up the account, makes the token and sends
// the mail. So the answer time is the same for every email (no lookup, no file write, no relay before the 202).
// Queue never blocks: a full queue drops the request (logged without the email); the user can ask again.
type Mailer struct {
	From     string // the sender address
	LinkBase string // the public web address of the app, https://...
	Store    *Store
	send     func(to string, msg []byte) error
	ch       chan string // normalized emails
	now      func() time.Time
	day      string // the UTC day of sent
	sent     int
	idle     chan struct{} // tests: a signal after each request is done
}

// NewMailer starts the worker. send is SMTP.Send (tests use a fake).
func NewMailer(st *Store, from, linkBase string, send func(to string, msg []byte) error) *Mailer {
	m := &Mailer{From: from, LinkBase: linkBase, Store: st, send: send, ch: make(chan string, 64), now: time.Now}
	go m.loop()
	return m
}

func (m *Mailer) queue(email string) {
	select {
	case m.ch <- email:
	default:
		log.Printf("reset request dropped (queue full)")
	}
}

func (m *Mailer) loop() {
	for email := range m.ch {
		m.one(email)
		if m.idle != nil {
			m.idle <- struct{}{}
		}
	}
}

func (m *Mailer) one(email string) {
	if d := m.now().UTC().Format("2006-01-02"); d != m.day {
		m.day, m.sent = d, 0
	}
	if m.sent >= DailyMails {
		log.Printf("reset request dropped (daily mail cap %d)", DailyMails)
		return
	}
	token, u, ok, err := m.Store.NewReset(email)
	switch {
	case err != nil:
		log.Printf("reset: %v", err)
		return
	case !ok:
		return // no account: no mail, the same answer was given
	}
	log.Printf("reset_requested user=%s", u.ID)
	m.sent++
	msg := resetMessage(m.From, u.Email, ResetLink(m.LinkBase, token), m.now())
	if err := m.send(u.Email, msg); err != nil {
		log.Printf("reset mail failed user=%s: %s", u.ID, scrub(err.Error(), u.Email, token))
		return
	}
	log.Printf("reset mail sent user=%s", u.ID)
}

// scrub removes the address and the token from an error text before it is logged (a relay may echo them).
func scrub(s, to, token string) string {
	for _, x := range []string{to, token} {
		if x != "" {
			s = strings.ReplaceAll(s, x, "***")
		}
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// ---- HTTP -------------------------------------------------------------------

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		fail(w, http.StatusUnsupportedMediaType, "send application/json")
		return false
	}
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "bad JSON body")
		return false
	}
	return true
}

func (c Config) resetRequest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	email := NormalizeEmail(in.Email)
	if !emailLoose.MatchString(email) { // X49w review: an older account gets its link too
		fail(w, http.StatusBadRequest, ErrBadEmail.Error())
		return
	}
	if c.Mailer == nil {
		fail(w, http.StatusServiceUnavailable, ErrResetOff.Error())
		return
	}
	// its own limit per address (the X49o review: the sign-in limit alone allows 28,800 a day), and per email
	// (the key is a hash: the limiter keeps no address), so nobody can fill one inbox
	if c.ResetIPLimiter != nil && !c.ResetIPLimiter.Allow(clientIP(r)) {
		fail(w, http.StatusTooManyRequests, "rate limited")
		return
	}
	if c.ResetLimiter != nil && !c.ResetLimiter.Allow(hashToken("reset:"+email)) {
		fail(w, http.StatusTooManyRequests, "rate limited")
		return
	}
	c.Mailer.queue(email) // the lookup, the token and the mail happen after the answer
	writeJSON(w, http.StatusAccepted, struct{}{})
}

func (c Config) resetConfirm(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	u, err := c.Store.ConfirmReset(in.Token, in.Password)
	switch {
	case errors.Is(err, ErrResetExpired), errors.Is(err, ErrShortPass):
		fail(w, http.StatusBadRequest, err.Error())
	case err != nil:
		log.Printf("reset confirm: %v", err)
		fail(w, http.StatusInternalServerError, "could not set the password")
	default:
		c.startSession(w, u, http.StatusOK, "reset_done")
	}
}

// resetCount is for the tests.
func (s *Store) resetCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.data.Resets)
}
