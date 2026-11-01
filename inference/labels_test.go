package inference

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// X32g: the app's report on an answer is logged next to it (same
// request_id), bounded and checked; it needs a caller.
func TestLabelsAreLoggedAndBounded(t *testing.T) {
	s := newSvc(t, oneBox(t, "Romanesco"), "FOOD", 0, nil)
	post := func(path, who, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if who == "bearer" {
			r.Header.Set("Authorization", "Bearer tok")
		} else if who != "" {
			r.AddCookie(&http.Cookie{Name: SessionCookie, Value: who})
		}
		w := httptest.NewRecorder()
		s.h.ServeHTTP(w, r)
		return w
	}
	rid := strings.Repeat("a", 32)
	long := strings.Repeat("é", 100) // 200 bytes: clipped to 120 bytes of whole characters
	ok := `{"request_id":"` + rid + `","outcome":"posted","locale":"es-MX","items":[` +
		`{"item":1,"photo_id":"` + strings.Repeat("b", 32) + `","model_title":"Red Apple","final_title":"` + long + `","model_servings":8,"final_servings":120,"kept":true},` +
		`{"item":2,"model_title":"Green pear","final_title":"Green pear","model_servings":1,"final_servings":1,"kept":false}]}`
	if w := post("/api/analyze/labels", "u1", ok); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := post("/v1/analyze/labels", "bearer", ok); w.Code != 204 {
		t.Fatalf("bearer %d %s", w.Code, w.Body)
	}
	ev := s.events(t)
	if len(ev) != 2 || ev[0].Step != "labels" || ev[0].RequestID != rid || ev[0].UserID != "u1" || ev[0].Labels == nil {
		t.Fatalf("%+v", ev)
	}
	l := ev[0].Labels
	if l.Outcome != "posted" || l.Locale != "es-MX" || len(l.Items) != 2 || l.Items[0].FinalServings != 99 ||
		len(l.Items[0].FinalTitle) > 120 || !strings.HasPrefix(long, l.Items[0].FinalTitle) || l.Items[1].Kept || ev[1].Caller != "bearer" {
		t.Fatalf("%+v", l)
	}
	for _, c := range []struct {
		who, body string
		code      int
	}{
		{"", ok, 401},
		{"u1", `{"request_id":"nope","outcome":"posted","items":[]}`, 400},
		{"u1", `{"request_id":"` + rid + `","outcome":"maybe","items":[]}`, 400},
		{"u1", `{"request_id":"` + rid + `","outcome":"posted","locale":"<script>","items":[]}`, 400},
		{"u1", `{"request_id":"` + rid + `","outcome":"posted","items":[{"item":0}]}`, 400},
		{"u1", `not json`, 400},
		{"u1", `{"request_id":"` + rid + `","outcome":"posted","items":[],"pad":"` + strings.Repeat("x", 33000) + `"}`, 413}, // X48u: 32 KB
	} {
		if w := post("/api/analyze/labels", c.who, c.body); w.Code != c.code {
			t.Fatalf("%q: want %d, got %d %s", c.body[:20], c.code, w.Code, w.Body)
		}
	}
	if n := len(s.events(t)); n != 2 {
		t.Fatalf("refused reports must not be logged: %d events", n)
	}
}

