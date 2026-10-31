package listings

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// N3: a new listing is posted to fan-out once, in the background, with only
// the giver's session cookie; a test account's listing and a failed create are
// not; no URL = off.
func TestShareEachNewListing(t *testing.T) {
	var mu sync.Mutex
	type hit struct{ path, cookie, other string }
	var hits []hit
	fan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, hit{r.Method + " " + r.URL.Path, r.Header.Get("Cookie"), r.Header.Get("X-Other")})
		mu.Unlock()
		w.WriteHeader(http.StatusServiceUnavailable) // no Page yet: fan-out says disabled
	}))
	defer fan.Close()
	done := make(chan int, 4)
	e := newEnv(t, nil)
	e.h = New(Config{Store: e.st, Auth: fakeAuth{}, ItemPhotos: e.items, TouchupPhotos: e.touch,
		TestTTLEmails: map[string]bool{"u_test@example.org": true},
		Share:         &ShareHook{URL: fan.URL + "/api/fanout/", Done: func(_ string, status int, _ error) { done <- status }}})
	req := form(t, "u_donor", okFields(), jpeg, "image/jpeg")
	req.Header.Set("X-Other", "not sent")
	req.AddCookie(&http.Cookie{Name: "other", Value: "not sent"})
	w, body := call(e.h, req)
	if w.Code != 201 {
		t.Fatalf("%d %v", w.Code, body)
	}
	select {
	case st := <-done:
		if st != 503 {
			t.Fatalf("status %d", st)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no share")
	}
	mu.Lock()
	got := append([]hit(nil), hits...)
	mu.Unlock()
	if len(got) != 1 || got[0].path != "POST /api/fanout/"+body["id"].(string) || got[0].cookie != sessionCookie+"=u_donor" || got[0].other != "" {
		t.Fatalf("%+v", got)
	}
	// a test account, and a create that fails: no share
	if w, _ := call(e.h, form(t, "u_test", okFields(), jpeg, "image/jpeg")); w.Code != 201 {
		t.Fatal(w.Code)
	}
	bad := okFields()
	bad["title"] = ""
	if w, _ := call(e.h, form(t, "u_donor", bad, jpeg, "image/jpeg")); w.Code != 400 {
		t.Fatal(w.Code)
	}
	select {
	case st := <-done:
		t.Fatalf("an extra share (%d)", st)
	case <-time.After(300 * time.Millisecond):
	}
	// off: no URL, a nil hook, no cookie, a bad id
	(&ShareHook{}).Share("l_0123456789abcdef", &http.Cookie{Name: sessionCookie, Value: "x"})
	(*ShareHook)(nil).Share("l_0123456789abcdef", &http.Cookie{Name: sessionCookie, Value: "x"})
	h := &ShareHook{URL: fan.URL + "/api/fanout/", Done: func(_ string, status int, _ error) { done <- status }}
	h.Share("l_0123456789abcdef", nil)
	h.Share("l_../../v1/fanout/status", &http.Cookie{Name: sessionCookie, Value: "x"})
	h.Share("l_0123456789ABCDEF", &http.Cookie{Name: sessionCookie, Value: "x"})
	select {
	case st := <-done:
		t.Fatalf("a share that should be off (%d)", st)
	case <-time.After(300 * time.Millisecond):
	}
	mu.Lock()
	defer mu.Unlock()
	if len(hits) != 1 || strings.Contains(hits[0].cookie, "other") {
		t.Fatalf("%+v", hits)
	}
}

// N3 review: no answer, 429 and 502 are tried again (at most 1 + len(Backoff)
// tries); 409 and 503 end it at once; the test-account check ignores case
// and spaces.
func TestShareTriesAgain(t *testing.T) {
	var mu sync.Mutex
	answers := []int{502, 429, 201}
	n := 0
	fan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		code := 201
		if n < len(answers) {
			code = answers[n]
		}
		n++
		w.WriteHeader(code)
	}))
	defer fan.Close()
	done := make(chan int, 2)
	h := &ShareHook{URL: fan.URL + "/api/fanout/", Backoff: []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond},
		Done: func(_ string, status int, _ error) { done <- status }}
	ck := &http.Cookie{Name: sessionCookie, Value: "u1"}
	h.Share("l_0123456789abcdef", ck)
	if st := <-done; st != 201 {
		t.Fatalf("status %d", st)
	}
	for _, c := range []struct {
		first int
		tries int
	}{{409, 1}, {503, 1}, {500, 4}} {
		mu.Lock()
		answers, n = []int{c.first, c.first, c.first, c.first, c.first}, 0
		mu.Unlock()
		h.Share("l_0123456789abcdef", ck)
		if st := <-done; st != c.first {
			t.Fatalf("%d: status %d", c.first, st)
		}
		mu.Lock()
		got := n
		mu.Unlock()
		if got != c.tries {
			t.Fatalf("%d: %d tries, want %d", c.first, got, c.tries)
		}
	}
	// no answer at all: tried again, then the error
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead.Close()
	errs := make(chan error, 1)
	(&ShareHook{URL: dead.URL + "/api/fanout/", Backoff: []time.Duration{time.Millisecond}, Done: func(_ string, _ int, err error) { errs <- err }}).Share("l_0123456789abcdef", ck)
	if err := <-errs; err == nil {
		t.Fatal("no error from a closed server")
	}
	// the test-account rule: " U_Test@Example.org " is a test account
	e := newEnv(t, nil)
	shared := make(chan int, 1)
	e.h = New(Config{Store: e.st, Auth: caseAuth{}, TestTTLEmails: map[string]bool{"u_test@example.org": true},
		Share: &ShareHook{URL: fan.URL + "/api/fanout/", Done: func(_ string, st int, _ error) { shared <- st }}})
	if w, _ := call(e.h, form(t, "u_test", okFields(), jpeg, "image/jpeg")); w.Code != 201 {
		t.Fatal(w.Code)
	}
	select {
	case <-shared:
		t.Fatal("a test account was shared")
	case <-time.After(200 * time.Millisecond):
	}
}

// caseAuth gives the user an email in another case, with spaces.
type caseAuth struct{}

func (caseAuth) Who(r *http.Request) (User, bool) {
	ck, err := r.Cookie(sessionCookie)
	if err != nil || ck.Value == "" {
		return User{}, false
	}
	return User{ID: ck.Value, Email: " " + strings.ToUpper(ck.Value[:1]) + ck.Value[1:] + "@Example.org "}, true
}
