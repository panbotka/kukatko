package clientip

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

// mustSet builds a Set from entries, failing the test if any entry is invalid.
func mustSet(t *testing.T, entries ...string) *Set {
	t.Helper()
	set, err := ParseSet(entries)
	if err != nil {
		t.Fatalf("ParseSet(%v) returned error: %v", entries, err)
	}
	return set
}

// TestParseSet_entries covers every accepted entry form plus the rejections.
func TestParseSet_entries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries []string
		wantErr bool
	}{
		{name: "empty list", entries: nil},
		{name: "keywords", entries: []string{"loopback", "private"}},
		{name: "keyword case-insensitive", entries: []string{"LoopBack"}},
		{name: "cidr block", entries: []string{"203.0.113.0/24"}},
		{name: "ipv6 cidr block", entries: []string{"2001:db8::/32"}},
		{name: "single address", entries: []string{"192.0.2.7"}},
		{name: "blank entries ignored", entries: []string{"  ", "10.0.0.1", ""}},
		{name: "hostname rejected", entries: []string{"proxy.example.com"}, wantErr: true},
		{name: "malformed block rejected", entries: []string{"10.0.0.0/99"}, wantErr: true},
		{name: "address with port rejected", entries: []string{"10.0.0.1:80"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseSet(tt.entries)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseSet(%v) error = %v, wantErr %v", tt.entries, err, tt.wantErr)
			}
		})
	}
}