// X48u: the edits in order and the chat lines are logged, bounded and
// checked; the pickup text is never kept (only that it changed and how).
func TestLabelsKeepEditsAndChat(t *testing.T) {
	s := newSvc(t, oneBox(t, "Romanesco"), "FOOD", 0, nil)
	post := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/analyze/labels", strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: SessionCookie, Value: "u1"})
		w := httptest.NewRecorder()
		s.h.ServeHTTP(w, r)
		return w
	}
	rid := strings.Repeat("a", 32)
	head := `{"request_id":"` + rid + `","outcome":"posted","items":[{"item":1,"model_title":"Bananas","final_title":"Ripe bananas","model_servings":2,"final_servings":6,"kept":true,"photo":"touched_up","touchup":"done"}]`
	ok := head + `,"edits":[{"item":1,"field":"title","from":"Bananas","to":"Ripe bananas","how":"voice"},` +
		`{"item":1,"field":"servings","from":"2","to":"6","how":"voice"},{"item":0,"field":"pickup","from":"12 Main St","to":"Back door, 9 Elm Rd","how":"text"},` +
		`{"item":1,"field":"photo","from":"original","to":"touched_up","how":"hand"}],` +
		`"chat":[{"how":"voice","text":"` + strings.Repeat("w", 600) + `","changes":2},{"how":"text","text":"back door","changes":1,"undone":true,"pickup":true}]}`
	if w := post(ok); w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	l := s.events(t)[0].Labels
	if len(l.Edits) != 4 || l.Edits[0].How != "voice" || l.Edits[1].To != "6" || l.Edits[2].From != "" || l.Edits[2].To != "" ||
		len(l.Chat) != 2 || len(l.Chat[0].Text) != 500 || !l.Chat[1].Undone || l.Chat[1].Text != "" || l.Items[0].Photo != "touched_up" || l.Items[0].Touchup != "done" {
		t.Fatalf("%+v", l)
	}
	// X49: the details the user changed are edits too (condition, diet, portions, weight), kept with their values
	det := head + `,"edits":[{"item":1,"field":"condition","from":"ready_to_eat","to":"sealed","how":"hand"},` +
		`{"item":1,"field":"diet","from":"0","to":"1","how":"hand"},{"item":1,"field":"portions","from":"","to":"bulk","how":"hand"},` +
		`{"item":1,"field":"weight","from":"1.2","to":"2.5","how":"hand"}]}`
	if w := post(det); w.Code != 204 {
		t.Fatalf("details edits: %d %s", w.Code, w.Body)
	}
	if d := s.events(t)[1].Labels; len(d.Edits) != 4 || d.Edits[0].Field != "condition" || d.Edits[0].To != "sealed" ||
		d.Edits[2].From != "" || d.Edits[2].To != "bulk" || d.Edits[3].Field != "weight" || d.Edits[3].To != "2.5" {
		t.Fatalf("%+v", d.Edits)
	}
	for _, bad := range []string{
		head + `,"edits":[{"item":1,"field":"price","how":"hand"}]}`,
		head + `,"edits":[{"item":1,"field":"meal_cost","from":"4","to":"5","how":"hand"}]}`,
		head + `,"edits":[{"item":1,"field":"title","how":"magic"}]}`,
		head + `,"edits":[{"item":100,"field":"title","how":"hand"}]}`,
		head + `,"chat":[{"how":"mind","text":"x","changes":1}]}`,
		head + `,"edits":[` + strings.TrimSuffix(strings.Repeat(`{"item":1,"field":"title","how":"hand"},`, 65), ",") + `]}`,
		strings.Replace(head, `"photo":"touched_up"`, `"photo":"other"`, 1) + `}`,
	} {
		if w := post(bad); w.Code != 400 {
			t.Fatalf("want 400, got %d for %.80s", w.Code, bad[len(head)-10:])
		}
	}
}

// I4: an item the find service added from a chat line is a "found" edit.
func TestLabelsFoundEdit(t *testing.T) {
	s := newSvc(t, oneBox(t, "Romanesco"), "FOOD", 0, nil)
	body := `{"request_id":"` + strings.Repeat("b", 32) + `","outcome":"posted","items":[],"edits":[{"item":4,"field":"found","from":"","to":"White rice","how":"text"}]}`
	r := httptest.NewRequest(http.MethodPost, "/api/analyze/labels", strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: "u1"})
	w := httptest.NewRecorder()
	s.h.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if e := s.events(t)[0].Labels.Edits; len(e) != 1 || e[0].Field != "found" || e[0].To != "White rice" {
		t.Fatalf("%+v", e)
	}
}
