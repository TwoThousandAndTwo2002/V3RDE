package listings

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// patch is a PATCH /api/listings/{id} with a JSON body.
func patch(user, id, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPatch, "/api/listings/"+id, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if user != "" {
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: user})
	}
	return r
}

// X60 (T9 2026-11-01): the owner changes the title and the servings of a
// listing nobody has grabbed, and resets its timer; the data file keeps the
// change; another user gets 404; bad fields are 400 and change nothing.
func TestUpdateTitleServingsAndTimer(t *testing.T) {
	e := newEnv(t, nil)
	l := e.create(t, "u_donor")
	if w, _ := call(e.h, patch("", l.ID, `{"reset":true}`)); w.Code != 401 {
		t.Fatalf("no session: %d", w.Code)
	}
	if w, body := call(e.h, patch("u_other", l.ID, `{"title":"Mine now"}`)); w.Code != 404 || body["error"] != ErrNotFound.Error() {
		t.Fatalf("another user: %d %v", w.Code, body)
	}
	for _, bad := range []string{`{}`, `{"title":"  "}`, `{"title":"` + strings.Repeat("x", MaxTitle+1) + `"}`,
		`{"number_of_serving":0}`, `{"number_of_serving":100}`, `{"pickup_text":"x"}`, `not json`} {
		if w, body := call(e.h, patch("u_donor", l.ID, bad)); w.Code != 400 {
			t.Fatalf("%s: %d %v", bad, w.Code, body)
		}
	}
	e.tick(20 * time.Minute)
	w, body := call(e.h, patch("u_donor", l.ID, `{"title":"  Rice, family box ","number_of_serving":10}`))
	if w.Code != 200 || body["title"] != "Rice, family box" || body["number_of_serving"] != float64(10) || body["expires_at"] != l.ExpiresAt {
		t.Fatalf("change: %d %v (the timer must not move without reset)", w.Code, body)
	}
	w, body = call(e.h, patch("u_donor", l.ID, `{"reset":true}`))
	want := e.clock.Add(30 * time.Minute).Format(time.RFC3339Nano)
	if w.Code != 200 || body["expires_at"] != want || body["title"] != "Rice, family box" {
		t.Fatalf("reset: %d %v, want expires_at %s", w.Code, body, want)
	}
	b, _ := os.ReadFile(filepath.Join(e.dir, "listings.json"))
	if !strings.Contains(string(b), "Rice, family box") || !strings.Contains(string(b), want) {
		t.Fatal("the data file does not have the change")
	}
	if st, err := Open(e.dir, 30*time.Minute); err != nil || st.Mine("u_donor")[0].NumberOfServing != 10 {
		t.Fatalf("reopen: %v", err)
	}
}

// X60 (T9: "post again"): an ended listing changes only with reset, and then
// it is in the feed again; a grabbed listing never changes (409), also with
// reset, because the receiver counts on it.
func TestUpdateEndedAndGrabbed(t *testing.T) {
	e := newEnv(t, nil)
	l := e.create(t, "u_donor")
	e.tick(31 * time.Minute)
	if items, _ := e.st.Active("", 0); len(items) != 0 {
		t.Fatal("the ended listing is in the feed")
	}
	if w, body := call(e.h, patch("u_donor", l.ID, `{"number_of_serving":3}`)); w.Code != 409 || !strings.Contains(body["error"].(string), "reset") {
		t.Fatalf("ended without reset: %d %v", w.Code, body)
	}
	if got := e.st.Mine("u_donor")[0]; got.NumberOfServing == 3 {
		t.Fatal("a refused change was kept")
	}
	if w, body := call(e.h, patch("u_donor", l.ID, `{"number_of_serving":3,"reset":true}`)); w.Code != 200 || body["status"] != "active" {
		t.Fatalf("post again: %d %v", w.Code, body)
	}
	if items, _ := e.st.Active("", 0); len(items) != 1 || items[0].NumberOfServing != 3 {
		t.Fatalf("not in the feed again: %+v", items)
	}
	if w, _ := call(e.h, post("u_r", "/api/listings/"+l.ID+"/grab")); w.Code != 200 {
		t.Fatalf("grab %d", w.Code)
	}
	if w, body := call(e.h, patch("u_donor", l.ID, `{"title":"Other","reset":true}`)); w.Code != 409 || !strings.Contains(body["error"].(string), "cannot be changed") {
		t.Fatalf("grabbed: %d %v", w.Code, body)
	}
}

