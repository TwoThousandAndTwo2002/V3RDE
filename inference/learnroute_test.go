package inference

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLearnRouteAdminOnly(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"report-20261031.json", "report-20261101.json", "report-20261101.md", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(`{"day":"`+f+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := newSvc(t, threeBoxes(t), "FOOD", 0, func(c *ServiceConfig) {
		c.LearnDir, c.Admins = dir, map[string]bool{"boss": true}
	})
	call := func(method, path, who string, bearer bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		if who != "" {
			r.AddCookie(&http.Cookie{Name: SessionCookie, Value: who})
		}
		if bearer {
			r.Header.Set("Authorization", "Bearer tok")
		}
		w := httptest.NewRecorder()
		s.h.ServeHTTP(w, r)
		return w
	}
	get := func(path, who string, bearer bool) *httptest.ResponseRecorder {
		return call(http.MethodGet, path, who, bearer)
	}
	// the answer of a path that does not exist (the mux's 404)
	none := call(http.MethodGet, "/api/analyze/nothere", "boss", false)
	w := get("/api/analyze/learn", "boss", false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"days":["20261101","20261031"]`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("admin list %d %s %v", w.Code, w.Body, w.Header())
	}
	if w := get("/api/analyze/learn/20261101", "boss", false); w.Code != 200 || w.Body.String() != `{"day":"report-20261101.json"}` ||
		w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("admin day %d %s", w.Code, w.Body)
	}
	if w := get("/api/analyze/learn/20261101?format=md", "boss", false); w.Code != 200 || !strings.Contains(w.Body.String(), "report-20261101.md") ||
		!strings.HasPrefix(w.Header().Get("Content-Type"), "text/markdown") {
		t.Fatalf("admin md %d %s", w.Code, w.Body)
	}
	for _, c := range []struct {
		path, who string
		bearer    bool
	}{
		{"/api/analyze/learn", "", false},                        // no session
		{"/api/analyze/learn", "u1", false},                      // not an admin
		{"/api/analyze/learn", "", true},                         // the bearer token is not an admin
		{"/v1/analyze/learn", "boss", false},                     // the bearer prefix has no route
		{"/api/analyze/learn/20261101", "u1", false},             // not an admin
		{"/api/analyze/learn/20261031?format=md", "boss", false}, // no md file
		{"/api/analyze/learn/2026110", "boss", false},            // not a day
		{"/api/analyze/learn/..%2Fx", "boss", false},             // not a day
		{"/api/analyze/learn/20261102", "boss", false},           // no report
	} {
		if w := get(c.path, c.who, c.bearer); w.Code != 404 || w.Body.String() != none.Body.String() {
			t.Errorf("%s as %q (bearer %v): %d %s", c.path, c.who, c.bearer, w.Code, w.Body)
		}
	}
	// another method, also for the admin, and the email of the admin is not the admin
	for _, m := range []string{http.MethodPost, http.MethodDelete, http.MethodHead, http.MethodPut} {
		for _, p := range []string{"/api/analyze/learn", "/api/analyze/learn/20261101"} {
			if w := call(m, p, "boss", false); w.Code != 404 || w.Header().Get("Allow") != "" {
				t.Errorf("%s %s: %d %v", m, p, w.Code, w.Header())
			}
		}
	}
	if w := get("/api/analyze/learn", "boss@example.org", false); w.Code != 404 {
		t.Errorf("an email as the id: %d", w.Code)
	}
	// no LearnDir: 404 for an admin too; no admins: 404
	for _, mod := range []func(*ServiceConfig){
		func(c *ServiceConfig) { c.Admins = map[string]bool{"boss": true} },
		func(c *ServiceConfig) { c.LearnDir = dir },
	} {
		o := newSvc(t, threeBoxes(t), "FOOD", 0, mod)
		r := httptest.NewRequest(http.MethodGet, "/api/analyze/learn", nil)
		r.AddCookie(&http.Cookie{Name: SessionCookie, Value: "boss"})
		w := httptest.NewRecorder()
		o.h.ServeHTTP(w, r)
		if w.Code != 404 {
			t.Errorf("off: %d", w.Code)
		}
	}
}
