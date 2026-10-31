package auth

// Tests of the password reset (X49o, reset.go).

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"log"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

type sentMail struct {
	to  string
	msg string
}

type fakeRelay struct {
	mu   sync.Mutex
	got  []sentMail
	wake chan struct{}
	idle chan struct{} // one signal per request the worker finished
}

// done waits until the worker finished one request.
func (f *fakeRelay) done(t *testing.T) {
	t.Helper()
	select {
	case <-f.idle:
	case <-time.After(2 * time.Second):
		t.Fatal("the worker did not finish")
	}
}

func (f *fakeRelay) send(to string, msg []byte) error {
	f.mu.Lock()
	f.got = append(f.got, sentMail{to, string(msg)})
	f.mu.Unlock()
	f.wake <- struct{}{}
	return nil
}

// wait is the next mail, or a failure after 2 s.
func (f *fakeRelay) wait(t *testing.T) sentMail {
	t.Helper()
	select {
	case <-f.wake:
	case <-time.After(2 * time.Second):
		t.Fatal("no mail")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.got[len(f.got)-1]
}

func (f *fakeRelay) none(t *testing.T) {
	t.Helper()
	select {
	case <-f.wake:
		t.Fatal("a mail went out")
	case <-time.After(150 * time.Millisecond):
	}
}

var linkToken = regexp.MustCompile(`https://app\.example/reset#token=([A-Za-z0-9_-]{43})\r\n`)

// resetRig is the rig with a reset mailer on a fake relay; perEmail is the reset burst per email (0 = no limit).
func resetRig(t *testing.T, perEmail int) (*rig, *fakeRelay) {
	t.Helper()
	g := newRig(t, 0)
	f := &fakeRelay{wake: make(chan struct{}, 16)}
	// made here (not NewMailer) so the test clock and the idle signal are set before the worker starts
	m := &Mailer{From: "no-reply@app.example", LinkBase: "https://app.example/", Store: g.st, send: f.send,
		ch: make(chan string, 64), now: func() time.Time { return *g.now }, idle: make(chan struct{}, 64)}
	go m.loop()
	f.idle = m.idle
	c := Config{Store: g.st, CookieSecure: true, Mailer: m}
	if perEmail > 0 {
		c.ResetLimiter = NewLimiter(perEmail, 15*time.Minute)
	}
	g.h = New(c)
	return g, f
}

func tokenOf(t *testing.T, m sentMail) string {
	t.Helper()
	k := linkToken.FindStringSubmatch(m.msg)
	if k == nil {
		t.Fatalf("no link in the mail:\n%s", m.msg)
	}
	return k[1]
}

func TestResetFlowSetsThePasswordAndEndsOtherSessions(t *testing.T) {
	g, f := resetRig(t, 0)
	w := g.send("POST", "/api/auth/signup", `{"email":"giver@example.org","password":"old-password-1"}`, "")
	if w.Code != 201 {
		t.Fatalf("signup %d", w.Code)
	}
	old := theCookie(t, w).Value
	w = g.send("POST", "/api/auth/reset", `{"email":" Giver@Example.org "}`, "")
	if w.Code != 202 || strings.TrimSpace(w.Body.String()) != "{}" {
		t.Fatalf("reset %d %s", w.Code, w.Body)
	}
	m := f.wait(t)
	if m.to != "giver@example.org" {
		t.Fatalf("to %q", m.to)
	}
	tok := tokenOf(t, m)
	if w := g.send("POST", "/api/auth/reset/confirm", `{"token":"`+tok+`","password":"short"}`, ""); w.Code != 400 || bodyOf(w)["error"] != ErrShortPass.Error() {
		t.Fatalf("short %d %s", w.Code, w.Body)
	}
	w = g.send("POST", "/api/auth/reset/confirm", `{"token":"`+tok+`","password":"new-password-2"}`, "")
	if w.Code != 200 || bodyOf(w)["email"] != "giver@example.org" {
		t.Fatalf("confirm %d %s", w.Code, w.Body)
	}
	fresh := theCookie(t, w).Value
	if w := g.send("GET", "/api/auth/me", "", fresh); w.Code != 200 {
		t.Fatalf("the new session %d", w.Code)
	}
	if w := g.send("GET", "/api/auth/me", "", old); w.Code != 401 {
		t.Fatalf("the old session still works: %d", w.Code)
	}
	if w := g.send("POST", "/api/auth/login", `{"email":"giver@example.org","password":"old-password-1"}`, ""); w.Code != 401 {
		t.Fatalf("the old password still works: %d", w.Code)
	}
	if w := g.send("POST", "/api/auth/login", `{"email":"giver@example.org","password":"new-password-2"}`, ""); w.Code != 200 {
		t.Fatalf("the new password does not work: %d", w.Code)
	}
	if w := g.send("POST", "/api/auth/reset/confirm", `{"token":"`+tok+`","password":"third-password"}`, ""); w.Code != 400 || bodyOf(w)["error"] != "reset link expired" {
		t.Fatalf("a used link works again: %d %s", w.Code, w.Body)
	}
	if n := g.st.resetCount(); n != 0 {
		t.Fatalf("%d tokens left", n)
	}
	// the file keeps only the hash of the token
	if b, _ := os.ReadFile(g.path); bytes.Contains(b, []byte(tok)) {
		t.Fatal("the token is in the data file")
	}
}

func TestResetUnknownEmailGetsTheSameAnswerAndNoMail(t *testing.T) {
	g, f := resetRig(t, 0)
	w := g.send("POST", "/api/auth/reset", `{"email":"nobody@example.org"}`, "")
	if w.Code != 202 || strings.TrimSpace(w.Body.String()) != "{}" {
		t.Fatalf("unknown email %d %s", w.Code, w.Body)
	}
	f.done(t)
	f.none(t)
	if g.st.resetCount() != 0 {
		t.Fatal("a token was made")
	}
}

func TestResetLinkEndsAfter30MinutesAndANewOneReplacesIt(t *testing.T) {
	g, f := resetRig(t, 0)
	g.send("POST", "/api/auth/signup", `{"email":"giver@example.org","password":"old-password-1"}`, "")
	g.send("POST", "/api/auth/reset", `{"email":"giver@example.org"}`, "")
	first := tokenOf(t, f.wait(t))
	g.send("POST", "/api/auth/reset", `{"email":"giver@example.org"}`, "")
	second := tokenOf(t, f.wait(t))
	if first == second || g.st.resetCount() != 1 {
		t.Fatalf("tokens %d", g.st.resetCount())
	}
	if w := g.send("POST", "/api/auth/reset/confirm", `{"token":"`+first+`","password":"new-password-2"}`, ""); w.Code != 400 || bodyOf(w)["error"] != "reset link expired" {
		t.Fatalf("the replaced link works: %d", w.Code)
	}
	*g.now = g.now.Add(ResetTTL)
	if w := g.send("POST", "/api/auth/reset/confirm", `{"token":"`+second+`","password":"new-password-2"}`, ""); w.Code != 400 || bodyOf(w)["error"] != "reset link expired" {
		t.Fatalf("the link works after 30 minutes: %d", w.Code)
	}
	for _, bad := range []string{"", "abc", strings.Repeat("A", 43) + "=", strings.Repeat("A", 43)} {
		if w := g.send("POST", "/api/auth/reset/confirm", `{"token":"`+bad+`","password":"new-password-2"}`, ""); w.Code != 400 || bodyOf(w)["error"] != "reset link expired" {
			t.Fatalf("token %q: %d %s", bad, w.Code, w.Body)
		}
	}
}

func TestResetBadEmailLimitAndOff(t *testing.T) {
	g, f := resetRig(t, 2)
	g.send("POST", "/api/auth/signup", `{"email":"giver@example.org","password":"old-password-1"}`, "")
	if w := g.send("POST", "/api/auth/reset", `{"email":"not an email"}`, ""); w.Code != 400 || bodyOf(w)["error"] != "email is not valid" {
		t.Fatalf("bad email %d %s", w.Code, w.Body)
	}
	if w := g.send("POST", "/api/auth/reset", `email=x`, ""); w.Code != 400 && w.Code != 415 {
		t.Fatalf("not JSON %d", w.Code)
	}
	for i := 0; i < 2; i++ {
		if w := g.send("POST", "/api/auth/reset", `{"email":"giver@example.org"}`, ""); w.Code != 202 {
			t.Fatalf("try %d: %d", i, w.Code)
		}
		f.wait(t)
	}
	if w := g.send("POST", "/api/auth/reset", `{"email":"GIVER@example.org"}`, ""); w.Code != 429 {
		t.Fatalf("third mail to one email %d", w.Code)
	}
	if w := g.send("POST", "/api/auth/reset", `{"email":"other@example.org"}`, ""); w.Code != 202 {
		t.Fatalf("another email is limited too: %d", w.Code)
	}
	off := newRig(t, 0) // no mailer
	if w := off.send("POST", "/api/auth/reset", `{"email":"giver@example.org"}`, ""); w.Code != 503 || bodyOf(w)["error"] != ErrResetOff.Error() {
		t.Fatalf("off %d %s", w.Code, w.Body)
	}
}

func TestResetMessageAndLogsHaveNoLeak(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)
	g, f := resetRig(t, 0)
	g.send("POST", "/api/auth/signup", `{"email":"giver@example.org","password":"old-password-1"}`, "")
	g.send("POST", "/api/auth/reset", `{"email":"giver@example.org"}`, "")
	m := f.wait(t)
	tok := tokenOf(t, m)
	head, body, ok := strings.Cut(m.msg, "\r\n\r\n")
	if !ok {
		t.Fatal("no header end")
	}
	for _, want := range []string{"From: V3RDE <no-reply@app.example>", "To: giver@example.org", "Subject: Reset your V3RDE password",
		"Date: Wed, 28 Oct 2026 18:00:00 +0000", "Message-ID: <", "@app.example>", "Content-Type: text/plain; charset=UTF-8"} {
		if !strings.Contains(head, want) {
			t.Fatalf("header %q missing:\n%s", want, head)
		}
	}
	if !strings.HasPrefix(body, "Open this link to set a new password:") || !strings.Contains(body, "works for 30 minutes, one time") {
		t.Fatalf("body:\n%s", body)
	}
	if strings.Contains(m.msg, "\n") && strings.Count(m.msg, "\n") != strings.Count(m.msg, "\r\n") {
		t.Fatal("a bare LF in the message")
	}
	g.send("POST", "/api/auth/reset/confirm", `{"token":"`+tok+`","password":"new-password-2"}`, "")
	time.Sleep(50 * time.Millisecond)
	l := logs.String()
	for _, leak := range []string{"giver@example.org", tok, "new-password-2"} {
		if strings.Contains(l, leak) {
			t.Fatalf("the log has %q:\n%s", leak, l)
		}
	}
	if !strings.Contains(l, "reset_requested user=u_") || !strings.Contains(l, "reset_done ok user=u_") {
		t.Fatalf("log:\n%s", l)
	}
	// a header value cannot add a header
	if msg := string(resetMessage("a@b.example\r\nBcc: x@evil.example", "c@d.example", "https://x/reset#token=t", *g.now)); strings.Contains(msg, "\r\nBcc:") {
		t.Fatalf("header injection:\n%s", msg)
	}
}

