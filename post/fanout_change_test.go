package post

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// changeEnv is newFO with a listing the test can change (the listings
// service after a PATCH) and a Muse that writes from the title it gets.
func changeEnv(t *testing.T) (*foEnv, []Listing, *atomic.Int32) {
	e := newFO(t, goodText)
	items := []Listing{{ID: "l_00000000000000f1", OwnerID: "u1", Title: "Apples", NumberOfServing: 8, Status: "active",
		ExpiresAt: e.now.Add(20 * time.Minute).Format(time.RFC3339Nano), PickupText: "Front desk of 12 Elm Street"}}
	ls := listingsFake(t, items)
	var textCalls atomic.Int32
	ms := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		b, _ := json.Marshal(body)
		out := `{"ok": true, "reason": "fine"}`
		if !strings.Contains(string(b), "You check posts") {
			textCalls.Add(1)
			out = `{"text": "Free apples from a neighbor! 8 servings"}`
			if strings.Contains(string(b), "Green apples") {
				out = `{"text": "Free green apples! 10 servings"}`
			}
		}
		WriteJSON(w, 200, map[string]any{"id": "resp_x",
			"output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": out}}}},
			"usage":  map[string]any{"input_tokens": 1000, "output_tokens": 100}})
	}))
	e.closes = append(e.closes, ls.Close, ms.Close)
	e.cfg.Listings = &ListingsClient{URL: ls.URL}
	e.cfg.Muse = &Muse{Base: ms.URL, Key: "key", Effort: "low"}
	if w := do(e.h, jsonReq("POST", "/api/fanout/l_00000000000000f1", "", "u1")); w.Code != 201 {
		t.Fatalf("publish %d %s", w.Code, w.Body)
	}
	return e, items, &textCalls
}

// X60 (T9 2026-11-01: "the Facebook post changes too"): a new title or
// number writes the post text again and edits the post once; a new timer
// alone does not call Muse or Facebook but moves the sweep; the same change
// again does nothing; another user gets 404.
func TestFanoutChangeEditsThePost(t *testing.T) {
	e, items, textCalls := changeEnv(t)
	defer e.stop()
	link := "\n\nClaim it in the app: https://icarus.example.org/l/l_00000000000000f1"
	if w := do(e.h, jsonReq("PUT", "/api/fanout/l_00000000000000f1", "", "u2")); w.Code != 404 {
		t.Fatalf("another user: %d", w.Code)
	}
	if w := do(e.h, jsonReq("PUT", "/api/fanout/l_00000000000000f1", "", "")); w.Code != 401 {
		t.Fatalf("no session: %d", w.Code)
	}
	items[0].Title, items[0].NumberOfServing = "Green apples", 10
	w := do(e.h, jsonReq("PUT", "/api/fanout/l_00000000000000f1", "", "u1"))
	if w.Code != 200 || textCalls.Load() != 2 {
		t.Fatalf("change: %d %s (%d text calls)", w.Code, w.Body, textCalls.Load())
	}
	last := e.graph.reqs[len(e.graph.reqs)-1]
	if len(e.graph.reqs) != 2 || last.path != "/v25.0/PAGE_123" || last.form["message"] != "Free green apples! 10 servings"+link {
		t.Fatalf("edit %+v (%d calls)", last, len(e.graph.reqs))
	}
	if w := do(e.h, jsonReq("PUT", "/api/fanout/l_00000000000000f1", "", "u1")); w.Code != 200 || len(e.graph.reqs) != 2 || textCalls.Load() != 2 {
		t.Fatalf("the same change again: %d, %d graph calls, %d text calls", w.Code, len(e.graph.reqs), textCalls.Load())
	}
	// a new timer only: no Muse, no Facebook; the sweep uses the new time
	items[0].ExpiresAt = e.now.Add(50 * time.Minute).Format(time.RFC3339Nano)
	if w := do(e.h, jsonReq("PUT", "/api/fanout/l_00000000000000f1", "", "u1")); w.Code != 200 || len(e.graph.reqs) != 2 || textCalls.Load() != 2 {
		t.Fatalf("timer: %d, %d graph calls", w.Code, len(e.graph.reqs))
	}
	later := e.now.Add(40 * time.Minute)
	e.cfg.now = func() time.Time { return later }
	if n := e.cfg.Sweep(t.Context()); n != 0 {
		t.Fatal("the sweep used the old timer")
	}
	b, _ := os.ReadFile(e.cfg.Events.Path(e.now))
	if !strings.Contains(string(b), `"route":"fanout.change"`) {
		t.Fatalf("events %s", b)
	}
}

