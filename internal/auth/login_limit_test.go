package auth

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/clientip"
)

// trustPrivate is the trusted-proxy set the deployed instance uses.
func trustPrivate(t *testing.T) *clientip.Set {
	t.Helper()
	set, err := clientip.ParseSet([]string{"loopback", "private"})
	if err != nil {
		t.Fatalf("ParseSet: %v", err)
	}
	return set
}

// loginRequestFrom builds a login POST from remoteAddr carrying the given
// forwarding headers.
func loginRequestFrom(t *testing.T, username, remoteAddr string, headers map[string]string) *http.Request {
	t.Helper()
	body := `{"username":"` + username + `","password":"whatever-guess"}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/auth/login", strings.NewReader(body))
	req.RemoteAddr = remoteAddr
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	return req
}

// keyFor runs a request through the client-IP middleware and reports the login
// bucket key the handler would compute for it.
func keyFor(t *testing.T, trusted *clientip.Set, username, remoteAddr string, headers map[string]string) string {
	t.Helper()
	var key string
	clientip.Middleware(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		key = loginLimitKey(username, r)
	})).ServeHTTP(httptest.NewRecorder(), loginRequestFrom(t, username, remoteAddr, headers))
	return key
}

// postLogin sends a login request through the client-IP middleware into the
// handler and returns the status code. Every call here is expected to be
// throttled before the credential check, so the API needs no service.
func postLogin(t *testing.T, api *API, trusted *clientip.Set,
	username, remoteAddr string, headers map[string]string,
) int {
	t.Helper()
	rec := httptest.NewRecorder()
	clientip.Middleware(trusted)(http.HandlerFunc(api.handleLogin)).
		ServeHTTP(rec, loginRequestFrom(t, username, remoteAddr, headers))
	return rec.Code
}

// TestLoginLimitKey_addressHalf is SEC-001 at the login endpoint: the bucket key
// follows the address the request was dialled from unless a *trusted* proxy
// named a different client, so no header a caller controls can move it.
func TestLoginLimitKey_addressHalf(t *testing.T) {
	t.Parallel()

	trusted := trustPrivate(t)

	tests := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		want       string
	}{
		{name: "no headers", remoteAddr: "203.0.113.44:9000", want: "target|203.0.113.44"},
		{
			name: "forged X-Forwarded-For from an untrusted peer", remoteAddr: "203.0.113.44:9000",
			headers: map[string]string{"X-Forwarded-For": "10.20.30.1"}, want: "target|203.0.113.44",
		},
		{
			name: "forged True-Client-IP from an untrusted peer", remoteAddr: "203.0.113.44:9000",
			headers: map[string]string{"True-Client-IP": "10.20.30.1"}, want: "target|203.0.113.44",
		},
		{
			name: "trusted proxy names the client", remoteAddr: "172.18.0.2:9000",
			headers: map[string]string{"X-Forwarded-For": "203.0.113.1"}, want: "target|203.0.113.1",
		},
		{
			name: "trusted proxy names a different client", remoteAddr: "172.18.0.2:9000",
			headers: map[string]string{"X-Forwarded-For": "203.0.113.2"}, want: "target|203.0.113.2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := keyFor(t, trusted, "target", tt.remoteAddr, tt.headers); got != tt.want {
				t.Errorf("loginLimitKey = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestLoginLimitKey_rotatingHeaderIsOneBucket states the same property the way
// the attack does: a hundred rotating header values from one machine are one
// bucket, where before the fix they were a hundred.
func TestLoginLimitKey_rotatingHeaderIsOneBucket(t *testing.T) {
	t.Parallel()

	trusted := trustPrivate(t)
	keys := make(map[string]struct{})
	for i := range 100 {
		forged := "10.20.30." + strconv.Itoa(i)
		headers := map[string]string{
			"X-Forwarded-For": forged,
			"X-Real-Ip":       forged,
			"True-Client-IP":  forged,
		}
		keys[keyFor(t, trusted, "target", "203.0.113.44:9000", headers)] = struct{}{}
	}
	if len(keys) != 1 {
		t.Errorf("rotating headers produced %d distinct limiter keys, want 1", len(keys))
	}
}

// TestHandleLogin_spentAddressBudgetSurvivesForgedHeaders verifies the handler
// really consults that bucket: with the budget for one address spent, further
// attempts from that address are throttled however they decorate themselves.
func TestHandleLogin_spentAddressBudgetSurvivesForgedHeaders(t *testing.T) {
	t.Parallel()

	trusted := trustPrivate(t)
	const budget = 2
	limiter := NewLimiter(budget, time.Minute)
	api := NewAPI(APIConfig{Limiter: limiter, UsernameLimiter: NewLimiter(1000, time.Minute)})

	now := time.Now()
	for i := range budget {
		if !limiter.Allow("target|203.0.113.44", now) {
			t.Fatalf("priming attempt %d was blocked, want allowed", i+1)
		}
	}

	for i := range 4 {
		forged := "10.20.30." + strconv.Itoa(i)
		headers := map[string]string{
			"X-Forwarded-For": forged,
			"X-Real-Ip":       forged,
			"True-Client-IP":  forged,
		}
		status := postLogin(t, api, trusted, "target", "203.0.113.44:9000", headers)
		if status != http.StatusTooManyRequests {
			t.Errorf("attempt %d claiming to be %s: status = %d, want 429", i+1, forged, status)
		}
	}
}

// TestHandleLogin_perUsernameBudgetIgnoresTheAddress verifies the IP-independent
// counter: once a username's budget is spent, attempts for it are throttled no
// matter which address they arrive from — the defence that would still hold if a
// future proxy misconfiguration made addresses forgeable again.
func TestHandleLogin_perUsernameBudgetIgnoresTheAddress(t *testing.T) {
	t.Parallel()

	trusted := trustPrivate(t)
	usernameLimiter := NewLimiter(2, time.Minute)
	api := NewAPI(APIConfig{Limiter: NewLimiter(1000, time.Minute), UsernameLimiter: usernameLimiter})

	now := time.Now()
	for i := range 2 {
		if !usernameLimiter.Allow("target", now) {
			t.Fatalf("priming attempt %d was blocked, want allowed", i+1)
		}
	}

	// Every one of these arrives from a genuinely different address.
	for i := range 3 {
		peer := "198.51.100." + strconv.Itoa(i) + ":40000"
		if status := postLogin(t, api, trusted, "target", peer, nil); status != http.StatusTooManyRequests {
			t.Errorf("attempt from %s: status = %d, want 429", peer, status)
		}
	}

	// A different account is untouched: the block is on the username, not on the
	// endpoint.
	if !api.usernameLimiter.Allow("someone-else", now) {
		t.Error("an unrelated username was blocked; the per-username budget leaked across accounts")
	}
}

// TestUsernameLimiterFor covers the derivation of the per-username limiter from
// the per-IP one, including the explicit-override and no-limiter cases.
func TestUsernameLimiterFor(t *testing.T) {
	t.Parallel()

	explicit := NewLimiter(7, time.Hour)
	got := usernameLimiterFor(APIConfig{Limiter: NewLimiter(5, time.Minute), UsernameLimiter: explicit})
	if got != explicit {
		t.Error("an explicitly configured UsernameLimiter was replaced")
	}

	derived := usernameLimiterFor(APIConfig{Limiter: NewLimiter(5, 15*time.Minute)})
	if derived.max != 5*usernameLimitFactor {
		t.Errorf("derived max = %d, want %d", derived.max, 5*usernameLimitFactor)
	}
	if derived.window != 15*time.Minute {
		t.Errorf("derived window = %s, want %s", derived.window, 15*time.Minute)
	}

	fallback := usernameLimiterFor(APIConfig{})
	if fallback == nil || fallback.max < 1 {
		t.Errorf("fallback limiter = %+v, want a usable limiter", fallback)
	}
}
