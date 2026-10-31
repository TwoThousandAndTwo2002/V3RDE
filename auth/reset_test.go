package auth

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// resetRig is a rig with the outbox mailer; wait() waits for the background
// work of one reset request.
type resetRig struct {
	*rig
	outbox string
	done   chan struct{}
}

func newResetRig(t *testing.T, perEmail int) *resetRig {
	g := newRig(t, 0)
	r := &resetRig{rig: g, outbox: filepath.Join(t.TempDir(), "outbox"), done: make(chan struct{}, 16)}
	var lim *Limiter
	if perEmail > 0 {
		lim = NewLimiter(perEmail, time.Hour)
	}
	g.h = New(Config{Store: g.st, CookieSecure: true, Mailer: OutboxMailer{Dir: r.outbox},
		ResetLinkBase: "https://app.example/", ResetLimiter: lim, resetDone: func() { r.done <- struct{}{} }})
	return r
}

func (r *resetRig) wait(t *testing.T) {
	t.Helper()
	select {
	case <-r.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the reset work did not end")
	}
}

var linkRe = regexp.MustCompile(`https://app\.example/reset#token=([A-Za-z0-9_-]{43})\r\n`)

// the mails in the outbox, oldest first, and the token of the last one
func (r *resetRig) mails(t *testing.T) ([]string, string) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(r.outbox, "*.eml"))
	var out []string
	tok := ""
	for _, f := range files {
		st, _ := os.Stat(f)
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode %v", f, st.Mode())
		}
		b, _ := os.ReadFile(f)
		out = append(out, string(b))
		if m := linkRe.FindStringSubmatch(string(b)); m != nil {
			tok = m[1]
		}
	}
	return out, tok
}

// N5: the request answers 202 for an account and for no account, with one
// email only for the account (the link with a 43-character token after "#");
// the confirm sets the password, signs in, ends the other sessions, and works
// one time.
func TestResetByEmailLink(t *testing.T) {
	g := newResetRig(t, 0)
	old := theCookie(t, g.send("POST", "/api/auth/signup", `{"email":"Ann@Example.org","password":"first-pass"}`, "")).Value
	if w := g.send("POST", "/api/auth/reset", `{"email":"nobody@example.org"}`, ""); w.Code != 202 || w.Body.String() != "{}\n" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	g.wait(t)
	if m, _ := g.mails(t); len(m) != 0 {
		t.Fatalf("a mail for no account: %v", m)
	}
	if w := g.send("POST", "/api/auth/reset", `{"email":" ann@example.org "}`, ""); w.Code != 202 || w.Body.String() != "{}\n" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	g.wait(t)
	m, tok := g.mails(t)
	if len(m) != 1 || tok == "" || !strings.HasPrefix(m[0], "To: ann@example.org\r\nSubject: Reset your V3RDE password\r\n") ||
		!strings.Contains(m[0], "The link works for 30 minutes, one time.") {
		t.Fatalf("%q", m)
	}
	b, _ := os.ReadFile(g.path)
	if bytes.Contains(b, []byte(tok)) || !bytes.Contains(b, []byte(hashToken(tok))) {
		t.Fatal("the data file must have the token's hash only")
	}
	// short password, then a wrong token: the password does not change
	if w := g.send("POST", "/api/auth/reset/confirm", `{"token":"`+tok+`","password":"short"}`, ""); w.Code != 400 || bodyOf(w)["error"] != "password too short" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := g.send("POST", "/api/auth/reset/confirm", `{"token":"`+other(tok)+`","password":"second-pass"}`, ""); w.Code != 400 || bodyOf(w)["error"] != "reset link expired" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	w := g.send("POST", "/api/auth/reset/confirm", `{"token":"`+tok+`","password":"second-pass"}`, "")
	if w.Code != 200 || bodyOf(w)["email"] != "ann@example.org" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	fresh := theCookie(t, w).Value
	if g.send("GET", "/api/auth/me", "", old).Code != 401 || g.send("GET", "/api/auth/me", "", fresh).Code != 200 {
		t.Fatal("the old session must end and the new one work")
	}
	if g.send("POST", "/api/auth/login", `{"email":"ann@example.org","password":"first-pass"}`, "").Code != 401 ||
		g.send("POST", "/api/auth/login", `{"email":"ann@example.org","password":"second-pass"}`, "").Code != 200 {
		t.Fatal("the password did not change")
	}
	if w := g.send("POST", "/api/auth/reset/confirm", `{"token":"`+tok+`","password":"third-pass"}`, ""); w.Code != 400 || bodyOf(w)["error"] != "reset link expired" {
		t.Fatalf("used twice: %d %s", w.Code, w.Body)
	}
}