// TestSMTPRefusesARelayWithoutSTARTTLS: the password never goes out on a plain connection.
func TestSMTPRefusesARelayWithoutSTARTTLS(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	sawAuth := make(chan bool, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		c.Write([]byte("220 fake ESMTP\r\n"))
		auth := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				break
			}
			switch {
			case strings.HasPrefix(line, "EHLO"):
				c.Write([]byte("250-fake\r\n250 AUTH PLAIN\r\n"))
			case strings.HasPrefix(line, "AUTH"):
				auth = true
				c.Write([]byte("235 ok\r\n"))
			case strings.HasPrefix(line, "QUIT"):
				c.Write([]byte("221 bye\r\n"))
			default:
				c.Write([]byte("250 ok\r\n"))
			}
		}
		sawAuth <- auth
	}()
	err = SMTP{Addr: ln.Addr().String(), User: "u@example.org", Pass: "secret", Timeout: 2 * time.Second}.Send("x@example.org", []byte("m\r\n"))
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("err %v", err)
	}
	if <-sawAuth {
		t.Fatal("the password went out without TLS")
	}
}

// TestSMTPImplicitTLS (port 465): TLS from the first byte, then PLAIN auth and the message; a relay that does not
// speak TLS gets no password.
func TestSMTPImplicitTLS(t *testing.T) {
	hs := httptest.NewUnstartedServer(nil) // only for its test certificate (127.0.0.1)
	hs.StartTLS()
	cert, roots := hs.TLS.Certificates, x509.NewCertPool()
	roots.AddCert(hs.Certificate())
	hs.Close()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: cert})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		c.Write([]byte("220 fake ESMTP\r\n"))
		var seen strings.Builder
		data := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				break
			}
			if data {
				if line == ".\r\n" {
					data = false
					c.Write([]byte("250 queued\r\n"))
				} else {
					seen.WriteString(line)
				}
				continue
			}
			seen.WriteString(line)
			switch {
			case strings.HasPrefix(line, "EHLO"):
				c.Write([]byte("250-fake\r\n250 AUTH PLAIN\r\n"))
			case strings.HasPrefix(line, "AUTH"):
				c.Write([]byte("235 ok\r\n"))
			case strings.HasPrefix(line, "DATA"):
				data = true
				c.Write([]byte("354 go\r\n"))
			case strings.HasPrefix(line, "QUIT"):
				c.Write([]byte("221 bye\r\n"))
				got <- seen.String()
				return
			default:
				c.Write([]byte("250 ok\r\n"))
			}
		}
		got <- seen.String()
	}()
	m := SMTP{Addr: ln.Addr().String(), User: "u@example.org", Pass: "secret", Timeout: 3 * time.Second, ImplicitTLS: true, roots: roots}
	if err := m.Send("x@example.org", []byte("Subject: s\r\n\r\nhello\r\n")); err != nil {
		t.Fatal(err)
	}
	l := <-got
	for _, want := range []string{"AUTH PLAIN", "MAIL FROM:<u@example.org>", "RCPT TO:<x@example.org>", "hello"} {
		if !strings.Contains(l, want) {
			t.Fatalf("the relay did not see %q:\n%s", want, l)
		}
	}
	// a plain relay on the TLS port: the TLS handshake fails, nothing is sent
	pl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pl.Close()
	saw := make(chan string, 1)
	go func() {
		c, err := pl.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.Write([]byte("220 fake ESMTP\r\n"))
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		b := make([]byte, 4096)
		n, _ := c.Read(b)
		saw <- string(b[:n])
	}()
	m.Addr = pl.Addr().String()
	if err := m.Send("x@example.org", []byte("m\r\n")); err == nil {
		t.Fatal("a plain relay was accepted on the TLS port")
	}
	if b := <-saw; strings.Contains(b, "AUTH") || strings.Contains(b, "secret") {
		t.Fatalf("the plain relay got %q", b)
	}
}

