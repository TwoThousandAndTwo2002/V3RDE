package muse

import (
	"context"
	"strconv"
	"sync"
	"time"
)

// Pacer spaces the requests evenly to stay under a requests-per-minute limit.
// Baseten's limits refill all the time, not at fixed times, and Baseten asks
// for a steady rate (L4e D1). The limit comes from the x-ratelimit-limit-requests
// header of an answer, unless it is fixed with SetRPM.
type Pacer struct {
	mu       sync.Mutex
	interval time.Duration
	fixed    bool
	next     time.Time
	Now      func() time.Time
	Sleep    func(context.Context, time.Duration) error
}

// NewPacer has no limit until an answer gives one (or SetRPM).
func NewPacer() *Pacer { return &Pacer{Now: time.Now, Sleep: sleepCtx} }

// SetRPM fixes the limit (n > 0); headers no longer change it.
func (p *Pacer) SetRPM(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if n > 0 {
		p.interval, p.fixed = time.Minute/time.Duration(n), true
	}
}

// RPM is the limit in use (0: none yet).
func (p *Pacer) RPM() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.interval == 0 {
		return 0
	}
	return int(time.Minute / p.interval)
}

// fromHeader takes the limit of an answer, when it is not fixed.
func (p *Pacer) fromHeader(limit string) {
	n, err := strconv.Atoi(limit)
	if err != nil || n <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.fixed {
		p.interval = time.Minute / time.Duration(n)
	}
}

// Wait keeps one slot for this request and sleeps until it.
func (p *Pacer) Wait(ctx context.Context) error {
	p.mu.Lock()
	now := p.Now()
	at := p.next
	if at.Before(now) {
		at = now
	}
	p.next = at.Add(p.interval)
	p.mu.Unlock()
	if d := at.Sub(now); d > 0 {
		return p.Sleep(ctx, d)
	}
	return nil
}
