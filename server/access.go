package analyze

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ---- the bearer token -----------------------------------------------------

// Bearer is one shared token, compared in constant time
// (https://pkg.go.dev/crypto/subtle#ConstantTimeCompare). A token in an app
// bundle is not a secret, so the rate limit bounds what a copied token can
// spend.
type Bearer struct{ token []byte }

// NewBearer keeps the token without spaces around it. An empty token lets
// no request in.
func NewBearer(token string) *Bearer { return &Bearer{token: []byte(strings.TrimSpace(token))} }

// OK is true only for "Authorization: Bearer <token>" with the exact token.
func (b *Bearer) OK(r *http.Request) bool {
	if b == nil || len(b.token) == 0 {
		return false
	}
	got, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !found {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(got)), b.token) == 1
}

// ---- the rate limit -------------------------------------------------------

// pruneAt is the number of keys at which Allow removes the keys with no hit
// in the window, so one client per address does not grow the map forever.
// It prunes at most one time per Window, so many live keys do not make each
// request scan the map (one scan per window, not one per request).
const pruneAt = 4096

// Limiter allows at most Burst requests per key in any Window (a sliding
// window of hit times). It stops a loop from using up the key; it is not a
// fair scheduler.
type Limiter struct {
	Burst  int
	Window time.Duration
	mu     sync.Mutex
	hits   map[string][]time.Time
	now    func() time.Time
	pruned time.Time // the last prune
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
		l.prune(now)
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

// prune removes every key with no hit in the window and keeps only the
// recent hits of the others (recent reuses the slice, so the shorter slice
// must be written back).
func (l *Limiter) prune(now time.Time) {
	l.pruned = now
	for k := range l.hits {
		if r := l.recent(k, now); len(r) == 0 {
			delete(l.hits, k)
		} else {
			l.hits[k] = r
		}
	}
}

// keys is the number of keys the limiter holds (for the tests).
func (l *Limiter) keys() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.hits)
}

// ---- the caller's address -------------------------------------------------

// ClientIPs returns the socket peer and, when a tunnel or a proxy set one,
// the forwarded address (CF-Connecting-IP first, else the first
// X-Forwarded-For entry). Both are logged; neither is used for auth.
func ClientIPs(r *http.Request) (remote, forwarded string) {
	remote = r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		remote = host
	}
	if v := r.Header.Get("CF-Connecting-IP"); v != "" {
		return remote, v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		first, _, _ := strings.Cut(v, ",")
		return remote, strings.TrimSpace(first)
	}
	return remote, ""
}

// ---- the request id -------------------------------------------------------

// NewRequestID is a random 128-bit id as 32 hex characters. The caller gets
// it, the metrics line has it and the app keeps it on the listing, so one id
// joins a complaint to its line. Since Go 1.24, crypto/rand.Read never
// returns an error (https://pkg.go.dev/crypto/rand@go1.24.10#Read).
func NewRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