// X60 ("post again"): a post marked [UNAVAILABLE] loses the banner when the
// listing shows again; a claimed post never changes; an ended listing and a
// post being published now are 409 (nothing reaches Facebook).
func TestFanoutChangeAfterPostAgainAndClaim(t *testing.T) {
	e, items, textCalls := changeEnv(t)
	defer e.stop()
	link := "\n\nClaim it in the app: https://icarus.example.org/l/l_00000000000000f1"
	later := e.now.Add(time.Hour)
	e.cfg.now = func() time.Time { return later }
	if n := e.cfg.Sweep(t.Context()); n != 1 {
		t.Fatalf("sweep %d", n)
	}
	if w := do(e.h, jsonReq("PUT", "/api/fanout/l_00000000000000f1", "", "u1")); w.Code != 409 || len(e.graph.reqs) != 2 {
		t.Fatalf("ended listing: %d, %d graph calls", w.Code, len(e.graph.reqs))
	}
	items[0].ExpiresAt = later.Add(30 * time.Minute).Format(time.RFC3339Nano) // posted again
	if w := do(e.h, jsonReq("PUT", "/api/fanout/l_00000000000000f1", "", "u1")); w.Code != 200 {
		t.Fatalf("post again: %d %s", w.Code, w.Body)
	}
	last := e.graph.reqs[len(e.graph.reqs)-1]
	if len(e.graph.reqs) != 3 || last.form["message"] != "Free apples from a neighbor! 8 servings"+link {
		t.Fatalf("edit %+v (%d calls)", last, len(e.graph.reqs))
	}
	if p, _ := e.cfg.Store.Get("l_00000000000000f1"); p.Status != PostPosted {
		t.Fatalf("stored %+v", p)
	}
	if w := do(e.h, statusReq(`{"listing_id":"l_00000000000000f1","status":"claimed"}`, "tok")); w.Code != 200 {
		t.Fatalf("claim %d", w.Code)
	}
	items[0].Title = "Green apples"
	before := textCalls.Load()
	if w := do(e.h, jsonReq("PUT", "/api/fanout/l_00000000000000f1", "", "u1")); w.Code != 409 || len(e.graph.reqs) != 4 || textCalls.Load() != before {
		t.Fatalf("claimed: %d, %d graph calls", w.Code, len(e.graph.reqs))
	}
	if p, _ := e.cfg.Store.Get("l_00000000000000f1"); p.Status != PostClaimed {
		t.Fatalf("claimed post changed: %+v", p)
	}
}

// X60 review: an older change that finishes after a newer one changes
// nothing (the listing has other words now); the sweep does not mark a post
// whose timer was reset after Due read it; the delete hook ("expired" before
// the end) still marks it.
func TestFanoutChangeStaleAndSweepAfterReset(t *testing.T) {
	e, items, _ := changeEnv(t)
	defer e.stop()
	old := items[0]
	items[0].Title = "Green apples" // a newer change is in the listing now
	_, edited, err := e.cfg.applyChange(t.Context(), old, "Old text", func() (Listing, error) { return items[0], nil })
	if !errors.Is(err, errStale) || edited || len(e.graph.reqs) != 1 {
		t.Fatalf("stale: %v %t, %d graph calls", err, edited, len(e.graph.reqs))
	}
	if p, _ := e.cfg.Store.Get(old.ID); p.Text != "Free apples from a neighbor! 8 servings" {
		t.Fatalf("stored %+v", p)
	}
	// the post's timer is in the future (a reset): the sweep path changes nothing
	if _, changed, err := e.cfg.setStatus(t.Context(), old.ID, PostExpired, true); changed || err != nil {
		t.Fatalf("sweep after reset: %t %v", changed, err)
	}
	if _, changed, err := e.cfg.SetStatus(t.Context(), old.ID, PostExpired); !changed || err != nil {
		t.Fatalf("delete hook: %t %v", changed, err)
	}
}
