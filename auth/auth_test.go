package auth

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Every signup and login here uses the real cost (600,000 iterations, about
// 0.3 s each), so the suite takes some seconds.

type rig struct {
	h    http.Handler
	st   *Store
	path string
	now  *time.Time
}

func newRig(t *testing.T, burst int) *rig {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data", "auth.json") // the folder is made by Open
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 28, 18, 0, 0, 0, time.UTC)
	g := &rig{st: st, path: path, now: &now}
	st.now = func() time.Time { return *g.now }
	var lim *Limiter
	if burst > 0 {
		lim = NewLimiter(burst, time.Minute)
	}
	g.h = New(Config{Store: st, Limiter: lim, CookieSecure: true})
	return g
}

func (g *rig) send(method, path, body, cookie string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	r.RemoteAddr = "192.0.2.5:4000"
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: CookieName, Value: cookie})
	}
	w := httptest.NewRecorder()
	g.h.ServeHTTP(w, r)
	return w
}

func bodyOf(w *httptest.ResponseRecorder) map[string]any {
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return m
}

func theCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == CookieName {
			return c
		}
	}
	t.Fatalf("no %s cookie: %v", CookieName, w.Header())
	return nil
}

// ---- the flow -------------------------------------------------------------

func TestSignupMeLogoutLogin(t *testing.T) {
	g := newRig(t, 0)
	w := g.send("POST", "/api/auth/signup", `{"email":"  Donor@Example.ORG ","password":"tray-of-lasagna"}`, "")
	if w.Code != 201 {
		t.Fatalf("signup %d %s", w.Code, w.Body)
	}
	u := bodyOf(w)
	if u["email"] != "donor@example.org" || !strings.HasPrefix(u["id"].(string), "u_") || u["created_at"] != "2026-10-28T18:00:00Z" {
		t.Fatalf("user %v", u)
	}
	if _, leak := u["pw_hash"]; leak || len(u) != 3 {
		t.Fatalf("the user JSON has other fields: %v", u)
	}
	ck := theCookie(t, w)
	if !ck.HttpOnly || !ck.Secure || ck.SameSite != http.SameSiteLaxMode || ck.Path != "/" || len(ck.Value) != 43 {
		t.Fatalf("cookie %+v", ck)
	}
	// Expires is the session end of the store's clock (the test clock);
	// Max-Age is counted from the real clock, as before, so only its sign is checked.
	if !ck.Expires.Equal(g.now.Add(SessionTTL)) || ck.MaxAge <= 0 {
		t.Fatalf("expires %s max-age %d", ck.Expires, ck.MaxAge)
	}
	if w := g.send("GET", "/api/auth/me", "", ck.Value); w.Code != 200 || bodyOf(w)["id"] != u["id"] {
		t.Fatalf("me %d %s", w.Code, w.Body)
	}
	w = g.send("POST", "/api/auth/logout", "", ck.Value)
	if w.Code != 204 {
		t.Fatalf("logout %d", w.Code)
	}
	if cleared := theCookie(t, w); cleared.Value != "" || cleared.MaxAge != -1 {
		t.Fatalf("logout cookie %+v", cleared)
	}
	if w := g.send("GET", "/api/auth/me", "", ck.Value); w.Code != 401 || bodyOf(w)["error"] != "not logged in" {
		t.Fatalf("me after logout %d", w.Code)
	}
	w = g.send("POST", "/api/auth/login", `{"email":"DONOR@example.org","password":"tray-of-lasagna"}`, "")
	if w.Code != 200 || bodyOf(w)["id"] != u["id"] || theCookie(t, w).Value == ck.Value {
		t.Fatalf("login %d %s", w.Code, w.Body)
	}
}

