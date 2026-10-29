package inference

// X44b: the scan as a job (?async=1, GET .../scan/{id}).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// poll GETs the job (auth as in req) until it is not running.
func (s *svc) poll(t *testing.T, path, auth string) (*httptest.ResponseRecorder, ScanJob) {
	t.Helper()
	for i := 0; i < 500; i++ {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		switch auth {
		case "bearer":
			r.Header.Set("Authorization", "Bearer tok")
		case "":
		default:
			r.AddCookie(&http.Cookie{Name: SessionCookie, Value: auth})
		}
		w := httptest.NewRecorder()
		s.h.ServeHTTP(w, r)
		var j ScanJob
		_ = json.Unmarshal(w.Body.Bytes(), &j)
		if w.Code != 200 || j.Status != ScanRunning {
			return w, j
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the scan job did not end")
	return nil, ScanJob{}
}

func startScan(t *testing.T, s *svc, r *http.Request) string {
	t.Helper()
	w, a := s.do(r)
	if w.Code != http.StatusAccepted || keys(a) != "request_id,scan_id,status" || a["status"] != ScanRunning {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	id := a["scan_id"].(string)
	if !PhotoIDPattern.MatchString(id) || id == a["request_id"] {
		t.Fatalf("scan_id %q", id)
	}
	return id
}

// The job gives the 200 route's answer, the boxes as soon as SAM is done and
// the names of each chunk; one events line (async), written when it ends.
func TestAnalyzeAsyncJob(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, nil)
	id := startScan(t, s, req(t, "/v1/analyze?async=1", "bearer", testPhoto(t), ""))
	w, j := s.poll(t, "/v1/analyze/scan/"+id, "bearer")
	if w.Code != 200 || j.Status != ScanDone || j.HTTPStatus != 200 || j.Stage != "done" || j.ScanID != id {
		t.Fatalf("job: %d %s", w.Code, w.Body)
	}
	var a map[string]any
	if err := json.Unmarshal(j.Answer, &a); err != nil || keys(a) != "count,items,latency_ms,model,request_id,source" ||
		a["count"] != 2.0 || a["request_id"] != j.RequestID {
		t.Fatalf("answer: %s", j.Answer)
	}
	if len(j.Boxes) != 3 || j.Boxes[0] == (Norm{}) {
		t.Fatalf("boxes: %+v", j.Boxes)
	}
	if len(j.Named) != 3 || j.Named[0] != (NamedBox{1, "Soup"}) || j.Named[2] != (NamedBox{3, "Beans"}) {
		t.Fatalf("named: %+v", j.Named)
	}
	ev := s.events(t)
	if len(ev) != 1 || !ev[0].Async || ev[0].Status != 200 || ev[0].Items != 2 || ev[0].RequestID != j.RequestID {
		t.Fatalf("events: %+v", ev)
	}
	// the same scan without async: the same answer fields
	w2, a2 := s.do(req(t, "/v1/analyze", "bearer", testPhoto(t), ""))
	if w2.Code != 200 || keys(a2) != keys(a) || a2["count"] != a["count"] {
		t.Fatalf("sync: %d %s", w2.Code, w2.Body)
	}
}

// Only the caller that started the job sees it: the same kind and the same user.
func TestAnalyzeAsyncOwnerOnly(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, nil)
	id := startScan(t, s, req(t, "/api/analyze?async=1", "u1", testPhoto(t), ""))
	if w, j := s.poll(t, "/api/analyze/scan/"+id, "u1"); w.Code != 200 || j.Status != ScanDone {
		t.Fatalf("owner: %d %s", w.Code, w.Body)
	}
	for name, c := range map[string]struct {
		path, auth string
		want       int
	}{
		"another user":  {"/api/analyze/scan/" + id, "u2", 404},
		"a bearer":      {"/v1/analyze/scan/" + id, "bearer", 404},
		"no caller":     {"/api/analyze/scan/" + id, "", 401},
		"an unknown id": {"/api/analyze/scan/" + newPhotoID(), "u1", 404},
	} {
		if w, _ := s.poll(t, c.path, c.auth); w.Code != c.want {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
}

// A refusal after the 202 is the job's answer (failed + the error body); a
// refusal before the photo is read stays a plain answer, with no job.
func TestAnalyzeAsyncRefusals(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "NOT_FOOD", 0, nil)
	id := startScan(t, s, req(t, "/v1/analyze?async=1", "bearer", testPhoto(t), ""))
	_, j := s.poll(t, "/v1/analyze/scan/"+id, "bearer")
	var a map[string]any
	_ = json.Unmarshal(j.Answer, &a)
	if j.Status != ScanFailed || j.HTTPStatus != 422 || a["code"] != CodeNotFood || museBodies(s.api) != 0 || len(j.Boxes) != 0 {
		t.Fatalf("not food: %+v %s", j, j.Answer)
	}
	w, b := s.do(req(t, "/v1/analyze?async=1", "", testPhoto(t), ""))
	errorIs(t, w, b, 401, CodeUnauthorized)
}

// The barcode form field reaches the job (the request is cloned with its
// values), so the fast path works there too.
func TestAnalyzeAsyncBarcode(t *testing.T) {
	s := newSvc(t, threeBoxes(t), "FOOD", 0, nil)
	id := startScan(t, s, req(t, "/v1/analyze?async=1", "bearer", testPhoto(t), "078742370026"))
	_, j := s.poll(t, "/v1/analyze/scan/"+id, "bearer")
	var a map[string]any
	_ = json.Unmarshal(j.Answer, &a)
	if j.Status != ScanDone || a["source"] != SourceBarcode || s.gate != 0 {
		t.Fatalf("barcode job: %+v %s", j, j.Answer)
	}
}

// The store holds ScanMaxJobs; a finished job makes room (the oldest first),
// one older than ScanKeep is gone; 64 running jobs: the next is refused.
func TestScanJobsLimit(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	s := &scanJobs{now: func() time.Time { return now }}
	var first *ScanJob
	for i := 0; i < ScanMaxJobs; i++ {
		j := s.start(newPhotoID(), "r", "bearer", "")
		if j == nil {
			t.Fatal("refused under the limit", i)
		}
		if i == 0 {
			first = j
		}
	}
	if s.start(newPhotoID(), "r", "bearer", "") != nil {
		t.Fatal("65 running jobs")
	}
	s.finish(first, 200, []byte(`{"ok":true}`))
	if s.start(newPhotoID(), "r", "bearer", "") == nil {
		t.Fatal("a finished job did not make room")
	}
	if _, ok := s.get(first.ScanID, "bearer", ""); ok {
		t.Fatal("the finished job was not the one dropped")
	}
	if s.start(newPhotoID(), "r", "x", "") != nil { // full again (64 running): refused
		t.Fatal("full store took a job")
	}
	// a finished job is kept ScanKeep, then dropped
	k := &scanJobs{now: func() time.Time { return now }}
	old := k.start(newPhotoID(), "r", "bearer", "")
	k.finish(old, 422, []byte(`{"code":"not_food"}`))
	if g, ok := k.get(old.ScanID, "bearer", ""); !ok || g.Status != ScanFailed || g.HTTPStatus != 422 {
		t.Fatalf("kept: %+v %v", g, ok)
	}
	now = now.Add(ScanKeep + time.Minute)
	k.start(newPhotoID(), "r", "bearer", "")
	if _, ok := k.get(old.ScanID, "bearer", ""); ok {
		t.Fatal("a job older than ScanKeep is still there")
	}
}

// The engine reports the boxes once SAM is done, before any name, then the
// names of each chunk as that chunk ends (Split 3: three reports).
func TestEngineProgress(t *testing.T) {
	e, done := engine(t, threeBoxes(t))
	defer done()
	e.Marks, e.Split, e.SplitMin = true, 3, 1
	var mu sync.Mutex
	var got []Progress
	ctx := WithProgress(context.Background(), func(p Progress) { mu.Lock(); got = append(got, p); mu.Unlock() })
	inv, err := e.Run(ctx, testPhoto(t), "image/jpeg", ModeSingle)
	if err != nil || len(inv.Items) == 0 {
		t.Fatal(err)
	}
	if len(got) != 4 || got[0].Stage != "naming" || len(got[0].Boxes) != 3 || got[0].Named != nil {
		t.Fatalf("reports: %+v", got)
	}
	named := map[int]bool{}
	for _, p := range got[1:] {
		if p.Boxes != nil || len(p.Named) != 1 {
			t.Fatalf("chunk report: %+v", p)
		}
		for n := range p.Named {
			named[n] = true
		}
	}
	if len(named) != 3 {
		t.Fatalf("named boxes: %v", named)
	}
	// no context value: no report, no panic
	if _, err := e.Run(context.Background(), testPhoto(t), "image/jpeg", ModeSingle); err != nil {
		t.Fatal(err)
	}
}
