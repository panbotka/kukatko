package mapsapi

import (
	"sync"
	"time"
)

// rateLimiter is a simple concurrency-safe token-bucket limiter used to cap how
// often reverse-geocode lookups reach mapy.com, protecting the monthly credit
// budget. A non-positive rate disables limiting (every call is allowed).
type rateLimiter struct {
	mu       sync.Mutex
	tokens   float64
	max      float64
	rate     float64 // tokens added per second
	last     time.Time
	disabled bool
}

// newRateLimiter returns a token-bucket limiter that refills at ratePerSec tokens
// per second up to a burst of burst tokens. A non-positive ratePerSec disables the
// limiter so it always allows.
func newRateLimiter(ratePerSec float64, burst int) *rateLimiter {
	if ratePerSec <= 0 {
		return &rateLimiter{disabled: true}
	}
	if burst < 1 {
		burst = 1
	}
	return &rateLimiter{
		tokens: float64(burst),
		max:    float64(burst),
		rate:   ratePerSec,
		last:   time.Now(),
	}
}

// allow reports whether a request may proceed now, consuming one token when it
// can. A disabled limiter always allows.
func (r *rateLimiter) allow() bool {
	if r.disabled {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(r.last).Seconds()
	r.last = now
	r.tokens += elapsed * r.rate
	if r.tokens > r.max {
		r.tokens = r.max
	}
	if r.tokens < 1 {
		return false
	}
	r.tokens--
	return true
}
