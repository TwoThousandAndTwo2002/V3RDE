package auth

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ---- the rate limit -------------------------------------------------------

// pruneAt: when a new address comes and the limiter holds this many, the
// addresses with no hit in the window are removed (at most one time per
// window), so the map does not grow until a restart.
const pruneAt = 4096

// Limiter allows at most Burst requests per key in any Window (a sliding
// window of hit times). It is the same rule as the analyze server's limiter.
type Limiter struct {
	Burst  int
	Window time.Duration
	mu     sync.Mutex
	hits   map[string][]time.Time
	now    func() time.Time
	pruned time.Time
}

// NewLimiter makes a limiter of burst requests per window.
func NewLimiter(burst int, window time.Duration) *Limiter {
	return &Limiter{Burst: burst, Window: window, hits: map[string][]time.Time{}, now: time.Now}
}

// Allow records one hit for key when it is within the budget, and reports
// whether it was.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	recent := l.recent(key, now)
	if len(recent) >= l.Burst {
		l.hits[key] = recent
		return false
	}
	if _, known := l.hits[key]; !known && len(l.hits) >= pruneAt && now.Sub(l.pruned) >= l.Window {
		l.pruned = now
		for k := range l.hits {
			if r := l.recent(k, now); len(r) == 0 {
				delete(l.hits, k)
			} else {
				l.hits[k] = r // recent reuses the slice: write it back
			}
		}
	}
	l.hits[key] = append(recent, now)
	return true
}

// recent is the hits of key still in the window (it reuses the slice).
func (l *Limiter) recent(key string, now time.Time) []time.Time {
	old := l.hits[key]
	keep := old[:0]
	for _, t := range old {
		if now.Sub(t) < l.Window {
			keep = append(keep, t)
		}
	}
	return keep
}

// clientIP is the rate-limit key: CF-Connecting-IP, else the first
// X-Forwarded-For entry (Caddy sets it), else the socket peer.
func clientIP(r *http.Request) string {
	if v := r.Header.Get("CF-Connecting-IP"); v != "" {
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		first, _, _ := strings.Cut(v, ",")
		return strings.TrimSpace(first)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// ---- HTTP -----------------------------------------------------------------

// Config is everything the routes need; cmd/auth fills it from flags.
type Config struct {
	Store        *Store
	Limiter      *Limiter // signup + login, per address; nil = no limit
	CookieSecure bool
	Prefix       string // "" = /api/auth
}

type creds struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// fail is every error body: {"error": msg}.
func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// cookie is the session cookie with its fixed attributes: HttpOnly (no
// script can read it), Secure (https only, unless -cookie-secure=false) and
// SameSite=Lax (https://pkg.go.dev/net/http@go1.24.7#Cookie).
func (c Config) cookie(value string) *http.Cookie {
	return &http.Cookie{Name: CookieName, Value: value, Path: "/", HttpOnly: true,
		Secure: c.CookieSecure, SameSite: http.SameSiteLaxMode}
}

func (c Config) setCookie(w http.ResponseWriter, token string, expires time.Time) {
	ck := c.cookie(token)
	ck.Expires = expires
	ck.MaxAge = int(time.Until(expires).Seconds())
	http.SetCookie(w, ck)
}

func (c Config) clearCookie(w http.ResponseWriter) {
	ck := c.cookie("")
	ck.MaxAge = -1
	http.SetCookie(w, ck)
}

func tokenFrom(r *http.Request) string {
	if ck, err := r.Cookie(CookieName); err == nil {
		return ck.Value
	}
	return ""
}

// readCreds reads {email, password} from a JSON body of at most 16 KiB.
func readCreds(w http.ResponseWriter, r *http.Request) (creds, bool) {
	var in creds
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		fail(w, http.StatusUnsupportedMediaType, "send application/json")
		return in, false
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, "bad JSON body")
		return in, false
	}
	return in, true
}

// New makes the routes; every route is under c.Prefix.
func New(c Config) http.Handler {
	if c.Prefix == "" {
		c.Prefix = "/api/auth"
	}
	p := c.Prefix
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+p+"/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "auth"})
	})
	mux.HandleFunc("POST "+p+"/signup", c.limited(c.signup))
	mux.HandleFunc("POST "+p+"/login", c.limited(c.login))
	mux.HandleFunc("POST "+p+"/logout", c.logout)
	mux.HandleFunc("GET "+p+"/me", c.me)
	return mux
}

// limited answers 429 when the caller's address is over the budget.
func (c Config) limited(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c.Limiter != nil && !c.Limiter.Allow(clientIP(r)) {
			fail(w, http.StatusTooManyRequests, "rate limited")
			return
		}
		next(w, r)
	}
}

// startSession sets the cookie of a new session and answers u, or a 500.
func (c Config) startSession(w http.ResponseWriter, u User, status int, what string) {
	tok, exp, err := c.Store.NewSession(u.ID)
	if err != nil {
		log.Printf("session: %v", err)
		fail(w, http.StatusInternalServerError, "could not start session")
		return
	}
	c.setCookie(w, tok, exp)
	log.Printf("%s ok user=%s", what, u.ID)
	writeJSON(w, status, u)
}

func (c Config) signup(w http.ResponseWriter, r *http.Request) {
	in, ok := readCreds(w, r)
	if !ok {
		return
	}
	u, err := c.Store.Signup(in.Email, in.Password)
	switch {
	case errors.Is(err, ErrBadEmail), errors.Is(err, ErrShortPass):
		fail(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrEmailTaken):
		fail(w, http.StatusConflict, err.Error())
	case err != nil:
		log.Printf("signup: %v", err)
		fail(w, http.StatusInternalServerError, "could not create account")
	default:
		c.startSession(w, u, http.StatusCreated, "signup")
	}
}

func (c Config) login(w http.ResponseWriter, r *http.Request) {
	in, ok := readCreds(w, r)
	if !ok {
		return
	}
	u, err := c.Store.Login(in.Email, in.Password)
	switch {
	case errors.Is(err, ErrBadLogin):
		fail(w, http.StatusUnauthorized, err.Error())
	case err != nil:
		log.Printf("login: %v", err)
		fail(w, http.StatusInternalServerError, "could not log in")
	default:
		c.startSession(w, u, http.StatusOK, "login")
	}
}

// logout always answers 204 and clears the cookie.
func (c Config) logout(w http.ResponseWriter, r *http.Request) {
	if t := tokenFrom(r); t != "" {
		if err := c.Store.DeleteSession(t); err != nil {
			log.Printf("logout: %v", err) // the session is gone from memory; the file write failed
		}
	}
	c.clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (c Config) me(w http.ResponseWriter, r *http.Request) {
	u, err := c.Store.UserForSession(tokenFrom(r))
	if err != nil {
		fail(w, http.StatusUnauthorized, "not logged in")
		return
	}
	writeJSON(w, http.StatusOK, u)
}