// N5: a link older than 30 minutes is expired; a new request replaces the
// earlier link; the store survives a reopen; bad input is refused.
func TestResetExpiryAndReplace(t *testing.T) {
	g := newResetRig(t, 0)
	g.send("POST", "/api/auth/signup", `{"email":"bo@example.org","password":"first-pass"}`, "")
	g.send("POST", "/api/auth/reset", `{"email":"bo@example.org"}`, "")
	g.wait(t)
	_, first := g.mails(t)
	*g.now = g.now.Add(time.Second)
	g.send("POST", "/api/auth/reset", `{"email":"bo@example.org"}`, "")
	g.wait(t)
	m, second := g.mails(t)
	if len(m) != 2 || first == second {
		t.Fatalf("%d mails", len(m))
	}
	if w := g.send("POST", "/api/auth/reset/confirm", `{"token":"`+first+`","password":"second-pass"}`, ""); w.Code != 400 {
		t.Fatalf("the replaced link still works: %d", w.Code)
	}
	st2, err := Open(g.path)
	if err != nil || len(st2.data.Resets) != 1 {
		t.Fatalf("reopen: %v %d", err, len(st2.data.Resets))
	}
	*g.now = g.now.Add(ResetTTL)
	if w := g.send("POST", "/api/auth/reset/confirm", `{"token":"`+second+`","password":"second-pass"}`, ""); w.Code != 400 || bodyOf(w)["error"] != "reset link expired" {
		t.Fatalf("an old link works: %d %s", w.Code, w.Body)
	}
	for _, c := range []struct {
		path, body string
		code       int
	}{
		{"/api/auth/reset", `{"email":"not an email"}`, 400},
		{"/api/auth/reset", `{"email":`, 400},
		{"/api/auth/reset/confirm", `{"token":"","password":"long-enough"}`, 400},
		{"/api/auth/reset/confirm", `{"token":"` + strings.Repeat("a", 200) + `","password":"long-enough"}`, 400},
	} {
		if w := g.send("POST", c.path, c.body, ""); w.Code != c.code {
			t.Fatalf("%s %s: %d", c.path, c.body, w.Code)
		}
	}
	r := g.send("POST", "/api/auth/reset", "", "")
	if r.Code != 415 {
		t.Fatalf("no JSON: %d", r.Code)
	}
}

// N5: the per-email limit (also for an email with no account), and no mailer
// = 202 and nothing sent.
func TestResetLimitAndNoMailer(t *testing.T) {
	g := newResetRig(t, 2)
	for i := 0; i < 2; i++ {
		if w := g.send("POST", "/api/auth/reset", `{"email":"x@example.org"}`, ""); w.Code != 202 {
			t.Fatal(w.Code)
		}
		g.wait(t)
	}
	if w := g.send("POST", "/api/auth/reset", `{"email":"X@example.org "}`, ""); w.Code != 429 {
		t.Fatalf("the third request: %d", w.Code)
	}
	if w := g.send("POST", "/api/auth/reset", `{"email":"y@example.org"}`, ""); w.Code != 202 {
		t.Fatalf("another email: %d", w.Code)
	}
	g.wait(t)
	h := newRig(t, 0)
	h.send("POST", "/api/auth/signup", `{"email":"cy@example.org","password":"first-pass"}`, "")
	if w := h.send("POST", "/api/auth/reset", `{"email":"cy@example.org"}`, ""); w.Code != 202 || len(h.st.data.Resets) != 0 {
		t.Fatalf("no mailer: %d %d", w.Code, len(h.st.data.Resets))
	}
}

