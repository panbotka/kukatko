// Package ratelimit provides a per-key token-bucket rate limiter and HTTP
// middleware for capping abusive request patterns on resource-intensive
// endpoints (upload, bulk edit, import triggers, map tile proxy).
//
// A limiter keys buckets by an arbitrary string — typically the client IP — so
// one noisy caller cannot exhaust shared resources while others keep working.
// Building a limiter with a non-positive rate yields a disabled limiter that
// allows everything, letting a single endpoint opt out purely via configuration
// without branching at the call site.
package ratelimit

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"
)

// Limiter is a concurrency-safe token-bucket rate limiter keyed by an arbitrary
// string. Each key gets its own bucket that refills at a fixed rate up to a
// burst ceiling. The zero value is not usable; construct one with New.
type Limiter struct {
	mu       sync.Mutex
	rate     float64 // tokens added per second
	burst    float64
	buckets  map[string]*bucket
	now      func() time.Time
	disabled bool
}

// bucket is the per-key token reservoir together with the last time it was
// refilled, used to compute lazy refill on the next request.
type bucket struct {
	tokens float64
	last   time.Time
}

// maxBuckets bounds the live bucket count. When a new key would exceed it, the
// limiter first prunes fully refilled buckets (which are indistinguishable from
// fresh ones), keeping memory bounded without an external maintenance goroutine.
const maxBuckets = 8192

// New returns a token-bucket Limiter that refills each key's bucket at
// ratePerSec tokens per second up to burst tokens. A non-positive ratePerSec
// disables the limiter so Allow always returns true and Middleware is a
// pass-through; a burst below one is clamped to one.
func New(ratePerSec float64, burst int) *Limiter {
	if ratePerSec <= 0 {
		return &Limiter{disabled: true, now: time.Now}
	}
	if burst < 1 {
		burst = 1
	}
	return &Limiter{
		rate:    ratePerSec,
		burst:   float64(burst),
		buckets: make(map[string]*bucket),
		now:     time.Now,
	}
}

// Allow reports whether a request keyed by key may proceed now, consuming one
// token from the key's bucket when it can. A disabled limiter always allows.
func (l *Limiter) Allow(key string) bool {
	return l.allowAt(key, l.now())
}

// allowAt is Allow with an explicit timestamp so tests can advance time
// deterministically without sleeping. It lazily refills the key's bucket based
// on the elapsed time since its last access before spending a token.
func (l *Limiter) allowAt(key string, now time.Time) bool {
	if l.disabled {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= maxBuckets {
			l.pruneLocked(now)
		}
		// A fresh key starts with a full bucket and immediately spends one token.
		l.buckets[key] = &bucket{tokens: l.burst - 1, last: now}
		return true
	}
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens += elapsed * l.rate
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Cleanup removes buckets that have fully refilled as of now, bounding memory
// for a limiter that sees many distinct keys over time. A fully refilled bucket
// is indistinguishable from a fresh one, so dropping it changes nothing.
func (l *Limiter) Cleanup(now time.Time) {
	if l.disabled {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)
}

// pruneLocked drops every fully refilled bucket as of now. The caller must hold
// l.mu.
func (l *Limiter) pruneLocked(now time.Time) {
	for key, b := range l.buckets {
		if b.tokens+now.Sub(b.last).Seconds()*l.rate >= l.burst {
			delete(l.buckets, key)
		}
	}
}

// RunMaintenance periodically prunes fully refilled buckets until ctx is
// canceled. It is meant to run in its own goroutine for the lifetime of the
// server. A disabled limiter returns immediately.
func (l *Limiter) RunMaintenance(ctx context.Context, interval time.Duration) {
	if l.disabled || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.Cleanup(l.now())
		}
	}
}

// Middleware returns net/http middleware that rate-limits requests by client IP,
// responding 429 with a JSON error and a Retry-After header when the caller's
// bucket is empty. A disabled limiter returns next unchanged, so an opted-out
// endpoint pays zero per-request overhead.
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	if l.disabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(clientIP(r)) {
			w.Header().Set("Retry-After", "1")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP returns the best-effort client IP for r. chi's RealIP middleware
// rewrites RemoteAddr from X-Forwarded-For/X-Real-IP upstream, so the host part
// of RemoteAddr is the real client address behind a trusted proxy. When
// RemoteAddr carries no port (already a bare address) it is returned as-is.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
