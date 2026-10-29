package post

// X44c: every photo of a scan in one call, with a queue.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

type batchAnswer struct {
	Jobs []struct {
		PhotoID   string `json:"photo_id"`
		TouchupID string `json:"touchup_id"`
		Status    string `json:"status"`
	} `json:"jobs"`
	Skipped []struct {
		PhotoID string `json:"photo_id"`
		Code    string `json:"code"`
	} `json:"skipped"`
	Code string `json:"code"`
}

func batchReq(path, user string, ids ...string) *http.Request {
	r := httptest.NewRequest("POST", path+"?mode=light&photo_ids="+strings.Join(ids, ","), nil)
	if user != "" {
		r = asUser(r, user)
	}
	return r
}

func doBatch(t *testing.T, e *tuEnv, r *http.Request, want int) batchAnswer {
	t.Helper()
	w := do(e.h, r)
	if w.Code != want {
		t.Fatalf("batch: %d %s (want %d)", w.Code, w.Body, want)
	}
	var a batchAnswer
	_ = json.Unmarshal(w.Body.Bytes(), &a)
	return a
}

// waitEnd polls until the job is neither queued nor running.
func waitEnd(t *testing.T, e *tuEnv, path, user string, auth func(*http.Request) *http.Request) Job {
	t.Helper()
	for i := 0; i < 400; i++ {
		r := httptest.NewRequest("GET", path, nil)
		if auth != nil {
			r = auth(r)
		} else {
			r = asUser(r, user)
		}
		w := do(e.h, r)
		var j Job
		_ = json.Unmarshal(w.Body.Bytes(), &j)
		if w.Code != 200 {
			t.Fatalf("GET %s: %d %s", path, w.Code, w.Body)
		}
		if j.Status != JobQueued && j.Status != JobRunning {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not end")
	return Job{}
}

// Three photos, two slots: every job is done (the third waited in the queue,
// not 503 busy); the rate limit counts the call once.
func TestTouchupBatchQueues(t *testing.T) {
	e := newTU(t, &metaFake{checkText: `{"same_food": true, "reason": "same"}`}, true)
	defer e.stop()
	e.cfg.Limiter = NewLimiter(1, time.Minute)
	e.h = NewTouchup(e.cfg)
	ids := []string{seed(t, e, "u1"), seed(t, e, "u1"), seed(t, e, "u1")}
	a := doBatch(t, e, batchReq("/api/touchup/batch", "u1", ids...), 202)
	if len(a.Jobs) != 3 || len(a.Skipped) != 0 || a.Jobs[2].PhotoID != ids[2] || a.Jobs[0].Status != JobQueued {
		t.Fatalf("answer %+v", a)
	}
	for _, j := range a.Jobs {
		if got := waitEnd(t, e, "/api/touchup/"+j.TouchupID, "u1", nil); got.Status != JobDone || got.SourcePhotoID != j.PhotoID {
			t.Fatalf("job %+v", got)
		}
	}
	for i := 0; i < 100 && len(e.cfg.slots) != 0; i++ { // a job frees its slot just after its status is set
		time.Sleep(5 * time.Millisecond)
	}
	if e.cfg.queuedNow() != 0 || len(e.cfg.slots) != 0 {
		t.Fatal("queue or slots not empty", e.cfg.queuedNow(), len(e.cfg.slots))
	}
	if w := do(e.h, tuReq(t, e, "u1", "")); w.Code != 429 { // the batch was one hit of the 1/min limit
		t.Fatalf("rate limit after one batch: %d", w.Code)
	}
}

// Refusals before any job: no caller, too many or repeated ids, a photo of
// another user (the whole call, one 404), a body.
func TestTouchupBatchRefusals(t *testing.T) {
	e := newTU(t, &metaFake{checkText: `{"same_food": true}`}, true)
	defer e.stop()
	mine, theirs := seed(t, e, "u1"), seed(t, e, "u2")
	doBatch(t, e, batchReq("/api/touchup/batch", "", mine), 401)
	seven := []string{}
	for i := 0; i < MaxBatch+1; i++ {
		seven = append(seven, seed(t, e, "u1"))
	}
	doBatch(t, e, batchReq("/api/touchup/batch", "u1", seven...), 400)
	doBatch(t, e, batchReq("/api/touchup/batch", "u1", mine, mine), 400)
	doBatch(t, e, batchReq("/api/touchup/batch", "u1"), 400)
	doBatch(t, e, batchReq("/api/touchup/batch", "u1", mine, theirs), 404)
	body := asUser(httptest.NewRequest("POST", "/api/touchup/batch?photo_ids="+mine, strings.NewReader("x")), "u1")
	doBatch(t, e, body, 400)
	if len(e.cfg.jobs) != 0 || e.fake.edited() {
		t.Fatal("a refused batch made a job or a paid call")
	}
}

// The caps count every job: over the caller's share, the rest is skipped
// with its code; when nothing can start, the answer is the first refusal.
func TestTouchupBatchCaps(t *testing.T) {
	e := newTU(t, &metaFake{checkText: `{"same_food": true}`}, true)
	defer e.stop()
	e.cfg.Budget.PerCaller = 0.04 // one 0.03 hold fits
	ids := []string{seed(t, e, "u1"), seed(t, e, "u1"), seed(t, e, "u1")}
	a := doBatch(t, e, batchReq("/api/touchup/batch", "u1", ids...), 202)
	if len(a.Jobs) != 1 || len(a.Skipped) != 2 || a.Skipped[0].Code != CodeCallerCap || a.Skipped[1].PhotoID != ids[2] {
		t.Fatalf("answer %+v", a)
	}
	waitEnd(t, e, "/api/touchup/"+a.Jobs[0].TouchupID, "u1", nil)
	b := doBatch(t, e, batchReq("/api/touchup/batch", "u1", seed(t, e, "u1")), 429)
	if b.Code != CodeCallerCap || len(b.Skipped) != 1 {
		t.Fatalf("nothing started: %+v", b)
	}
}

// The queue has a size and a time limit: over MaxQueued a job is skipped
// (busy); a job that waits longer than QueueWait fails and pays nothing.
func TestTouchupBatchQueueLimits(t *testing.T) {
	e := newTU(t, &metaFake{checkText: `{"same_food": true}`}, true)
	defer e.stop()
	e.cfg.MaxQueued, e.cfg.QueueWait = 1, 50*time.Millisecond
	e.h = NewTouchup(e.cfg)
	for i := 0; i < cap(e.cfg.slots); i++ { // every slot is taken
		e.cfg.slots <- struct{}{}
	}
	a := doBatch(t, e, batchReq("/api/touchup/batch", "u1", seed(t, e, "u1"), seed(t, e, "u1")), 202)
	if len(a.Jobs) != 1 || len(a.Skipped) != 1 || a.Skipped[0].Code != "busy" {
		t.Fatalf("answer %+v", a)
	}
	j := waitEnd(t, e, "/api/touchup/"+a.Jobs[0].TouchupID, "u1", nil)
	if j.Status != JobFailed || !strings.Contains(j.Reason, "waited") || j.CostUSD != 0 {
		t.Fatalf("job %+v", j)
	}
	if e.cfg.Budget.Spent() != 0 || e.cfg.queuedNow() != 0 || e.fake.edited() {
		t.Fatal("a job that never ran paid or stayed queued")
	}
	if h, ok := e.cfg.Budget.Reserve(); !ok || h == nil {
		t.Fatal("the hold of the failed job was not freed")
	}
}

// /v1/touchup/batch takes the token and the bearer photos only (X42e rules).
func TestTouchupBatchBearer(t *testing.T) {
	e := newTU(t, &metaFake{checkText: `{"same_food": true}`}, true)
	defer e.stop()
	e.cfg.Auth = analyze.NewBearer("tok")
	e.h = NewTouchup(e.cfg)
	bearer := func(r *http.Request) *http.Request { r.Header.Set("Authorization", "Bearer tok"); return r }
	bp, up := seed(t, e, ""), seed(t, e, "u1")
	a := doBatch(t, e, bearer(batchReq("/v1/touchup/batch", "", bp)), 202)
	if j := waitEnd(t, e, "/v1/touchup/"+a.Jobs[0].TouchupID, "", bearer); j.Status != JobDone {
		t.Fatalf("bearer job %+v", j)
	}
	doBatch(t, e, bearer(batchReq("/v1/touchup/batch", "", up)), 404)
	doBatch(t, e, batchReq("/v1/touchup/batch", "u1", bp), 401)
}

// edited reports whether the paid edit call was made.
func (f *metaFake) edited() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.editForm != nil
}
