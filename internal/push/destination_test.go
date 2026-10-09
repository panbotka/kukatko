package push

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// newInternalRecorder starts a plain-http server on loopback standing in for an
// internal service (a database admin, a metadata endpoint) and returns it with
// the counter of requests it has received.
func newInternalRecorder(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

// TestVAPIDSender_doesNotFollowRedirects is the SEC-020 reproduction: a TLS
// "push service" answers with a redirect to a plain-http internal server, and
// the sender is handed a client that trusts the TLS server but keeps Go's
// default redirect policy. Before the fix the internal server received the
// request (and a 307/308 even replayed the POST) and its 201 counted as a
// delivery; now the redirect is a rejected delivery and nothing follows it.
func TestVAPIDSender_doesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	for _, status := range []int{
		http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			internal, hits := newInternalRecorder(t)
			front := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, internal.URL+"/probe", status)
			}))
			t.Cleanup(front.Close)
			client := front.Client()
			if client.CheckRedirect != nil {
				t.Fatal("the test client must keep the default redirect policy")
			}
			keys := mustKeys(t)
			sender, err := NewVAPID(Config{
				Enabled: true, PublicKey: keys.PublicKey, PrivateKey: keys.PrivateKey,
				Subject: "mailto:ops@example.org", HTTPClient: client,
			})
			if err != nil {
				t.Fatalf("NewVAPID: %v", err)
			}

			err = sender.Send(context.Background(), newTestBrowser(t, front.URL+"/send/abc").sub,
				Notification{Title: "Ahoj", URL: "/"})
			var statusErr *StatusError
			if !errors.Is(err, ErrRejected) || !errors.As(err, &statusErr) || statusErr.StatusCode != status {
				t.Fatalf("Send error = %v, want ErrRejected with HTTP %d", err, status)
			}
			if got := hits.Load(); got != 0 {
				t.Fatalf("the internal server received %d requests, want 0", got)
			}
			if client.CheckRedirect != nil {
				t.Fatal("NewVAPID changed the caller's client instead of a copy")
			}
		})
	}
}

// TestVAPIDSender_defaultClientRefusesNonPublic sends through the production
// client (no HTTPClient injected) to a server on loopback, once by IP literal
// and once by a name that resolves to it. Both are refused at dial time, before
// a byte reaches the server, and reported as a permanent rejection.
func TestVAPIDSender_defaultClientRefusesNonPublic(t *testing.T) {
	t.Parallel()

	svc := newFakePushService(t)
	keys := mustKeys(t)
	sender, err := NewVAPID(Config{
		Enabled: true, PublicKey: keys.PublicKey, PrivateKey: keys.PrivateKey, Subject: "mailto:ops@example.org",
	})
	if err != nil {
		t.Fatalf("NewVAPID: %v", err)
	}
	parsed, err := url.Parse(svc.server.URL)
	if err != nil {
		t.Fatalf("parsing the server URL: %v", err)
	}
	for _, endpoint := range []string{
		svc.server.URL + "/send/abc",
		"https://localhost:" + parsed.Port() + "/send/abc",
	} {
		err := sender.Send(context.Background(), newTestBrowser(t, endpoint).sub, Notification{Title: "Ahoj", URL: "/"})
		if !errors.Is(err, ErrRejected) || !errors.Is(err, errNonPublicDestination) || Retryable(err) {
			t.Errorf("Send to %s error = %v, want a permanent ErrRejected for a non-public address", endpoint, err)
		}
	}
	if got := len(svc.received()); got != 0 {
		t.Fatalf("the loopback server received %d requests, want 0", got)
	}
}

// TestNewDefaultClient checks the production client's policy: a timeout, no
// redirects, no environment proxy.
func TestNewDefaultClient(t *testing.T) {
	t.Parallel()

	client := newDefaultClient()
	if client.Timeout != DefaultTimeout {
		t.Errorf("Timeout = %v, want %v", client.Timeout, DefaultTimeout)
	}
	if client.CheckRedirect == nil || !errors.Is(client.CheckRedirect(nil, nil), http.ErrUseLastResponse) {
		t.Error("the default client follows redirects")
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport is %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy != nil {
		t.Error("the default client takes a proxy from the environment")
	}
	if transport.DialContext == nil {
		t.Error("the default client has no guarded dialer")
	}
}

// TestIsPublicAddr covers the address policy, public addresses included so a
// normal push service still gets dialled.
func TestIsPublicAddr(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"142.250.74.10":        true, // fcm.googleapis.com-like
		"34.117.237.239":       true,
		"2a00:1450:4001::5f":   true,
		"::ffff:142.250.74.10": true,
		"64:ff9b::8efa:4a0a":   true, // NAT64 of 142.250.74.10
		"127.0.0.1":            false,
		"127.1.2.3":            false,
		"::1":                  false,
		"10.0.0.1":             false,
		"172.16.5.4":           false,
		"192.168.1.1":          false,
		"169.254.169.254":      false,
		"fe80::1":              false,
		"fd7a:115c:a1e0::1":    false, // tailnet IPv6 (ULA)
		"100.68.61.47":         false, // tailnet IPv4 (CGNAT)
		"0.0.0.0":              false,
		"::":                   false,
		"224.0.0.1":            false,
		"ff02::1":              false,
		"255.255.255.255":      false,
		"240.0.0.1":            false,
		"198.18.0.1":           false,
		"192.0.0.8":            false,
		"::ffff:127.0.0.1":     false,
		"::ffff:10.1.2.3":      false,
		"64:ff9b::a00:1":       false, // NAT64 of 10.0.0.1
		"64:ff9b:1::1":         false,
		"fec0::1":              false,
	}
	for raw, want := range tests {
		if got := isPublicAddr(netip.MustParseAddr(raw)); got != want {
			t.Errorf("isPublicAddr(%s) = %v, want %v", raw, got, want)
		}
	}
}