// TestSet_Contains checks membership for both keyword-expanded ranges and
// explicit entries, including the nil set that trusts nothing.
func TestSet_Contains(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		set  *Set
		addr string
		want bool
	}{
		{name: "nil set trusts nothing", set: nil, addr: "127.0.0.1", want: false},
		{name: "empty set trusts nothing", set: mustSet(t), addr: "127.0.0.1", want: false},
		{name: "loopback v4", set: mustSet(t, "loopback"), addr: "127.0.0.1", want: true},
		{name: "loopback v6", set: mustSet(t, "loopback"), addr: "::1", want: true},
		{name: "loopback excludes lan", set: mustSet(t, "loopback"), addr: "10.1.2.3", want: false},
		{name: "private rfc1918", set: mustSet(t, "private"), addr: "172.18.0.4", want: true},
		{name: "private unique-local v6", set: mustSet(t, "private"), addr: "fd00::5", want: true},
		{name: "private excludes cgnat", set: mustSet(t, "private"), addr: "100.100.1.1", want: false},
		{name: "private excludes public", set: mustSet(t, "private"), addr: "203.0.113.9", want: false},
		{name: "explicit block", set: mustSet(t, "203.0.113.0/24"), addr: "203.0.113.9", want: true},
		{name: "explicit host", set: mustSet(t, "192.0.2.7"), addr: "192.0.2.7", want: true},
		{name: "explicit host neighbour", set: mustSet(t, "192.0.2.7"), addr: "192.0.2.8", want: false},
		{name: "ipv4-mapped v6 matches v4 rule", set: mustSet(t, "private"), addr: "::ffff:10.0.0.1", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			addr, err := netip.ParseAddr(tt.addr)
			if err != nil {
				t.Fatalf("ParseAddr(%q): %v", tt.addr, err)
			}
			if got := tt.set.Contains(addr); got != tt.want {
				t.Errorf("Contains(%s) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}

// TestSet_Contains_invalidAddress verifies the zero address is never trusted,
// so a request whose RemoteAddr could not be parsed cannot slip through.
func TestSet_Contains_invalidAddress(t *testing.T) {
	t.Parallel()

	if mustSet(t, "loopback", "private").Contains(netip.Addr{}) {
		t.Error("Contains(invalid address) = true, want false")
	}
}

// TestSet_Empty distinguishes a configured set from one that trusts nothing.
func TestSet_Empty(t *testing.T) {
	t.Parallel()

	var nilSet *Set
	if !nilSet.Empty() {
		t.Error("(*Set)(nil).Empty() = false, want true")
	}
	if !mustSet(t).Empty() {
		t.Error("ParseSet(nil).Empty() = false, want true")
	}
	if mustSet(t, "loopback").Empty() {
		t.Error("ParseSet(loopback).Empty() = true, want false")
	}
}

// resolveVia runs one request through Middleware and reports what FromRequest
// saw inside the handler.
func resolveVia(t *testing.T, trusted *Set, remoteAddr string, headers map[string]string) string {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	var seen string
	handler := Middleware(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = FromRequest(r)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), req)
	return seen
}

// TestMiddleware_resolution is the core of SEC-001: a forwarding header only
// counts when the machine that sent it is a trusted proxy.
func TestMiddleware_resolution(t *testing.T) {
	t.Parallel()

	trusted := mustSet(t, "loopback", "private")

	tests := []struct {
		name       string
		trusted    *Set
		remoteAddr string
		headers    map[string]string
		want       string
	}{
		{
			name:       "untrusted peer forging X-Forwarded-For is ignored",
			trusted:    trusted,
			remoteAddr: "203.0.113.9:44321",
			headers:    map[string]string{"X-Forwarded-For": "10.0.0.77"},
			want:       "203.0.113.9",
		},
		{
			name:       "untrusted peer forging X-Real-Ip is ignored",
			trusted:    trusted,
			remoteAddr: "203.0.113.9:44321",
			headers:    map[string]string{"X-Real-Ip": "10.0.0.77"},
			want:       "203.0.113.9",
		},
		{
			name:       "True-Client-IP is never honoured, even from a trusted proxy",
			trusted:    trusted,
			remoteAddr: "127.0.0.1:9999",
			headers:    map[string]string{"True-Client-IP": "10.0.0.77"},
			want:       "127.0.0.1",
		},
		{
			name:       "trusted proxy's X-Forwarded-For is honoured",
			trusted:    trusted,
			remoteAddr: "172.18.0.2:33445",
			headers:    map[string]string{"X-Forwarded-For": "198.51.100.4"},
			want:       "198.51.100.4",
		},
		{
			name:       "trusted proxy's X-Real-Ip is honoured when there is no chain",
			trusted:    trusted,
			remoteAddr: "127.0.0.1:9999",
			headers:    map[string]string{"X-Real-Ip": "198.51.100.4"},
			want:       "198.51.100.4",
		},
		{
			name:       "chain: the rightmost hop the client could not have written wins",
			trusted:    trusted,
			remoteAddr: "127.0.0.1:9999",
			headers:    map[string]string{"X-Forwarded-For": "198.51.100.4, 203.0.113.9, 10.0.0.2"},
			want:       "203.0.113.9",
		},
		{
			name:       "chain of only trusted hops falls back to the leftmost",
			trusted:    trusted,
			remoteAddr: "127.0.0.1:9999",
			headers:    map[string]string{"X-Forwarded-For": "10.0.0.9, 172.18.0.2"},
			want:       "10.0.0.9",
		},
		{
			name:       "unparseable chain falls back to X-Real-Ip",
			trusted:    trusted,
			remoteAddr: "127.0.0.1:9999",
			headers:    map[string]string{"X-Forwarded-For": "not-an-ip", "X-Real-Ip": "198.51.100.4"},
			want:       "198.51.100.4",
		},
		{
			name:       "unparseable headers fall back to the socket peer",
			trusted:    trusted,
			remoteAddr: "127.0.0.1:9999",
			headers:    map[string]string{"X-Forwarded-For": "nope", "X-Real-Ip": "also-nope"},
			want:       "127.0.0.1",
		},
		{
			name:       "no trusted proxies at all: headers never count",
			trusted:    nil,
			remoteAddr: "127.0.0.1:9999",
			headers:    map[string]string{"X-Forwarded-For": "198.51.100.4"},
			want:       "127.0.0.1",
		},
		{
			name:       "ipv6 peer keeps its address without the port",
			trusted:    trusted,
			remoteAddr: "[2001:db8::5]:44321",
			headers:    map[string]string{"X-Forwarded-For": "198.51.100.4"},
			want:       "2001:db8::5",
		},
		{
			name:       "ipv4-mapped forwarded address is unwrapped",
			trusted:    trusted,
			remoteAddr: "127.0.0.1:9999",
			headers:    map[string]string{"X-Forwarded-For": "::ffff:198.51.100.4"},
			want:       "198.51.100.4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveVia(t, tt.trusted, tt.remoteAddr, tt.headers); got != tt.want {
				t.Errorf("FromRequest = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestMiddleware_rotatingHeaderYieldsOneAddress is the rate-limiter property
// SEC-001 turned on: however the header rotates, an untrusted peer always
// resolves to the one address it dialled from, so it always lands in the same
// limiter bucket.
func TestMiddleware_rotatingHeaderYieldsOneAddress(t *testing.T) {
	t.Parallel()

	trusted := mustSet(t, "loopback", "private")
	forged := []string{"10.0.0.1", "10.0.0.2", "198.51.100.3", "203.0.113.4"}
	for _, value := range forged {
		headers := map[string]string{"X-Forwarded-For": value, "X-Real-Ip": value}
		got := resolveVia(t, trusted, "198.51.100.200:5555", headers)
		if got != "198.51.100.200" {
			t.Errorf("forged %q resolved to %q, want the socket peer 198.51.100.200", value, got)
		}
	}
}

// TestFromRequest_withoutMiddleware verifies the safe fallback: a handler
// mounted outside the middleware chain sees the socket peer, never a header.
func TestFromRequest_withoutMiddleware(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.30:1234"
	req.Header.Set("X-Forwarded-For", "10.9.9.9")
	if got := FromRequest(req); got != "192.0.2.30" {
		t.Errorf("FromRequest = %q, want %q", got, "192.0.2.30")
	}
}

// TestPeer covers the RemoteAddr shapes net/http and tests produce.
func TestPeer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		remoteAddr string
		want       string
	}{
		{name: "host and port", remoteAddr: "192.0.2.30:1234", want: "192.0.2.30"},
		{name: "bracketed ipv6 and port", remoteAddr: "[2001:db8::5]:1234", want: "2001:db8::5"},
		{name: "bare address", remoteAddr: "192.0.2.30", want: "192.0.2.30"},
		{name: "ipv6 zone stripped", remoteAddr: "[fe80::1%eth0]:1234", want: "fe80::1"},
		{name: "ipv4-mapped unwrapped", remoteAddr: "[::ffff:192.0.2.30]:1234", want: "192.0.2.30"},
		{name: "empty", remoteAddr: "", want: ""},
		{name: "unparseable kept as-is", remoteAddr: "pipe", want: "pipe"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if got := Peer(req); got != tt.want {
				t.Errorf("Peer(%q) = %q, want %q", tt.remoteAddr, got, tt.want)
			}
		})
	}
}
