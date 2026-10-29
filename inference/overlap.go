package inference

// X48a (T9 2026-10-29, the speed plan): the food gate runs at the same time
// as SAM. The rule (T9, X36 handoff B4): SAM starts with the gate; the paid
// Muse calls start only after the gate says FOOD. A GateBarrier in the
// request context holds every "muse:" call of Client.Do until the gate
// answers; a NOT_FOOD or a gate failure closes it, and the run is
// cancelled. WithStart puts the request's start time in the context, so
// each call logs at_ms (when it started, from the request start) and the
// critical path of a scan can be read from the events line.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ErrGateClosed: the gate did not pass the photo, so no Muse call is made.
var ErrGateClosed = errors.New("the food gate did not pass the photo")

// GateBarrier is open after Open(true); Open(false) closes it for good.
type GateBarrier struct {
	done chan struct{}
	once sync.Once
	ok   bool
}

// NewGateBarrier is a barrier that is not decided yet.
func NewGateBarrier() *GateBarrier { return &GateBarrier{done: make(chan struct{})} }

// Open decides the barrier one time: true lets the Muse calls go, false
// stops them. Later calls change nothing.
func (g *GateBarrier) Open(ok bool) {
	g.once.Do(func() { g.ok = ok; close(g.done) })
}

type gateKey struct{}
type startKey struct{}

// WithGate makes the Muse calls of ctx wait for g.
func WithGate(ctx context.Context, g *GateBarrier) context.Context {
	return context.WithValue(ctx, gateKey{}, g)
}

// WithStart records the request's start time for the calls' at_ms.
func WithStart(ctx context.Context, t0 time.Time) context.Context {
	return context.WithValue(ctx, startKey{}, t0)
}

// waitGate blocks a Muse call until the gate decides (no barrier: at once).
func waitGate(ctx context.Context, purpose string) error {
	g, _ := ctx.Value(gateKey{}).(*GateBarrier)
	if g == nil || !strings.HasPrefix(purpose, "muse:") {
		return nil
	}
	select {
	case <-g.done:
		if !g.ok {
			return ErrGateClosed
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// sinceStart is the time from the request start in ms (-1: no start).
func sinceStart(ctx context.Context) int64 {
	t0, ok := ctx.Value(startKey{}).(time.Time)
	if !ok {
		return -1
	}
	return time.Since(t0).Milliseconds()
}

// MetaTransport is the one connection pool of the Meta API calls. A scan
// opens 4 to 7 calls to the same host at once (3 SAM concepts, the early
// whole call, the naming chunks), and Go's default transport keeps only 2
// idle connections per host (http.DefaultMaxIdleConnsPerHost), so the
// others paid a new TLS handshake on each scan.
var MetaTransport = func() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 64
	t.MaxIdleConnsPerHost = 32
	t.IdleConnTimeout = 90 * time.Second
	return t
}()