func TestScrubRemovesTheAddressAndTheToken(t *testing.T) {
	got := scrub("550 5.1.1 <giver@example.org> no such user; token abcTOKEN", "giver@example.org", "abcTOKEN")
	if strings.Contains(got, "giver@example.org") || strings.Contains(got, "abcTOKEN") {
		t.Fatal(got)
	}
}

func TestResetHasItsOwnAddressLimitAndADailyCap(t *testing.T) {
	g, f := resetRig(t, 0)
	g.send("POST", "/api/auth/signup", `{"email":"giver@example.org","password":"old-password-1"}`, "")
	// the address limit: the third request of one address in an hour is refused, for any email
	lim := New(Config{Store: g.st, CookieSecure: true, Mailer: rigMailer(g, f), ResetIPLimiter: NewLimiter(2, time.Hour)})
	g.h = lim
	for i, e := range []string{"a@example.org", "b@example.org"} {
		if w := g.send("POST", "/api/auth/reset", `{"email":"`+e+`"}`, ""); w.Code != 202 {
			t.Fatalf("try %d: %d", i, w.Code)
		}
		f.done(t)
	}
	if w := g.send("POST", "/api/auth/reset", `{"email":"giver@example.org"}`, ""); w.Code != 429 {
		t.Fatalf("third from one address %d", w.Code)
	}
	// the daily cap: no mail once the cap is reached; the next UTC day sends again
	m := rigMailer(g, f)
	g.h = New(Config{Store: g.st, CookieSecure: true, Mailer: m})
	m.ch <- "giver@example.org"
	f.wait(t)
	f.done(t)
	m.sent = DailyMails // the worker is idle: no race
	m.ch <- "giver@example.org"
	f.done(t)
	f.none(t)
	*g.now = g.now.Add(24 * time.Hour)
	m.ch <- "giver@example.org"
	f.wait(t)
	f.done(t)
}

