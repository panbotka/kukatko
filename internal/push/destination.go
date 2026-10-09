package push

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// A subscription's endpoint is a URL the browser hands in, so it is whatever
// the person who subscribed wants it to be. Left alone, the sender would POST
// to it from inside the server's network — to the loopback interface, to the
// tailnet, to a cloud metadata address. Two guards keep it on the public
// internet (SEC-020):
//
//   - the client never follows a redirect (refuseRedirect), so a public
//     endpoint cannot bounce the request somewhere else;
//   - the production client's dialer refuses every non-public address after DNS
//     resolution (refuseNonPublic), so neither an IP literal nor a hostname that
//     resolves — or is later rebound — to an internal address is ever connected
//     to.
//
// Store.Upsert additionally refuses the endpoints that are visibly internal
// (checkEndpointHost) so the person subscribing hears about it at once; that
// check is a courtesy, the dialer is the guard.

const (
	// dialTimeout bounds establishing one TCP connection to a push service.
	dialTimeout = 10 * time.Second
	// tlsHandshakeTimeout bounds the TLS handshake with a push service.
	tlsHandshakeTimeout = 10 * time.Second
	// idleConnTimeout is how long a kept-alive connection to a push service may
	// sit unused before it is closed.
	idleConnTimeout = 90 * time.Second
	// maxIdleConns caps the kept-alive connections across all push services.
	maxIdleConns = 32
)

// errNonPublicDestination indicates the dialer refused an address that is not
// on the public internet. Send reports it as ErrRejected: the endpoint will
// resolve the same way next time, so retrying is pointless.
var errNonPublicDestination = errors.New("push: the endpoint resolves to a non-public address")

// nonPublicPrefixes are ranges netip's predicates do not cover but that must
// never be dialled either: shared address space (CGNAT — which is also where
// the tailnet lives), "this network", IETF protocol assignments, benchmarking,
// the reserved class E block and the local-use NAT64 prefix.
var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("fec0::/10"),
}

// nat64Prefix is the well-known NAT64 prefix (RFC 6052): its last 32 bits are an
// IPv4 address a NAT64 gateway connects to, so that embedded address is what
// has to be public.
var nat64Prefix = netip.MustParsePrefix("64:ff9b::/96")

// isPublicAddr reports whether addr is a globally routable unicast address a
// push service could live at. Loopback, private (RFC 1918 and ULA), link-local,
// unspecified, multicast and the ranges in nonPublicPrefixes are not; an
// IPv4-mapped or NAT64 address is judged by the IPv4 address inside it.
func isPublicAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if nat64Prefix.Contains(addr) {
		raw := addr.As16()
		return isPublicAddr(netip.AddrFrom4([4]byte(raw[12:])))
	}
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return false
	}
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

// refuseNonPublic is the dialer's Control hook. It runs after DNS resolution,
// once per connection attempt, with the concrete address about to be connected
// to, and returns errNonPublicDestination unless that address is public — so a
// hostname that resolves (or is rebound) to an internal address is refused just
// like an IP literal.
func refuseNonPublic(network, address string, _ syscall.RawConn) error {
	addrPort, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("%w: %s %q is not an address", errNonPublicDestination, network, address)
	}
	if !isPublicAddr(addrPort.Addr()) {
		return fmt.Errorf("%w: %s", errNonPublicDestination, addrPort.Addr())
	}
	return nil
}

// refuseRedirect is every sender client's CheckRedirect: it hands the 3xx back
// to Send unfollowed, where classify reports it as ErrRejected. A push service
// answers a delivery with 201; a redirect only ever means the endpoint is not
// one.
func refuseRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

// newDefaultClient returns the client production sends with: no redirects, no
// proxy taken from the environment (a proxy would dial the endpoint itself,
// past the address check) and a dialer that refuses every non-public address.
func newDefaultClient() *http.Client {
	dialer := &net.Dialer{Timeout: dialTimeout, Control: refuseNonPublic}
	return &http.Client{
		Timeout:       DefaultTimeout,
		CheckRedirect: refuseRedirect,
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, address)
			},
			ForceAttemptHTTP2:   true,
			MaxIdleConns:        maxIdleConns,
			IdleConnTimeout:     idleConnTimeout,
			TLSHandshakeTimeout: tlsHandshakeTimeout,
		},
	}
}

// withoutRedirects returns a copy of client that never follows a redirect,
// keeping everything else — so an injected test client still reaches its
// httptest server, but a 3xx from it is a failed delivery like in production.
func withoutRedirects(client *http.Client) *http.Client {
	clone := *client
	clone.CheckRedirect = refuseRedirect
	return &clone
}

// checkEndpointHost refuses, with ErrInvalidSubscription, an endpoint whose host
// is visibly not a push service on the internet: a non-public IP literal, a
// single-label name (localhost, a bare intranet or tailnet host) or a name
// under .localhost. It resolves nothing — a hostname that merely resolves to an
// internal address passes here and is refused by the dialer at send time.
func checkEndpointHost(endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("%w: the endpoint is not an https URL", ErrInvalidSubscription)
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if addr, err := netip.ParseAddr(host); err == nil {
		if !isPublicAddr(addr) {
			return fmt.Errorf("%w: the endpoint's address %s is not on the public internet",
				ErrInvalidSubscription, addr)
		}
		return nil
	}
	if !strings.Contains(host, ".") || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("%w: the endpoint's host %q is not a public name", ErrInvalidSubscription, host)
	}
	return nil
}
