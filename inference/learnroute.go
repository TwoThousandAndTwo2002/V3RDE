package inference

// LR3 (I1, I3; T9 2026-11-01: the report page is for "Admin accounts only"):
// the stored daily reports of cmd/learn, read-only, for an admin session.
//
//	GET /api/analyze/learn            -> {"days": ["20261101", ...]} (newest first)
//	GET /api/analyze/learn/{day}      -> the stored report JSON
//	GET /api/analyze/learn/{day}?format=md -> the report as Markdown
//
// Admin = a signed-in user whose account id is in ServiceConfig.Admins (not
// the email: sign-up does not check that an email is the user's, so anybody
// could sign up first with an admin's email). Any other caller (no session,
// another user, the bearer token, another method) gets the same plain 404 as
// a path that does not exist, so the route does not show that it exists
// (OWASP API5:2023, function level authorization). No cache.

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var learnDayRe = regexp.MustCompile(`^[0-9]{8}$`)

// admin is true for a GET of a signed-in admin.
func (c *ServiceConfig) admin(r *http.Request) bool {
	if r.Method != http.MethodGet || c.Session == nil || len(c.Admins) == 0 {
		return false
	}
	u, ok := c.Session.Who(r)
	return ok && u.ID != "" && c.Admins[u.ID]
}

// learnNotFound is the mux's own 404 (http.NotFound), the answer of a path
// that does not exist.
func (c *ServiceConfig) learnNotFound(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}

func (c *ServiceConfig) learnDays(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if c.LearnDir == "" || !c.admin(r) {
		c.learnNotFound(w, r)
		return
	}
	files, _ := filepath.Glob(filepath.Join(c.LearnDir, "report-????????.json"))
	days := []string{}
	for _, f := range files {
		if d := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "report-"), ".json"); learnDayRe.MatchString(d) {
			days = append(days, d)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(days)))
	writeJSONResp(w, http.StatusOK, map[string]any{"days": days})
}

func (c *ServiceConfig) learnDay(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	day := r.PathValue("day")
	if c.LearnDir == "" || !c.admin(r) || !learnDayRe.MatchString(day) {
		c.learnNotFound(w, r)
		return
	}
	ext, ctype := ".json", "application/json"
	if r.URL.Query().Get("format") == "md" {
		ext, ctype = ".md", "text/markdown; charset=utf-8"
	}
	b, err := os.ReadFile(filepath.Join(c.LearnDir, "report-"+day+ext))
	if err != nil {
		c.learnNotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(b)
}