func TestSignupRefusals(t *testing.T) {
	g := newRig(t, 0)
	if w := g.send("POST", "/api/auth/signup", `{"email":"a@b.co","password":"12345678"}`, ""); w.Code != 201 {
		t.Fatalf("first %d", w.Code)
	}
	for _, tc := range []struct {
		body string
		code int
		msg  string
	}{
		{`{"email":"A@B.co ","password":"another-one"}`, 409, "email already registered"},
		{`{"email":"not-an-email","password":"12345678"}`, 400, "email is not valid"},
		{`{"email":"a b@c.de","password":"12345678"}`, 400, "email is not valid"},
		{`{"email":"x@y.co","password":"1234567"}`, 400, "password must be at least 8 characters"},
		{`{"email":`, 400, "bad JSON body"},
	} {
		w := g.send("POST", "/api/auth/signup", tc.body, "")
		if w.Code != tc.code || bodyOf(w)["error"] != tc.msg {
			t.Errorf("%s: %d %s", tc.body, w.Code, w.Body)
		}
	}
	r := httptest.NewRequest("POST", "/api/auth/signup", strings.NewReader(`email=a`))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	g.h.ServeHTTP(w, r)
	if w.Code != 415 || bodyOf(w)["error"] != "send application/json" {
		t.Fatalf("form body %d %s", w.Code, w.Body)
	}
	big := `{"email":"big@x.co","password":"` + strings.Repeat("p", maxBodyBytes) + `"}`
	if w := g.send("POST", "/api/auth/signup", big, ""); w.Code != 400 {
		t.Fatalf("over 16 KiB %d", w.Code)
	}
}

// X49w: the stricter email form (any country); the app's EMAIL_FORM has the same cases (mobile/tests/signin.test.mts).
func TestEmailForm(t *testing.T) {
	for _, ok := range []string{"sam@example.com", "a@b.co", "donor@example.org", "x.y+z@mail.example.co.uk", "ade@company.ng",
		"priya@school.in", "korede@thehungreeapp.com", "o'brien@example.ie"} {
		if !emailForm.MatchString(ok) {
			t.Errorf("refused %q", ok)
		}
	}
	for _, bad := range []string{"sam", "sam@", "sam@example", "@example.com", "sam@@example.com", "sam @example.com", "sam@example.",
		"sam@example.c", "sam@example.c0m", "sam@.example.com", "sam@example..com", "sam@-example.com", "sam@example-.com",
		"sam(at)example.com", "sam@exa mple.com", "sam@example.com.", "sam,x@example.com"} {
		if emailForm.MatchString(bad) {
			t.Errorf("took %q", bad)
		}
	}
	// X49w review: the reset request keeps the loose form, so an account made before the strict form gets its link
	for _, ok := range []string{"sam@example.c", "sam@example.c0m", "josé@example.com", "sam@example.com"} {
		if !emailLoose.MatchString(ok) {
			t.Errorf("loose refused %q", ok)
		}
	}
	for _, bad := range []string{"sam", "sam@example", "sam @example.com", "sam@@example.com"} {
		if emailLoose.MatchString(bad) {
			t.Errorf("loose took %q", bad)
		}
	}
	s := newRig(t, 0).st
	if _, _, ok, err := s.NewReset("sam@example.c"); ok || err != nil {
		t.Errorf("NewReset(old form) = ok %v, err %v; want no account and no error", ok, err)
	}
	if _, err := s.Signup("sam@example.c", "abcdefgh"); err != ErrBadEmail {
		t.Errorf("Signup(old form) err = %v; want ErrBadEmail", err)
	}
}

// X49w (T9): a wrong password and an unknown email have two answers, both 401 with no cookie, each with its code.
func TestLoginTwoAnswers(t *testing.T) {
	g := newRig(t, 0)
	g.send("POST", "/api/auth/signup", `{"email":"a@b.co","password":"right-password"}`, "")
	wrong := g.send("POST", "/api/auth/login", `{"email":"a@b.co","password":"wrong-password"}`, "")
	none := g.send("POST", "/api/auth/login", `{"email":"nobody@b.co","password":"right-password"}`, "")
	if wrong.Code != 401 || bodyOf(wrong)["code"] != CodeWrongPassword || bodyOf(wrong)["error"] != "wrong password" {
		t.Fatalf("wrong %d %s", wrong.Code, wrong.Body)
	}
	if none.Code != 401 || bodyOf(none)["code"] != CodeNoAccount || bodyOf(none)["error"] != "no account uses this email" {
		t.Fatalf("none %d %s", none.Code, none.Body)
	}
	if len(wrong.Result().Cookies()) != 0 || len(none.Result().Cookies()) != 0 {
		t.Fatal("a failed login sets a cookie")
	}
	if !errors.Is(ErrNoAccount, ErrBadLogin) || !errors.Is(ErrWrongPassword, ErrBadLogin) {
		t.Fatal("both are a bad login")
	}
}

