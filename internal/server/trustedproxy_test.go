package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/panbotka/kukatko/internal/clientip"
	"github.com/panbotka/kukatko/internal/ratelimit"
)

// mustTrust builds a trusted-proxy set for a test, failing on a bad entry.
func mustTrust(t *testing.T, entries ...string) *clientip.Set {
	t.Helper()
	set, err := clientip.ParseSet(entries)
	if err != nil {
		t.Fatalf("ParseSet(%v): %v", entries, err)
	}
	return set
}

// limitedServer builds a server whose every route sits behind a token bucket of
// exactly burst requests per client IP, so a test can count how many callers the
// limiter believes it is talking to.
func limitedServer(t *testing.T, trusted *clientip.Set, burst int) http.Handler {
	t.Helper()
	// A rate of one token per second cannot refill within a test, so the bucket
	// is effectively "burst requests, then blocked".
	limiter := ratelimit.New(1, burst)
	return New("",
		WithTrustedProxies(trusted),
		WithMiddleware(limiter.Middleware),
	).Handler()
}

// hitHealthz sends one GET /healthz from remoteAddr carrying the given
// forwarding headers and returns the status code.
func hitHealthz(t *testing.T, handler http.Handler, remoteAddr string, headers map[string]string) int {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
	req.RemoteAddr = remoteAddr
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code
}

// TestTrustedProxies_forgedHeaderCannotRefillTheBucket is SEC-001 as an
// end-to-end property of the router: an anonymous caller that rotates its
// forwarding headers on every request stays in one rate-limit bucket, because
// the address it dialled from is not a trusted proxy. Before the fix each forged
// value minted a fresh bucket and the limiter never fired.
func TestTrustedProxies_forgedHeaderCannotRefillTheBucket(t *testing.T) {
	t.Parallel()

	const burst = 3
	handler := limitedServer(t, mustTrust(t, "loopback", "private"), burst)

	blocked := 0
	for i := range 10 {
		forged := "10.11.12." + strconv.Itoa(i)
		headers := map[string]string{
			"X-Forwarded-For": forged,
			"X-Real-Ip":       forged,
			"True-Client-IP":  forged,
		}
		if hitHealthz(t, handler, "198.51.100.77:40000", headers) == http.StatusTooManyRequests {
			blocked++
		}
	}
	if want := 10 - burst; blocked != want {
		t.Errorf("blocked %d of 10 rotating-header requests, want %d", blocked, want)
	}
}

// TestTrustedProxies_trustedHeaderStillCountsPerClient verifies the other half:
// behind a trusted reverse proxy every request arrives from the proxy's address,
// so the forwarded client must still be what the limiter keys on — otherwise the
// whole household would share one bucket.
func TestTrustedProxies_trustedHeaderStillCountsPerClient(t *testing.T) {
	t.Parallel()

	const burst = 3
	handler := limitedServer(t, mustTrust(t, "loopback", "private"), burst)

	// Each of three distinct forwarded clients spends its own bucket.
	for _, client := range []string{"203.0.113.1", "203.0.113.2", "203.0.113.3"} {
		for i := range burst {
			if got := hitHealthz(t, handler, "127.0.0.1:40000",
				map[string]string{"X-Forwarded-For": client}); got != http.StatusOK {
				t.Fatalf("client %s request %d: status = %d, want 200", client, i+1, got)
			}
		}
		if got := hitHealthz(t, handler, "127.0.0.1:40000",
			map[string]string{"X-Forwarded-For": client}); got != http.StatusTooManyRequests {
			t.Errorf("client %s past its burst: status = %d, want 429", client, got)
		}
	}
}

// TestTrustedProxies_defaultTrustsNothing verifies a server built without the
// option ignores forwarding headers entirely — the conservative default for any
// caller that mounts the router itself.
func TestTrustedProxies_defaultTrustsNothing(t *testing.T) {
	t.Parallel()

	const burst = 2
	limiter := ratelimit.New(1, burst)
	handler := New("", WithMiddleware(limiter.Middleware)).Handler()

	blocked := 0
	for i := range 6 {
		forged := "10.11.12." + strconv.Itoa(i)
		if hitHealthz(t, handler, "127.0.0.1:40000",
			map[string]string{"X-Forwarded-For": forged}) == http.StatusTooManyRequests {
			blocked++
		}
	}
	if want := 6 - burst; blocked != want {
		t.Errorf("blocked %d of 6 requests from one peer, want %d", blocked, want)
	}
}
