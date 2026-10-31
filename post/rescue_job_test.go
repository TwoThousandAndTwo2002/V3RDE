package post

// R4 tests: the rescue job (?async=1), and the web search agent when the
// directory finds nothing or fails.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const webPlaces = `{"places": [
 {"name": "Hope\nShelter", "type": "shelter", "address": "5 Pine Rd, Denver, CO 80219", "phone": "303-555-0105",
  "website": "http://hope.example.org", "source_url": "https://hope.example.org/meals"},
 {"name": "Westside Pantry", "type": "food_pantry", "address": "6 Oak Ave, Denver, CO 80219", "source_url": "https://west.example.org"},
 {"name": "No Source Kitchen", "type": "soup_kitchen", "address": "7 Elm St, Denver, CO 80219"},
 {"name": "Bad Link", "type": "food_bank", "address": "8 Ash St", "source_url": "javascript:alert(1)"},
 {"name": "Odd Type", "type": "restaurant", "address": "9 Fir St, Denver, CO 80219", "source_url": "https://odd.example.org"}
]}`

// museTools is a Muse fake: a request with tools is the web search (its
// answer has two web_search_call items); any other is the draft.
type museTools struct {
	gate     chan struct{} // when set, the draft call waits for it
	mu       sync.Mutex
	draft    string
	search   string
	searchSt int
	bodies   []map[string]any
}

func (m *museTools) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, tools := body["tools"]; !tools && m.gate != nil {
			<-m.gate
		}
		m.mu.Lock()
		m.bodies = append(m.bodies, body)
		m.mu.Unlock()
		text, out := m.draft, []any{}
		if _, ok := body["tools"]; ok {
			if m.searchSt != 0 {
				w.WriteHeader(m.searchSt)
				return
			}
			text = m.search
			out = append(out, map[string]any{"type": "web_search_call", "status": "completed"},
				map[string]any{"type": "web_search_call", "status": "completed"})
		}
		out = append(out, map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": text}}})
		WriteJSON(w, 200, map[string]any{"id": "resp_1", "output": out,
			"usage": map[string]any{"input_tokens": 1000, "output_tokens": 100, "input_tokens_details": map[string]any{"cached_tokens": 0}}})
	}))
}

func (m *museTools) searchPrompts() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, b := range m.bodies {
		if _, ok := b["tools"]; ok {
			out = append(out, b["input"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string))
		}
	}
	return out
}

// jobEnv is newRS with the tools fake and a directory that answers dirBody
// (or dirStatus).
func jobEnv(t *testing.T, m *museTools, dirStatus int, dirBody string) *rsEnv {
	e := newRS(t, stableDraft, 200, true)
	ms := m.server()
	ds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if dirStatus != 0 {
			w.WriteHeader(dirStatus)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(dirBody))
	}))
	e.closes = append(e.closes, ms.Close, ds.Close)
	e.cfg.Muse = &Muse{Base: ms.URL, Key: "key", Effort: "low"}
	e.cfg.Search = &PlaceSearch{Muse: &Muse{Base: ms.URL, Key: "key"}}
	e.cfg.Directory = &Directory{Base: ds.URL}
	e.cfg.MinServings = 30
	e.cfg.JobTimeout = 5 * time.Second
	return e
}

// waitRescue polls GET /api/rescue/{id} until the job is not running.
func waitRescue(t *testing.T, e *rsEnv, id string) Rescue {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w := do(e.h, jsonReq("GET", "/api/rescue/"+id, "", "u1"))
		var res Rescue
		json.Unmarshal(w.Body.Bytes(), &res)
		if w.Code != 200 {
			t.Fatalf("poll %d %s", w.Code, w.Body)
		}
		if res.Status != StatusRunning {
			return res
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the job did not end")
	return Rescue{}
}

func startRescue(t *testing.T, e *rsEnv, lid string) Rescue {
	w := do(e.h, jsonReq("POST", "/api/rescue/"+lid+"?async=1", ``, "u1"))
	var res Rescue
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 202 || res.Status != StatusRunning || res.RescueID == "" || len(res.Places) != 0 {
		t.Fatalf("start %d %s", w.Code, w.Body)
	}
	return res
}

func rescueEvents(t *testing.T, e *rsEnv) string {
	b, _ := os.ReadFile(e.cfg.Events.Path(e.now))
	return string(b)
}

// The directory finds places: the job ends done with them; no web search.
func TestRescueJobDirectory(t *testing.T) {
	m := &museTools{draft: `{"shelf_stable": true, "reason": "sealed"}`, search: webPlaces}
	e := jobEnv(t, m, 0, dirAnswer)
	defer e.stop()
	run := startRescue(t, e, "l_0000000000000011")
	if w := do(e.h, jsonReq("GET", "/api/rescue/"+run.RescueID, "", "u2")); w.Code != 404 {
		t.Fatalf("another user read the job: %d", w.Code)
	}
	res := waitRescue(t, e, run.RescueID)
	if res.Status != StatusDone || res.FoundBy != "directory" || res.Mode != ModeDropoff || len(res.Places) != 3 ||
		res.Attribution != DirectoryAttribution || res.RescueID != run.RescueID {
		t.Fatalf("%+v", res)
	}
	if len(m.searchPrompts()) != 0 {
		t.Fatal("a web search ran although the directory found places")
	}
	ev := rescueEvents(t, e)
	if !strings.Contains(ev, `"async":true`) || !strings.Contains(ev, `"found_by":"directory"`) || strings.Contains(ev, "Main St") {
		t.Fatalf("events %s", ev)
	}
}

// The directory finds nothing: the agent searches near the pickup ZIP, and
// its checked places are the answer, marked as found by a web search.
func TestRescueJobWebSearchWhenNothingFound(t *testing.T) {
	m := &museTools{draft: `{"shelf_stable": false, "reason": "fresh bagels"}`, search: webPlaces}
	e := jobEnv(t, m, 0, `{"success":true,"count":0,"resources":[]}`)
	defer e.stop()
	res := waitRescue(t, e, startRescue(t, e, "l_0000000000000011").RescueID)
	if res.Status != StatusDone || res.FoundBy != SourceWebSearch || res.Attribution != WebAttribution || res.Kind != KindMealProgram {
		t.Fatalf("%+v", res)
	}
	// 3 of the 5 pass the checks (no source_url, a javascript: link); the shelter goes first (perishable)
	if len(res.Places) != 3 || res.Places[0].Name != "Hope Shelter" || res.Places[0].Website != "" ||
		res.Places[0].Source != SourceWebSearch || res.Places[0].SourceURL != "https://hope.example.org/meals" ||
		!strings.Contains(res.Places[0].Directions, "origin=12+Main+St") || res.Places[0].Mailto != "" {
		t.Fatalf("places %+v", res.Places)
	}
	ps := m.searchPrompts()
	if len(ps) != 1 || !strings.Contains(ps[0], "near ZIP 80219") || !strings.Contains(ps[0], "found no places") ||
		strings.Contains(ps[0], "Main St") || strings.Contains(ps[0], "front desk") {
		t.Fatalf("search prompt %q", ps)
	}
	// the two searches are priced: 2 x $0.0025 on top of the tokens
	if res.CostUSD < 2*PriceWebSearch {
		t.Fatalf("cost %v", res.CostUSD)
	}
	if ev := rescueEvents(t, e); !strings.Contains(ev, `"web_search":true`) || !strings.Contains(ev, `"web_searches":2`) {
		t.Fatalf("events %s", ev)
	}
}

// The directory fails: its failure seeds the search, and the web places are
// the answer.
func TestRescueJobWebSearchWhenDirectoryFails(t *testing.T) {
	m := &museTools{draft: `{"shelf_stable": true, "reason": "cans"}`, search: webPlaces}
	e := jobEnv(t, m, 500, "")
	defer e.stop()
	res := waitRescue(t, e, startRescue(t, e, "l_0000000000000011").RescueID)
	if res.Status != StatusDone || res.FoundBy != SourceWebSearch || len(res.Places) != 3 || res.Places[0].Name != "Westside Pantry" {
		t.Fatalf("%+v", res)
	}
	if ps := m.searchPrompts(); len(ps) != 1 || !strings.Contains(ps[0], "did not answer (directory: HTTP 500") {
		t.Fatalf("search prompt %q", ps)
	}
	if ev := rescueEvents(t, e); !strings.Contains(ev, `"directory_error"`) {
		t.Fatalf("events %s", ev)
	}
}

// The directory and the search both fail: the job ends failed (502 words),
// and a new POST may run again.
func TestRescueJobFailsAndCanRunAgain(t *testing.T) {
	m := &museTools{draft: `{"shelf_stable": true, "reason": "cans"}`, searchSt: 500}
	e := jobEnv(t, m, 500, "")
	defer e.stop()
	first := waitRescue(t, e, startRescue(t, e, "l_0000000000000011").RescueID)
	if first.Status != StatusFailed || first.Code != CodeUpstream || !strings.Contains(first.Error, "web search") {
		t.Fatalf("%+v", first)
	}
	m.mu.Lock()
	m.searchSt, m.search = 0, webPlaces
	m.mu.Unlock()
	second := waitRescue(t, e, startRescue(t, e, "l_0000000000000011").RescueID)
	if second.Status != StatusDone || second.RescueID == first.RescueID {
		t.Fatalf("%+v", second)
	}
	// now the listing has a rescue: a new POST gets it (no new call)
	w := do(e.h, jsonReq("POST", "/api/rescue/l_0000000000000011?async=1", ``, "u1"))
	if w.Code != 200 || !strings.Contains(w.Body.String(), second.RescueID) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

// The search finds nothing too: done, no places, found_by none.
func TestRescueJobNothingAnywhere(t *testing.T) {
	m := &museTools{draft: `{"shelf_stable": true, "reason": "cans"}`, search: `{"places": []}`}
	e := jobEnv(t, m, 0, `{"success":true,"resources":[]}`)
	defer e.stop()
	res := waitRescue(t, e, startRescue(t, e, "l_0000000000000011").RescueID)
	if res.Status != StatusDone || res.FoundBy != "none" || len(res.Places) != 0 {
		t.Fatalf("%+v", res)
	}
}

// The one-request route (no ?async=1) keeps X35b: no web search, even when the
// directory finds nothing, and no status field.
func TestRescueSyncRouteHasNoWebSearch(t *testing.T) {
	m := &museTools{draft: `{"shelf_stable": true, "reason": "cans"}`, search: webPlaces}
	e := jobEnv(t, m, 0, `{"success":true,"resources":[]}`)
	defer e.stop()
	w := do(e.h, jsonReq("POST", "/api/rescue/l_0000000000000011", ``, "u1"))
	if w.Code != 200 || strings.Contains(w.Body.String(), `"status"`) || !strings.Contains(w.Body.String(), `"places":[]`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if len(m.searchPrompts()) != 0 {
		t.Fatal("the one-request route ran a web search")
	}
}

// A job that was running when the service stopped is failed on the next start.
func TestRescueStoreFailsInterruptedJobs(t *testing.T) {
	d := t.TempDir()
	st, _ := OpenRescues(d)
	st.Put(Rescue{RescueID: "r1", OwnerID: "u1", ListingID: "l_000000000000000a", Status: StatusRunning})
	st.Put(Rescue{RescueID: "r2", OwnerID: "u1", ListingID: "l_000000000000000b"})
	st2, err := OpenRescues(d)
	if err != nil {
		t.Fatal(err)
	}
	r1, _ := st2.Get("r1", "u1")
	r2, _ := st2.Get("r2", "u1")
	if r1.Status != StatusFailed || r1.Code != "interrupted" || r2.Status != "" {
		t.Fatalf("%+v %+v", r1, r2)
	}
	if _, found, _ := st2.Begin("l_000000000000000a", "u1"); found {
		t.Fatal("a failed job blocks a new rescue")
	}
}

func TestSearchPlacesChecks(t *testing.T) {
	var obj map[string]any
	json.Unmarshal([]byte(webPlaces), &obj)
	ps := SearchPlaces(obj)
	if len(ps) != 3 || ps[0].ID != "w1" || ps[0].Name != "Hope Shelter" || ps[0].Website != "" || ps[2].Type != "" {
		t.Fatalf("%+v", ps)
	}
	many := `{"places": [` + strings.TrimSuffix(strings.Repeat(`{"name":"P","address":"1 A St","source_url":"https://p.example.org"},`, 9), ",") + `]}`
	json.Unmarshal([]byte(many), &obj)
	if len(SearchPlaces(obj)) != MaxSearchPlaces {
		t.Fatal("more than 6 places")
	}
	for _, u := range []string{"http://x.example.org", "https://", "https://user:pw@x.example.org", "ftp://x", "/relative"} {
		if httpsURL(u) != "" {
			t.Fatalf("%q kept", u)
		}
	}
	if SearchPlaces(map[string]any{"places": "nope"}) != nil {
		t.Fatal("a bad shape gave places")
	}
}

// AskJSONWith sends the tools and prices each web_search_call.
func TestMuseAskJSONWithTools(t *testing.T) {
	m := &museTools{search: `{"places": []}`}
	srv := m.server()
	defer srv.Close()
	mu := &Muse{Base: srv.URL, Key: "key"}
	_, call, err := mu.AskJSONWith(t.Context(), "x", "find", []map[string]any{{"type": "web_search"}})
	if err != nil || call.WebSearches != 2 {
		t.Fatalf("%v %+v", err, call)
	}
	tools, _ := m.bodies[0]["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["type"] != "web_search" {
		t.Fatalf("tools %v", m.bodies[0]["tools"])
	}
	want := (1000*PriceMuseInputPerM+100*PriceMuseOutputPerM)/1e6 + 2*PriceWebSearch
	if d := call.CostUSD - want; d > 1e-12 || d < -1e-12 {
		t.Fatalf("cost %v want %v", call.CostUSD, want)
	}
	_, plain, _ := mu.AskJSON(t.Context(), "y", "draft")
	if _, ok := m.bodies[1]["tools"]; ok || plain.WebSearches != 0 {
		t.Fatal("AskJSON sent tools")
	}
}

// R4 review: the final save fails (the file cannot be written): the job is
// failed in memory, not "running", and a new POST may run again.
func TestRescueJobSaveFailureIsNotRunning(t *testing.T) {
	m := &museTools{draft: `{"shelf_stable": true, "reason": "cans"}`, search: webPlaces, gate: make(chan struct{})}
	e := jobEnv(t, m, 0, dirAnswer)
	defer e.stop()
	d := t.TempDir()
	st, _ := OpenRescues(d)
	e.cfg.Store = st
	run := startRescue(t, e, "l_0000000000000011")
	// the file becomes a folder: the rename of the next write fails
	os.Remove(d + "/rescues.json")
	os.Mkdir(d+"/rescues.json", 0o700)
	close(m.gate)
	res := waitRescue(t, e, run.RescueID)
	if res.Status != StatusFailed || res.Code != CodeInternal {
		t.Fatalf("%+v", res)
	}
	if _, found, busy := st.Begin("l_0000000000000011", "u1"); found || busy {
		t.Fatal("the failed job blocks a new rescue")
	}
}

// R4 review: the one-request route keeps the X35b shape (no found_by), and
// answers 409 while a job for the listing runs.
func TestRescueSyncRouteShapeAndRunningJob(t *testing.T) {
	m := &museTools{draft: `{"shelf_stable": true, "reason": "cans"}`, search: webPlaces, gate: make(chan struct{})}
	e := jobEnv(t, m, 0, dirAnswer)
	defer e.stop()
	run := startRescue(t, e, "l_0000000000000011")
	if w := do(e.h, jsonReq("POST", "/api/rescue/l_0000000000000011", ``, "u1")); w.Code != 409 {
		t.Fatalf("sync while a job runs: %d %s", w.Code, w.Body)
	}
	if w := do(e.h, jsonReq("POST", "/api/rescue/l_0000000000000011?async=1", ``, "u1")); w.Code != 200 ||
		!strings.Contains(w.Body.String(), `"status":"running"`) {
		t.Fatalf("async while a job runs: %d %s", w.Code, w.Body)
	}
	close(m.gate)
	waitRescue(t, e, run.RescueID)
	m.gate = nil
	w := do(e.h, jsonReq("POST", "/api/rescue/l_000000000000000a", `{"zip":"10001"}`, "u1"))
	if w.Code != 200 || strings.Contains(w.Body.String(), `"found_by"`) || strings.Contains(w.Body.String(), `"status"`) {
		t.Fatalf("sync shape: %d %s", w.Code, w.Body)
	}
}