// rigMailer is one more worker on the rig's store and relay.
func rigMailer(g *rig, f *fakeRelay) *Mailer {
	m := &Mailer{From: "no-reply@app.example", LinkBase: "https://app.example/", Store: g.st, send: f.send,
		ch: make(chan string, 64), now: func() time.Time { return *g.now }, idle: f.idle}
	go m.loop()
	return m
}

func TestResetRollsBackWhenTheFileCannotBeWritten(t *testing.T) {
	g, f := resetRig(t, 0)
	g.send("POST", "/api/auth/signup", `{"email":"giver@example.org","password":"old-password-1"}`, "")
	g.send("POST", "/api/auth/reset", `{"email":"giver@example.org"}`, "")
	first := tokenOf(t, f.wait(t))
	f.done(t)
	// the rename onto a folder that is not empty fails, also for root
	real := g.st.path
	g.st.path = filepath.Dir(real)
	_, _, _, err := g.st.NewReset("giver@example.org")
	g.st.path = real
	if err == nil {
		t.Fatal("the save did not fail")
	}
	if w := g.send("POST", "/api/auth/reset/confirm", `{"token":"`+first+`","password":"new-password-2"}`, ""); w.Code != 200 {
		t.Fatalf("the first link was lost by a failed request: %d %s", w.Code, w.Body)
	}
}
