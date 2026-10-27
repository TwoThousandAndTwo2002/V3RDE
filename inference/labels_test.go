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
		{"u1", `{"request_id":"` + rid + `","outcome":"posted","items":[],"pad":"` + strings.Repeat("x", 17000) + `"}`, 413},
	} {
		if w := post("/api/analyze/labels", c.who, c.body); w.Code != c.code {
			t.Fatalf("%q: want %d, got %d %s", c.body[:20], c.code, w.Code, w.Body)
		}
	}
	if n := len(s.events(t)); n != 2 {
		t.Fatalf("refused reports must not be logged: %d events", n)
	}
}