// An unknown email does the PBKDF2 work too, so the time does not tell which
// emails exist. Measured as "not much faster" (a loose bound: CI is noisy).
func TestLoginMissTakesTheHashTime(t *testing.T) {
	g := newRig(t, 0)
	g.send("POST", "/api/auth/signup", `{"email":"a@b.co","password":"right-password"}`, "")
	timeIt := func(body string) time.Duration {
		start := time.Now()
		g.send("POST", "/api/auth/login", body, "")
		return time.Since(start)
	}
	median := func(body string) time.Duration {
		var d []time.Duration
		for i := 0; i < 3; i++ {
			d = append(d, timeIt(body))
		}
		sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
		return d[1]
	}
	wrong := median(`{"email":"a@b.co","password":"wrong-password"}`)
	miss := median(`{"email":"nobody@b.co","password":"wrong-password"}`)
	if miss < wrong/3 {
		t.Fatalf("an unknown email is much faster: %s vs %s", miss, wrong)
	}
}

// ---- the password hash ----------------------------------------------------

func TestPasswordHash(t *testing.T) {
	h, err := hashPassword("secret-pass", 1000)
	if err != nil {
		t.Fatal(err)
	}
	f := strings.Split(h, "$")
	if len(f) != 5 || f[0] != "pbkdf2" || f[1] != "sha256" || f[2] != "1000" {
		t.Fatalf("form %q", h)
	}
	if !verifyPassword(h, "secret-pass") || verifyPassword(h, "secret-pasS") || verifyPassword(h, "") {
		t.Fatal("verify")
	}
	h2, _ := hashPassword("secret-pass", 1000)
	if h2 == h {
		t.Fatal("the salt is not random")
	}
	for _, bad := range []string{"", "pbkdf2$sha1$1000$" + f[3] + "$" + f[4], "pbkdf2$sha256$999$" + f[3] + "$" + f[4],
		"pbkdf2$sha256$1000$" + f[3] + "$" + f[4] + "$extra",
		"pbkdf2$sha256$x$" + f[3] + "$" + f[4], "pbkdf2$sha256$1000$!!$" + f[4], "pbkdf2$sha256$1000$" + f[3] + "$",
		"pbkdf2$sha256$1000$" + f[3]} {
		if verifyPassword(bad, "secret-pass") {
			t.Errorf("accepted %q", bad)
		}
	}
	if !strings.HasPrefix(missHash, "pbkdf2$sha256$600000$") {
		t.Fatalf("the miss hash must cost the same as a real one: %q", missHash[:24])
	}
}

// The key derivation is PBKDF2-HMAC-SHA256: a published vector, RFC 7914
// section 11, P="Password", S="NaCl", c=80000, dkLen=64
// (https://www.rfc-editor.org/rfc/rfc7914).
func TestPBKDF2KnownVector(t *testing.T) {
	want, err := hex.DecodeString("4ddcd8f60b98be21830cee5ef22701f9641a4418d04c0414aeff08876b34ab56" +
		"a1d425a1225833549adb841b51c9b3176a272bdebba1d078478f62b397f33c8d")
	if err != nil {
		t.Fatal(err)
	}
	got, err := derive("Password", []byte("NaCl"), 80000, 64)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("derive %x %v", got, err)
	}
}

// A damaged stored hash is never a match (the X38c review): a short key, a
// short salt, too few or too many iterations.
func TestDamagedHashIsNoMatch(t *testing.T) {
	salt16 := b64.EncodeToString(make([]byte, 16))
	key1, _ := derive("pw", make([]byte, 16), 1000, 1)
	key32, _ := derive("pw", make([]byte, 16), 1000, 32)
	short := "pbkdf2$sha256$1000$" + salt16 + "$" + b64.EncodeToString(key1)
	if verifyPassword(short, "pw") {
		t.Fatal("a 1-byte key must be refused, also with the right password")
	}
	good := "pbkdf2$sha256$1000$" + salt16 + "$" + b64.EncodeToString(key32)
	if !verifyPassword(good, "pw") {
		t.Fatal("the good form must verify")
	}
	shortSalt := "pbkdf2$sha256$1000$" + b64.EncodeToString(make([]byte, 8)) + "$" + b64.EncodeToString(key32)
	tooMany := "pbkdf2$sha256$6000001$" + salt16 + "$" + b64.EncodeToString(key32)
	for _, bad := range []string{shortSalt, tooMany} {
		start := time.Now()
		if verifyPassword(bad, "pw") {
			t.Errorf("accepted %q", bad)
		}
		if time.Since(start) > 5*time.Second {
			t.Fatalf("a damaged hash took %s", time.Since(start))
		}
	}
}

