// Package ratelimit limits repeated failures, such as failed logins, per key.
// State is in memory only and is lost on restart, which is acceptable.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter allows at most Max failures per key within Window.  Once a key
// reaches the limit it is blocked until its oldest failure ages out.
type Limiter struct {
	Max    int
	Window time.Duration
	now    func() time.Time

	mu       sync.Mutex
	failures map[string][]time.Time
	calls    int
}

// New returns a limiter allowing max failures per key per window.
func New(max int, window time.Duration) *Limiter {
	return &Limiter{Max: max, Window: window, now: time.Now, failures: make(map[string][]time.Time)}
}

// Blocked reports whether key has reached its limit, and if so for how long.
func (l *Limiter) Blocked(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.prune(key)
	if len(f) < l.Max {
		return false, 0
	}
	return true, f[0].Add(l.Window).Sub(l.now())
}

// Fail records a failure for key.
func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures[key] = append(l.prune(key), l.now())
	l.sweep()
}

// Reset forgets key's failures, for example after a successful login.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}

// prune drops failures older than the window.  Caller holds l.mu.
func (l *Limiter) prune(key string) []time.Time {
	f := l.failures[key]
	cutoff := l.now().Add(-l.Window)
	i := 0
	for i < len(f) && !f[i].After(cutoff) {
		i++
	}
	f = f[i:]
	if len(f) == 0 {
		delete(l.failures, key)
		return nil
	}
	l.failures[key] = f
	return f
}

// sweep occasionally drops idle keys so memory stays bounded under a spray of
// distinct keys.  Caller holds l.mu.
func (l *Limiter) sweep() {
	l.calls++
	if l.calls%1000 != 0 {
		return
	}
	for k := range l.failures {
		l.prune(k)
	}
}
