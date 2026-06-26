package auth

import (
	"sync"
	"time"
)

// Limiter is a concurrency-safe sliding-window rate limiter keyed by an
// arbitrary string (the login handler keys on username+client IP). It records
// failed-attempt timestamps per key and blocks a key once it accumulates max
// attempts within the trailing window. A successful login should Reset the key.
//
// Timestamps are supplied by the caller (Allow/Reset take an explicit now) so
// the limiter is deterministic under test; in production the caller passes
// time.Now().
type Limiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	attempts map[string][]time.Time
}

// NewLimiter returns a Limiter that permits at most max attempts per key within
// window. max is clamped to a minimum of 1 and window to a minimum of one
// nanosecond so the limiter is always well-formed.
func NewLimiter(maxAttempts int, window time.Duration) *Limiter {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	if window <= 0 {
		window = time.Nanosecond
	}
	return &Limiter{
		max:      maxAttempts,
		window:   window,
		attempts: make(map[string][]time.Time),
	}
}

// Allow records an attempt for key at time now and reports whether the attempt
// is permitted. It first discards attempts older than the window; if the key is
// already at the limit it returns false without recording (so a blocked key
// does not extend its own block), otherwise it records now and returns true.
func (l *Limiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	live := l.prune(l.attempts[key], now)
	if len(live) >= l.max {
		l.attempts[key] = live
		return false
	}
	l.attempts[key] = append(live, now)
	return true
}

// Reset clears all recorded attempts for key, called after a successful login so
// the user is not penalised for earlier failures.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// Cleanup drops keys whose every recorded attempt has aged out of the window as
// of now, bounding memory for the many distinct keys seen over time. It is safe
// to call periodically from a background goroutine.
func (l *Limiter) Cleanup(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, ts := range l.attempts {
		if live := l.prune(ts, now); len(live) == 0 {
			delete(l.attempts, key)
		} else {
			l.attempts[key] = live
		}
	}
}

// prune returns the subset of ts that falls within the trailing window ending at
// now (attempts strictly older than now-window are dropped). The input slice may
// be reused, so callers must use the returned slice.
func (l *Limiter) prune(timestamps []time.Time, now time.Time) []time.Time {
	cutoff := now.Add(-l.window)
	live := timestamps[:0]
	for _, ts := range timestamps {
		if ts.After(cutoff) {
			live = append(live, ts)
		}
	}
	return live
}