// A damaged hash takes the time of a real check, so the time does not show
// which accounts have one.
func TestDamagedHashTakesTheHashTime(t *testing.T) {
	median := func(stored string) time.Duration {
		var d []time.Duration
		for i := 0; i < 5; i++ {
			start := time.Now()
			verifyPassword(stored, "pw")
			d = append(d, time.Since(start))
		}
		sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
		return d[2]
	}
	real := median(missHash)
	damaged := median("pbkdf2$sha256$x$y$z")
	if damaged < real/3 {
		t.Fatalf("a damaged hash is much faster: %s vs %s", damaged, real)
	}
}

// ---- sessions and the data file -------------------------------------------

func TestTheFileHasNoTokenAndNoPassword(t *testing.T) {
	g := newRig(t, 0)
	w := g.send("POST", "/api/auth/signup", `{"email":"a@b.co","password":"my-plain-password"}`, "")
	tok := theCookie(t, w).Value
	b, err := os.ReadFile(g.path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(tok)) || bytes.Contains(b, []byte("my-plain-password")) {
		t.Fatal("the data file has the raw token or the password")
	}
	if !bytes.Contains(b, []byte(hashToken(tok))) {
		t.Fatal("the data file has no hash of the session")
	}
	if fi, _ := os.Stat(g.path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %v", fi.Mode().Perm())
	}
	if _, err := os.Stat(g.path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("a temp file was left")
	}
}

func TestSessionExpiresAndIsRemoved(t *testing.T) {
	g := newRig(t, 0)
	tok := theCookie(t, g.send("POST", "/api/auth/signup", `{"email":"a@b.co","password":"12345678"}`, "")).Value
	*g.now = g.now.Add(SessionTTL - time.Second)
	if w := g.send("GET", "/api/auth/me", "", tok); w.Code != 200 {
		t.Fatalf("one second before the end: %d", w.Code)
	}
	*g.now = g.now.Add(time.Second)
	if w := g.send("GET", "/api/auth/me", "", tok); w.Code != 401 {
		t.Fatalf("at the end: %d", w.Code)
	}
	if g.st.sessionCount() != 0 {
		t.Fatal("the expired session was kept")
	}
}

func TestNewSessionRemovesExpiredOnes(t *testing.T) {
	g := newRig(t, 0)
	u, err := g.st.Signup("a@b.co", "12345678")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		g.st.NewSession(u.ID)
	}
	*g.now = g.now.Add(SessionTTL)
	g.st.NewSession(u.ID)
	if n := g.st.sessionCount(); n != 1 {
		t.Fatalf("sessions %d", n)
	}
}

func TestReopenKeepsUsersAndSessions(t *testing.T) {
	g := newRig(t, 0)
	tok := theCookie(t, g.send("POST", "/api/auth/signup", `{"email":"a@b.co","password":"12345678"}`, "")).Value
	st, err := Open(g.path)
	if err != nil {
		t.Fatal(err)
	}
	st.now = g.st.now
	if u, err := st.UserForSession(tok); err != nil || u.Email != "a@b.co" {
		t.Fatalf("after reopen: %+v %v", u, err)
	}
	if _, err := st.Login("a@b.co", "12345678"); err != nil {
		t.Fatalf("login after reopen: %v", err)
	}
	os.WriteFile(g.path, []byte("{broken"), 0o600)
	if _, err := Open(g.path); err == nil || !strings.Contains(err.Error(), "auth.json") {
		t.Fatalf("a broken file must stop the start: %v", err)
	}
}