// X60: after a change, fan-out hears it once (PUT with only the giver's
// session cookie), never for a test account or a refused change.
func TestUpdateTellsFanout(t *testing.T) {
	hits := make(chan string, 4)
	fan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits <- r.Method + " " + r.URL.Path + " " + r.Header.Get("Cookie")
		w.WriteHeader(http.StatusOK)
	}))
	defer fan.Close()
	e := newEnv(t, nil)
	e.h = New(Config{Store: e.st, Auth: fakeAuth{}, ItemPhotos: e.items, TouchupPhotos: e.touch,
		TestTTLEmails: map[string]bool{"u_test@example.org": true}, Share: &ShareHook{URL: fan.URL + "/api/fanout/"}})
	l := e.create(t, "u_donor")
	tl := e.create(t, "u_test")
	select {
	case h := <-hits: // the share of the new listing
		if !strings.HasPrefix(h, "POST ") {
			t.Fatalf("hit %q", h)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no share")
	}
	if w, _ := call(e.h, patch("u_donor", l.ID, `{"title":"New name"}`)); w.Code != 200 {
		t.Fatal(w.Code)
	}
	select {
	case h := <-hits:
		if h != "PUT /api/fanout/"+l.ID+" "+sessionCookie+"=u_donor" {
			t.Fatalf("hit %q", h)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fan-out did not hear the change")
	}
	if w, _ := call(e.h, patch("u_test", tl.ID, `{"title":"Test"}`)); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w, _ := call(e.h, patch("u_donor", l.ID, `{"title":""}`)); w.Code != 400 {
		t.Fatal(w.Code)
	}
	select {
	case h := <-hits:
		t.Fatalf("an extra call %q", h)
	case <-time.After(300 * time.Millisecond):
	}
}

// X60 review: a change while the first post is still being published (fan-out
// 409 "pending") is tried again; another 409 (a newer change, not active) ends it.
func TestChangeHookWaitsForThePost(t *testing.T) {
	answers := make(chan string, 8)
	for _, a := range []string{"pending", "ok", "stale"} {
		answers <- a
	}
	fan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch <-answers {
		case "pending":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(409)
			w.Write([]byte(`{"error":"the post is being published now: retry","code":"pending"}`))
		case "stale":
			w.WriteHeader(409)
			w.Write([]byte(`{"error":"x","code":"stale"}`))
		default:
			w.WriteHeader(200)
		}
	}))
	defer fan.Close()
	done := make(chan int, 2)
	h := &ShareHook{URL: fan.URL + "/api/fanout/", Backoff: []time.Duration{time.Millisecond, time.Millisecond},
		Done: func(_ string, status int, _ error) { done <- status }}
	ck := &http.Cookie{Name: sessionCookie, Value: "u_donor"}
	h.Changed("l_0123456789abcdef", ck)
	if st := <-done; st != 200 {
		t.Fatalf("after pending: %d", st)
	}
	h.Changed("l_0123456789abcdef", ck)
	if st := <-done; st != 409 || len(answers) != 0 {
		t.Fatalf("stale: %d (%d answers left)", st, len(answers))
	}
	answers <- "pending"
	h.Share("l_0123456789abcdef", ck) // a new post: 409 is never tried again (Begin: one post)
	if st := <-done; st != 409 {
		t.Fatalf("share: %d", st)
	}
}