// TestRefuseNonPublic checks the dialer hook on the address strings the dialer
// hands it.
func TestRefuseNonPublic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		address string
		refused bool
	}{
		{address: "142.250.74.10:443"},
		{address: "[2a00:1450:4001::5f]:443"},
		{address: "127.0.0.1:443", refused: true},
		{address: "[::1]:443", refused: true},
		{address: "169.254.169.254:80", refused: true},
		{address: "10.1.2.3:5432", refused: true},
		{address: "not-an-address", refused: true},
	}
	for _, tt := range tests {
		err := refuseNonPublic("tcp", tt.address, nil)
		if got := errors.Is(err, errNonPublicDestination); got != tt.refused || (err != nil) != tt.refused {
			t.Errorf("refuseNonPublic(%s) = %v, want refused %v", tt.address, err, tt.refused)
		}
	}
}

// TestValidateSubscription_formatOnly pins that ValidateSubscription judges the
// shape of a subscription, not where it points: the internal endpoints from the
// SEC-020 lead pass it, and are stopped by checkEndpointHost and the dialer.
func TestValidateSubscription_formatOnly(t *testing.T) {
	t.Parallel()

	for _, endpoint := range []string{
		"https://127.0.0.1:8443/x", "https://10.0.0.1/x", "https://192.168.1.10/x", "https://169.254.169.254/x",
	} {
		if err := ValidateSubscription(newTestBrowser(t, endpoint).sub); err != nil {
			t.Errorf("ValidateSubscription(%s) = %v, want nil", endpoint, err)
		}
	}
}

// TestCheckEndpointHost covers the subscribe-time refusal of visibly internal
// endpoints, and that real push services pass.
func TestCheckEndpointHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		endpoint string
		refused  bool
	}{
		{endpoint: "https://fcm.googleapis.com/fcm/send/abc"},
		{endpoint: "https://updates.push.services.mozilla.com/wpush/v2/abc"},
		{endpoint: "https://wns2-db5p.notify.windows.com/w/?token=abc"},
		{endpoint: "https://web.push.apple.com/abc"},
		{endpoint: "https://push.example.org/abc"},
		{endpoint: "https://142.250.74.10/abc"},
		{endpoint: "https://127.0.0.1:8443/x", refused: true},
		{endpoint: "https://[::1]/x", refused: true},
		{endpoint: "https://10.0.0.1/x", refused: true},
		{endpoint: "https://169.254.169.254/latest/meta-data", refused: true},
		{endpoint: "https://[fe80::1%25eth0]/x", refused: true},
		{endpoint: "https://100.68.61.47/x", refused: true},
		{endpoint: "https://localhost/x", refused: true},
		{endpoint: "https://LOCALHOST./x", refused: true},
		{endpoint: "https://box:6490/x", refused: true},
		{endpoint: "https://db.localhost/x", refused: true},
		{endpoint: "https://%zz", refused: true},
	}
	for _, tt := range tests {
		err := checkEndpointHost(tt.endpoint)
		if got := errors.Is(err, ErrInvalidSubscription); got != tt.refused || (err != nil) != tt.refused {
			t.Errorf("checkEndpointHost(%s) = %v, want refused %v", tt.endpoint, err, tt.refused)
		}
	}
}

// TestPlainText checks that a push service's answer is reduced to one line of
// bounded plain text.
func TestPlainText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "plain", raw: "  invalid token \n", want: "invalid token"},
		{name: "escape sequence", raw: "a\x1b[2Jb\x07c", want: "a [2Jb c"},
		{name: "newlines and tabs", raw: "line1\r\nline2\tend", want: "line1 line2 end"},
		{name: "bidi override", raw: "abc\u202edef", want: "abc def"},
		{name: "invalid utf-8", raw: "ok\xffok", want: "ok\uFFFDok"},
		{name: "html stays text", raw: "<b>no</b>", want: "<b>no</b>"},
		{name: "truncated", raw: strings.Repeat("ž", maxErrorText+5), want: strings.Repeat("ž", maxErrorText) + "…"},
		{name: "empty", raw: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := plainText([]byte(tt.raw)); got != tt.want {
				t.Errorf("plainText(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
