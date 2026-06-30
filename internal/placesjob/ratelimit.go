package placesjob

import (
	"sync"
	"time"
)

// RateLimiter decides whether a geocode call may proceed now. It is an interface
// so the Service can be unit-tested with a deterministic fake instead of a clock.
type RateLimiter interface {
	// Allow reports whether one geocode request may proceed now, consuming a
	// token when it can.
	Allow() bool
}

// allowAll is the default limiter used when none is configured: every call is
// allowed (throttling disabled).
type allowAll struct{}

// Allow always permits the request.
func (allowAll) Allow() bool { return true }

// TokenBucket is a concurrency-safe token-bucket limiter that caps how often the
// places job reaches mapy.com, protecting the monthly geocode credit budget. It
// mirrors the limiter the maps reverse-geocode proxy uses. A non-positive rate
// disables limiting (every call is allowed).
type TokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	max      float64
	rate     float64 // tokens added per second
	last     time.Time
	disabled bool
}

// NewTokenBucket returns a limiter that refills at ratePerSec tokens per second up
// to a burst of burst tokens. A non-positive ratePerSec disables the limiter so it
// always allows; a burst below one is raised to one.
func NewTokenBucket(ratePerSec float64, burst int) *TokenBucket {
	if ratePerSec <= 0 {
		return &TokenBucket{disabled: true}
	}
	if burst < 1 {
		burst = 1
	}
	return &TokenBucket{
		tokens: float64(burst),
		max:    float64(burst),
		rate:   ratePerSec,
		last:   time.Now(),
	}
}

// Allow reports whether a geocode request may proceed now, consuming one token
// when it can. A disabled limiter always allows.
func (t *TokenBucket) Allow() bool {
	if t.disabled {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(t.last).Seconds()
	t.last = now
	t.tokens += elapsed * t.rate
	if t.tokens > t.max {
		t.tokens = t.max
	}
	if t.tokens < 1 {
		return false
	}
	t.tokens--
	return true
}