// oldDataFile was written by the auth service of the app before the
// Hackathon: a user (password "old-pass-123") and a session (oldToken).
// The accounts on the data volume must keep working.
const oldDataFile = `{
 "users": {
  "u_173fb968b678ebad": {
   "id": "u_173fb968b678ebad",
   "email": "donor@example.org",
   "pw_hash": "pbkdf2$sha256$600000$E5fNQLDXmTCGJqNSPT8+kQ$ybYuH+kgDgEeK3qsFtCqxh8PN3q5dgwPYV3cLUrMsMk",
   "created_at": "2026-10-25T17:00:00Z"
  }
 },
 "emails": {
  "donor@example.org": "u_173fb968b678ebad"
 },
 "sessions": {
  "1b5ea92936bc8fb4b595cdcf56cadf0c7c572f6472ca4f23480778d5e700be03": {
   "id_hash": "1b5ea92936bc8fb4b595cdcf56cadf0c7c572f6472ca4f23480778d5e700be03",
   "user_id": "u_173fb968b678ebad",
   "created_at": "2026-10-25T17:00:00Z",
   "expires_at": "2026-11-01T17:00:00Z"
  }
 }
}`

const oldToken = "jZDQMmdZkzN0XEu3dVNDfJ_8a0brLfskfG3Y4YSFhYs"

func TestOldAccountsKeepWorking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(oldDataFile), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st.now = func() time.Time { return time.Date(2026, 10, 28, 18, 0, 0, 0, time.UTC) }
	if u, err := st.UserForSession(oldToken); err != nil || u.ID != "u_173fb968b678ebad" {
		t.Fatalf("old session: %+v %v", u, err)
	}
	if u, err := st.Login(" DONOR@example.org", "old-pass-123"); err != nil || u.CreatedAt != "2026-10-25T17:00:00Z" {
		t.Fatalf("old password: %+v %v", u, err)
	}
	if _, err := st.Login("donor@example.org", "old-pass-124"); err != ErrWrongPassword {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := st.Signup("donor@example.org", "12345678"); err != ErrEmailTaken {
		t.Fatalf("the old email is taken: %v", err)
	}
}

// A failed write leaves no temp file of its own (the rename fails here: a
// folder is where the data file must go).
func TestFailedWriteLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil { // rename onto a folder fails
		t.Fatal(err)
	}
	if _, err := st.Signup("a@b.co", "12345678"); err == nil {
		t.Fatal("the write must fail")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("the failed write left its temp file")
	}
}

// A signup that is not written makes no account: the email stays free.
func TestSignupWriteFailureMakesNoAccount(t *testing.T) {
	g := newRig(t, 0)
	if err := os.Mkdir(g.path+".tmp", 0o700); err != nil { // a folder where the temp file must go: the write fails, also as root
		t.Fatal(err)
	}
	w := g.send("POST", "/api/auth/signup", `{"email":"a@b.co","password":"12345678"}`, "")
	if w.Code != 500 || bodyOf(w)["error"] != "could not create account" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if err := os.Remove(g.path + ".tmp"); err != nil {
		t.Fatal(err)
	}
	if w := g.send("POST", "/api/auth/signup", `{"email":"a@b.co","password":"12345678"}`, ""); w.Code != 201 {
		t.Fatalf("the email must still be free: %d %s", w.Code, w.Body)
	}
}