// N5: the log lines have the user id only: never the email, the token or the password.
func TestResetLogsNoSecret(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	log.SetOutput(writerFunc(func(p []byte) (int, error) { mu.Lock(); defer mu.Unlock(); return buf.Write(p) }))
	defer log.SetOutput(os.Stderr)
	g := newResetRig(t, 0)
	g.send("POST", "/api/auth/signup", `{"email":"dee@example.org","password":"first-pass"}`, "")
	g.send("POST", "/api/auth/reset", `{"email":"dee@example.org"}`, "")
	g.wait(t)
	_, tok := g.mails(t)
	g.send("POST", "/api/auth/reset/confirm", `{"token":"`+tok+`","password":"second-pass"}`, "")
	mu.Lock()
	out := buf.String()
	mu.Unlock()
	if !strings.Contains(out, "reset_requested user=u_") || !strings.Contains(out, "reset_done ok user=u_") {
		t.Fatalf("log: %s", out)
	}
	for _, s := range []string{"dee@example.org", tok, "second-pass", "first-pass"} {
		if strings.Contains(out, s) {
			t.Fatalf("the log has %q", s)
		}
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// other is tok with its first character changed (never the same token).
func other(tok string) string {
	if tok[0] == 'x' {
		return "y" + tok[1:]
	}
	return "x" + tok[1:]
}

type failMailer struct{}

func (failMailer) Send(string, string, string) error { return os.ErrPermission }

// N5 review: a mail error still ends the work (and logs no secret); an old data
// file with no "resets" opens; the outbox removes files older than ResetTTL.
func TestResetMailErrorOldFileAndOutboxPrune(t *testing.T) {
	g := newResetRig(t, 0)
	g.h = New(Config{Store: g.st, CookieSecure: true, Mailer: failMailer{}, ResetLinkBase: "https://app.example",
		resetDone: func() { g.done <- struct{}{} }})
	g.send("POST", "/api/auth/signup", `{"email":"eve@example.org","password":"first-pass"}`, "")
	if w := g.send("POST", "/api/auth/reset", `{"email":"eve@example.org"}`, ""); w.Code != 202 {
		t.Fatal(w.Code)
	}
	g.wait(t)
	path := filepath.Join(t.TempDir(), "auth.json")
	os.WriteFile(path, []byte(`{"users":{},"emails":{},"sessions":{}}`), 0o600)
	st, err := Open(path)
	if err != nil || st.data.Resets == nil {
		t.Fatalf("old file: %v", err)
	}
	if tok, _, _, err := st.NewReset("nobody@example.org"); tok != "" || err != nil {
		t.Fatalf("no account: %q %v", tok, err)
	}
	dir := t.TempDir()
	oldFile := filepath.Join(dir, "20261001T000000.000000000Z-aa.eml")
	os.WriteFile(oldFile, []byte("x"), 0o600)
	os.Chtimes(oldFile, time.Now().Add(-ResetTTL-time.Minute), time.Now().Add(-ResetTTL-time.Minute))
	newFile := filepath.Join(dir, "20261031T000000.000000000Z-bb.eml")
	os.WriteFile(newFile, []byte("y"), 0o600)
	if err := (OutboxMailer{Dir: dir}).Send("a@example.org", "s", "b"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatal("the old file is still there")
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "*.eml")); len(files) != 2 {
		t.Fatalf("files %v", files)
	}
}
