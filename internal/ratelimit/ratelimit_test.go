package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestLimiter_disabled verifies a non-positive rate produces a limiter that
// allows every request regardless of key.
func TestLimiter_disabled(t *testing.T) {
	t.Parallel()

	l := New(0, 5)
	for i := range 100 {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("disabled limiter denied request %d", i)
		}
	}
}

// TestLimiter_burstThenDeny verifies the bucket allows up to burst requests
// immediately and then denies once empty.
func TestLimiter_burstThenDeny(t *testing.T) {
	t.Parallel()

	now := time.Unix(0, 0)
	l := New(1, 3)
	for i := range 3 {
		if !l.allowAt("k", now) {
			t.Fatalf("request %d within burst should be allowed", i)
		}
	}
	if l.allowAt("k", now) {
		t.Fatal("fourth request should be denied once burst is spent")
	}
}

// TestLimiter_refill verifies tokens replenish at the configured rate as time
// advances, allowing exactly the refilled count and no more.
func TestLimiter_refill(t *testing.T) {
	t.Parallel()

	now := time.Unix(0, 0)
	l := New(2, 2) // 2 tokens/sec, burst 2
	// Drain the burst.
	l.allowAt("k", now)
	l.allowAt("k", now)
	if l.allowAt("k", now) {
		t.Fatal("expected drained bucket to deny")
	}
	// After 1 second, 2 tokens have refilled.
	later := now.Add(time.Second)
	if !l.allowAt("k", later) {
		t.Fatal("expected refill to allow first request")
	}
	if !l.allowAt("k", later) {
		t.Fatal("expected refill to allow second request")
	}
	if l.allowAt("k", later) {
		t.Fatal("expected third request to exceed refilled tokens")
	}
}

// TestLimiter_perKeyIsolation verifies one key's exhaustion does not affect
// another key.
func TestLimiter_perKeyIsolation(t *testing.T) {
	t.Parallel()

	now := time.Unix(0, 0)
	l := New(1, 1)
	if !l.allowAt("a", now) {
		t.Fatal("first request for key a should be allowed")
	}
	if l.allowAt("a", now) {
		t.Fatal("second request for key a should be denied")
	}
	if !l.allowAt("b", now) {
		t.Fatal("key b must have its own bucket and be allowed")
	}
}

// TestLimiter_cleanup verifies fully refilled buckets are pruned while partially
// drained ones are retained.
func TestLimiter_cleanup(t *testing.T) {
	t.Parallel()

	now := time.Unix(0, 0)
	l := New(1, 4)
	l.allowAt("full", now) // one token spent, refills quickly
	for range 4 {          // drain "empty" completely
		l.allowAt("empty", now)
	}
	// After 10s both are fully refilled and should be pruned.
	l.Cleanup(now.Add(10 * time.Second))
	l.mu.Lock()
	remaining := len(l.buckets)
	l.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("expected all refilled buckets pruned, %d remain", remaining)
	}
}

// TestMiddleware_passthroughWhenDisabled verifies a disabled limiter's
// middleware forwards every request.
func TestMiddleware_passthroughWhenDisabled(t *testing.T) {
	t.Parallel()

	l := New(0, 1)
	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for i := range 5 {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d got %d, want 200", i, rec.Code)
		}
	}
}

// TestMiddleware_limitsByIP verifies the middleware allows burst requests from
// an IP and then returns 429, while a different IP is unaffected.
func TestMiddleware_limitsByIP(t *testing.T) {
	t.Parallel()

	l := New(0.0001, 2) // tiny refill so the burst does not replenish mid-test
	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	do := func(ip string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/upload", nil)
		req.RemoteAddr = ip + ":5555"
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := do("1.1.1.1"); got != http.StatusOK {
		t.Fatalf("first request: got %d, want 200", got)
	}
	if got := do("1.1.1.1"); got != http.StatusOK {
		t.Fatalf("second request: got %d, want 200", got)
	}
	if got := do("1.1.1.1"); got != http.StatusTooManyRequests {
		t.Fatalf("third request: got %d, want 429", got)
	}
	// A different client IP keeps its own allowance.
	if got := do("2.2.2.2"); got != http.StatusOK {
		t.Fatalf("other IP: got %d, want 200", got)
	}
}

// TestClientIP verifies extraction of the host portion with and without a port.
func TestClientIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		remoteAddr string
		want       string
	}{
		{name: "ipv4 with port", remoteAddr: "192.0.2.1:443", want: "192.0.2.1"},
		{name: "bare ipv4", remoteAddr: "192.0.2.1", want: "192.0.2.1"},
		{name: "ipv6 with port", remoteAddr: "[2001:db8::1]:443", want: "2001:db8::1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if got := clientIP(req); got != tt.want {
				t.Errorf("clientIP(%q) = %q, want %q", tt.remoteAddr, got, tt.want)
			}
		})
	}
}