// A logout ends the session in memory also when the file write fails.
func TestLogoutEndsTheSessionWhenTheWriteFails(t *testing.T) {
	g := newRig(t, 0)
	tok := theCookie(t, g.send("POST", "/api/auth/signup", `{"email":"a@b.co","password":"12345678"}`, "")).Value
	if err := os.Mkdir(g.path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	if w := g.send("POST", "/api/auth/logout", "", tok); w.Code != 204 {
		t.Fatalf("logout %d", w.Code)
	}
	if w := g.send("GET", "/api/auth/me", "", tok); w.Code != 401 {
		t.Fatalf("the session lives on: %d", w.Code)
	}
	if w := g.send("POST", "/api/auth/logout", "", "unknown"); w.Code != 204 {
		t.Fatalf("logout of an unknown session %d", w.Code)
	}
}

func TestSignupOneAccountPerEmail(t *testing.T) {
	g := newRig(t, 0)
	var ok atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := g.st.Signup("race@b.co", "12345678"); err == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 {
		t.Fatalf("%d accounts for one email", ok.Load())
	}
}

// ---- rate limit, healthz --------------------------------------------------

func TestRateLimitOnSignupAndLogin(t *testing.T) {
	g := newRig(t, 2)
	login := func(fwd string) int {
		r := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"email":"x@y.co","password":"12345678"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-For", fwd)
		w := httptest.NewRecorder()
		g.h.ServeHTTP(w, r)
		return w.Code
	}
	if login("198.51.100.1") != 401 || login("198.51.100.1") != 401 || login("198.51.100.1") != 429 {
		t.Fatal("the third try of one address must be 429")
	}
	if login("198.51.100.2, 10.0.0.1") != 401 {
		t.Fatal("another address has its own budget")
	}
	// signup and login of one address share one budget; /me and logout are not limited
	if w := g.send("POST", "/api/auth/login", `{"email":"a@b.co","password":"12345678"}`, ""); w.Code != 401 {
		t.Fatalf("login %d", w.Code)
	}
	if w := g.send("POST", "/api/auth/signup", `{"email":"a@b.co","password":"12345678"}`, ""); w.Code != 201 {
		t.Fatalf("signup %d", w.Code)
	}
	if w := g.send("POST", "/api/auth/signup", `{"email":"c@b.co","password":"12345678"}`, ""); w.Code != 429 || bodyOf(w)["error"] != "rate limited" {
		t.Fatalf("the third try of the address must be 429: %d", w.Code)
	}
	for i := 0; i < 5; i++ {
		if w := g.send("GET", "/api/auth/me", "", "x"); w.Code != 401 {
			t.Fatalf("me is limited: %d", w.Code)
		}
	}
}

// The prune removes idle keys, keeps the recent hits of live ones (each one
// time) and runs at most one time per window.
func TestLimiterPrune(t *testing.T) {
	l := NewLimiter(2, time.Minute)
	now := time.Unix(5000, 0)
	l.now = func() time.Time { return now }
	for i := 0; i < pruneAt-1; i++ {
		l.Allow(fmt.Sprintf("idle%d", i)) // t=0
	}
	now = now.Add(50 * time.Second)
	l.Allow("live") // t=50s; the map now has pruneAt keys
	now = now.Add(11 * time.Second)
	l.Allow("new") // t=61s: a new key at the limit: the idle keys go
	if len(l.hits) != 2 {
		t.Fatalf("keys after the prune: %d", len(l.hits))
	}
	if !l.Allow("live") || l.Allow("live") {
		t.Fatal("live has one hit in the window: one more is allowed, then 429")
	}
	first := l.pruned
	for i := 0; i < pruneAt; i++ {
		l.Allow(fmt.Sprintf("more%d", i))
	}
	if !l.pruned.Equal(first) {
		t.Fatal("a second prune in the same window")
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "172.18.0.9:999"
	if ip := clientIP(r); ip != "172.18.0.9" {
		t.Fatal(ip)
	}
	r.Header.Set("X-Forwarded-For", " 203.0.113.1 ,10.0.0.1")
	if ip := clientIP(r); ip != "203.0.113.1" {
		t.Fatal(ip)
	}
	r.Header.Set("CF-Connecting-IP", "203.0.113.2")
	if ip := clientIP(r); ip != "203.0.113.2" {
		t.Fatal(ip)
	}
}

func TestHealthz(t *testing.T) {
	g := newRig(t, 0)
	w := g.send("GET", "/api/auth/healthz", "", "")
	if w.Code != 200 || bodyOf(w)["service"] != "auth" || bodyOf(w)["ok"] != true {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

// ---- the events log -------------------------------------------------------

func TestLoggedHasNoEmailOrCookie(t *testing.T) {
	g := newRig(t, 0)
	dir := t.TempDir()
	rec := NewRecorder(dir, 16)
	h := Logged("auth", "abc1234", rec, g.h)
	r := httptest.NewRequest("POST", "/api/auth/signup", strings.NewReader(`{"email":"secret@b.co","password":"12345678"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Trace-Id", "scan-7")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	tok := theCookie(t, w).Value
	rec.Close()
	files, _ := filepath.Glob(filepath.Join(dir, "events-*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("files %v", files)
	}
	b, _ := os.ReadFile(files[0])
	var e Event
	if err := json.Unmarshal(bytes.TrimSpace(b), &e); err != nil {
		t.Fatal(err)
	}
	if e.Route != "POST /api/auth/signup" || e.Status != 201 || e.TraceID != "scan-7" || e.Service != "auth" || e.Build != "abc1234" {
		t.Fatalf("event %+v", e)
	}
	for _, s := range []string{"secret@b.co", "12345678", tok} {
		if bytes.Contains(b, []byte(s)) {
			t.Fatalf("the events log has %q", s)
		}
	}
}
