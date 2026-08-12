// Package clientip resolves the address a request really came from and makes it
// available to everything that keys on the caller: the rate limiters, the audit
// trail and the access log.
//
// A forwarding header (`X-Forwarded-For`, `X-Real-Ip`) is just request data —
// any client can send one. Honouring it unconditionally, as chi's
// middleware.RealIP does, lets an anonymous caller pick its own apparent address
// and so hand itself a fresh rate-limit bucket per request and a forged source
// IP in the audit trail. This package therefore reads those headers **only when
// the immediate socket peer is a trusted proxy**; from anyone else the transport
// address wins and the headers are ignored. `True-Client-IP` and the vendor
// variants are never consulted at all: no proxy in this deployment sets them,
// and a header nobody writes is a header nobody has to be trusted about.
//
// Middleware resolves the address once per request and stores it on the context;
// FromRequest reads it back. FromRequest without the middleware falls back to the
// socket peer, which is the safe answer — a handler mounted outside the server's
// middleware chain (a test, another router) can never be tricked into believing
// a header.
package clientip

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Keywords accepted by ParseSet in place of a CIDR, naming the address ranges a
// reverse proxy realistically runs in.
const (
	// KeywordLoopback expands to the IPv4 and IPv6 loopback ranges — a proxy on
	// the same host.
	KeywordLoopback = "loopback"
	// KeywordPrivate expands to the RFC 1918 ranges and IPv6 unique-local
	// addresses — a proxy on the same Docker network or LAN. It deliberately does
	// not include the 100.64.0.0/10 carrier-grade NAT range that Tailscale uses:
	// a tailnet is a network of clients, not of proxies.
	KeywordPrivate = "private"
)

// loopbackPrefixes are the ranges KeywordLoopback expands to.
var loopbackPrefixes = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("::1/128"),
}

// privatePrefixes are the ranges KeywordPrivate expands to.
var privatePrefixes = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("fc00::/7"),
}

// Set is a set of networks whose members are trusted to name the real client in
// a forwarding header. The zero value and a nil *Set trust nothing, which makes
// "no configuration" mean "believe only the socket".
type Set struct {
	prefixes []netip.Prefix
}

// ParseSet builds a Set from configuration entries. Each entry is either a CIDR
// block ("10.0.0.0/8"), a single address ("192.0.2.7"), or one of the keywords
// KeywordLoopback / KeywordPrivate. Entries are trimmed and empty ones ignored,
// so a list from an environment variable with stray spaces still parses. It
// returns an error naming the offending entry if one is neither a keyword nor a
// valid address or block.
func ParseSet(entries []string) (*Set, error) {
	set := &Set{}
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		prefixes, err := parseEntry(entry)
		if err != nil {
			return nil, err
		}
		set.prefixes = append(set.prefixes, prefixes...)
	}
	return set, nil
}

// parseEntry expands one configuration entry into the prefixes it names,
// returning an error that quotes the entry when it parses as nothing.
func parseEntry(entry string) ([]netip.Prefix, error) {
	switch strings.ToLower(entry) {
	case KeywordLoopback:
		return loopbackPrefixes, nil
	case KeywordPrivate:
		return privatePrefixes, nil
	}
	if strings.Contains(entry, "/") {
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			return nil, fmt.Errorf("clientip: trusted proxy %q is not a valid CIDR block: %w", entry, err)
		}
		return []netip.Prefix{prefix.Masked()}, nil
	}
	addr, err := netip.ParseAddr(entry)
	if err != nil {
		return nil, fmt.Errorf("clientip: trusted proxy %q is not a valid address, block or keyword: %w", entry, err)
	}
	normalized := normalize(addr)
	return []netip.Prefix{netip.PrefixFrom(normalized, normalized.BitLen())}, nil
}

// Contains reports whether addr is one of the trusted proxies. A nil Set (no
// configuration) contains nothing, and an invalid address is never trusted.
func (s *Set) Contains(addr netip.Addr) bool {
	if s == nil || !addr.IsValid() {
		return false
	}
	normalized := normalize(addr)
	for _, prefix := range s.prefixes {
		if prefix.Contains(normalized) {
			return true
		}
	}
	return false
}

// Empty reports whether the set trusts no proxy at all, so a caller can log the
// distinction between "headers are ignored" and "headers from N networks count".
func (s *Set) Empty() bool {
	return s == nil || len(s.prefixes) == 0
}

// contextKey is an unexported type for this package's context keys so they
// cannot collide with keys from other packages.
type contextKey int

// clientIPContextKey addresses the resolved client IP on a request context.
const clientIPContextKey contextKey = iota

// Middleware returns net/http middleware that resolves the client address once
// per request against trusted and stores it on the request context for
// FromRequest. It must be installed ahead of any middleware or handler that
// keys on the caller (rate limiters, access log, audit trail); a nil trusted set
// makes every request resolve to its socket peer.
func Middleware(trusted *Set) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), clientIPContextKey, resolve(r, trusted))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// FromRequest returns the client address resolved by Middleware, or — when the
// middleware is not installed — the request's socket peer. The result is a bare
// address without a port, and is the empty string only if the request carries no
// usable RemoteAddr at all.
func FromRequest(r *http.Request) string {
	if ip, ok := r.Context().Value(clientIPContextKey).(string); ok {
		return ip
	}
	return Peer(r)
}

// Peer returns the host part of the request's RemoteAddr — the address of the
// machine that actually opened the connection, ignoring every header. A
// RemoteAddr without a port is returned as-is (trimmed).
func Peer(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	if addr, parseErr := netip.ParseAddr(host); parseErr == nil {
		return normalize(addr).String()
	}
	return host
}

// resolve computes the effective client address for r: the socket peer unless
// that peer is a trusted proxy, in which case the forwarding headers it set are
// believed.
func resolve(r *http.Request, trusted *Set) string {
	peer := Peer(r)
	addr, err := netip.ParseAddr(peer)
	if err != nil || !trusted.Contains(addr) {
		return peer
	}
	if forwarded := rightmostUntrusted(r.Header.Get("X-Forwarded-For"), trusted); forwarded != "" {
		return forwarded
	}
	if realIP, ok := parseHeaderAddr(r.Header.Get("X-Real-Ip")); ok {
		return realIP
	}
	return peer
}

// rightmostUntrusted walks an X-Forwarded-For chain from the right (the hop
// closest to us, which the trusted proxy appended and therefore observed itself)
// towards the left (the hop closest to the client, which anyone may have forged)
// and returns the first address that is not itself a trusted proxy. Unparseable
// hops are skipped. When every hop is a trusted proxy the leftmost one is
// returned — it is still an address our own infrastructure vouched for — and an
// absent or entirely unparseable header yields the empty string.
func rightmostUntrusted(header string, trusted *Set) string {
	if header == "" {
		return ""
	}
	hops := strings.Split(header, ",")
	leftmost := ""
	for i := len(hops) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			continue
		}
		leftmost = normalize(addr).String()
		if !trusted.Contains(addr) {
			return leftmost
		}
	}
	return leftmost
}

// parseHeaderAddr parses a single-address header value, reporting whether it
// held a valid address.
func parseHeaderAddr(value string) (string, bool) {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return "", false
	}
	return normalize(addr).String(), true
}

// normalize strips an IPv6 zone and unwraps an IPv4-mapped IPv6 address, so the
// same machine yields the same key whether it appears as "::ffff:10.0.0.1",
// "10.0.0.1" or "fe80::1%eth0".
func normalize(addr netip.Addr) netip.Addr {
	return addr.Unmap().WithZone("")
}
