package storage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"time"
)

const (
	// schemeHTTP and schemeHTTPS are the only URL schemes this package accepts, for
	// a media base URL as well as for an S3 endpoint.
	schemeHTTP  = "http"
	schemeHTTPS = "https"
	// QueryExpires is the query parameter carrying the Unix expiry timestamp of a
	// signed media URL.
	QueryExpires = "exp"
	// QuerySignature is the query parameter carrying the hex-encoded HMAC of a
	// signed media URL.
	QuerySignature = "sig"
	// DefaultURLTTL is the lifetime of a signed URL when none is configured. It is
	// short on purpose: a leaked URL stays useful for at most this long, and photo
	// payloads carry freshly signed URLs on every API response.
	DefaultURLTTL = time.Hour
)

// Signing errors, matchable with errors.Is.
var (
	// ErrInvalidSignature indicates the signature is absent, malformed, or does
	// not match any accepted signing secret. A tampered object key or expiry
	// surfaces as this error rather than ErrURLExpired.
	ErrInvalidSignature = errors.New("storage: invalid URL signature")
	// ErrURLExpired indicates a correctly signed URL whose expiry has passed.
	ErrURLExpired = errors.New("storage: signed URL expired")
	// ErrInvalidBaseURL indicates the configured media base URL is not an absolute
	// http(s) URL.
	ErrInvalidBaseURL = errors.New("storage: invalid media base URL")
	// ErrMissingSigningSecret indicates a media base URL was configured without a
	// signing secret, which would hand out URLs no edge Worker could authorize.
	ErrMissingSigningSecret = errors.New("storage: URL signing secret is required")
)

// URLSigner mints and verifies the short-lived, signed media URLs that let a
// browser fetch a private object straight from the edge without touching the
// application:
//
//	https://<media-base-url>/<object-key>?exp=<unix-seconds>&sig=<hex HMAC-SHA256>
//
// The signature is HMAC-SHA256 over the object key, a newline, and the decimal
// expiry — that is, over exactly the two things the edge Worker must be unable to
// forge. The key is signed unescaped; only its rendering into the URL path is
// percent-encoded, so a UTF-8 filename signs and verifies unchanged.
//
// Two secrets are accepted on verification, the current one and the previous one,
// so a secret can be rotated without invalidating URLs already handed out.
// Signing always uses the current secret. The zero value is not usable; call
// NewURLSigner.
type URLSigner struct {
	base *url.URL
	// secrets holds the accepted signing secrets, the current one first. Its
	// contents never appear in a log line or an error.
	secrets [][]byte
	ttl     time.Duration
	// now is time.Now in production and a fixed clock in tests.
	now func() time.Time
}

// NewURLSigner returns a signer that mints URLs under baseURL (the media domain
// the edge Worker serves, optionally with a path prefix) signed with secret.
// previousSecret, when non-empty, is additionally accepted by Verify so a secret
// rotation has no window of broken URLs. A non-positive ttl falls back to
// DefaultURLTTL.
//
// It returns ErrInvalidBaseURL when baseURL is not an absolute http(s) URL and
// ErrMissingSigningSecret when secret is empty.
func NewURLSigner(baseURL, secret, previousSecret string, ttl time.Duration) (*URLSigner, error) {
	base, err := parseBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if secret == "" {
		return nil, ErrMissingSigningSecret
	}
	if ttl <= 0 {
		ttl = DefaultURLTTL
	}
	secrets := [][]byte{[]byte(secret)}
	if previousSecret != "" {
		secrets = append(secrets, []byte(previousSecret))
	}
	return &URLSigner{base: base, secrets: secrets, ttl: ttl, now: time.Now}, nil
}

// parseBaseURL validates that raw is an absolute http(s) URL with a host and
// returns it stripped of any query or fragment.
func parseBaseURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, fmt.Errorf("%w: empty", ErrInvalidBaseURL)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidBaseURL, err)
	}
	if parsed.Scheme != schemeHTTP && parsed.Scheme != schemeHTTPS {
		return nil, fmt.Errorf("%w: scheme %q must be http or https", ErrInvalidBaseURL, parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("%w: missing host", ErrInvalidBaseURL)
	}
	return &url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: parsed.Path}, nil
}

// SignedURL returns the signed, client-fetchable URL of the object at key, valid
// for the signer's TTL from now. key must already be a canonical object key
// (slash-separated, no leading slash).
func (s *URLSigner) SignedURL(key string) string {
	expiry := s.now().Add(s.ttl).Unix()
	target := *s.base
	target.Path = path.Join("/", s.base.Path, key)
	target.RawQuery = url.Values{
		QueryExpires:   {strconv.FormatInt(expiry, 10)},
		QuerySignature: {hex.EncodeToString(sign(s.secrets[0], key, expiry))},
	}.Encode()
	return target.String()
}

// Verify checks a signed URL's query parameters against key, which must be the
// decoded object key. expiry and signature are the raw exp and sig query values.
//
// It returns ErrInvalidSignature when the signature is malformed or matches
// neither the current nor the previous secret — including when the key or the
// expiry was tampered with, since both are covered by the HMAC — and ErrURLExpired
// when a genuinely signed URL has aged out.
func (s *URLSigner) Verify(key, expiry, signature string) error {
	expiresAt, err := strconv.ParseInt(expiry, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: malformed expiry", ErrInvalidSignature)
	}
	presented, err := hex.DecodeString(signature)
	if err != nil || len(presented) == 0 {
		return fmt.Errorf("%w: malformed signature", ErrInvalidSignature)
	}
	if !s.matches(key, expiresAt, presented) {
		return ErrInvalidSignature
	}
	if s.now().After(time.Unix(expiresAt, 0)) {
		return fmt.Errorf("%w at %s", ErrURLExpired, time.Unix(expiresAt, 0).UTC().Format(time.RFC3339))
	}
	return nil
}

// matches reports whether presented is the signature of key and expiresAt under
// any accepted secret. Every candidate is compared in constant time, and all of
// them are compared even after a match, so the answer leaks neither the
// signature's bytes nor which secret signed it.
func (s *URLSigner) matches(key string, expiresAt int64, presented []byte) bool {
	matched := false
	for _, secret := range s.secrets {
		if hmac.Equal(presented, sign(secret, key, expiresAt)) {
			matched = true
		}
	}
	return matched
}

// sign returns the raw HMAC-SHA256 of the object key and its expiry under secret.
// The message is "<key>\n<expiry>"; the newline separator keeps a key ending in
// digits from colliding with a different key and expiry.
func sign(secret []byte, key string, expiresAt int64) []byte {
	mac := hmac.New(sha256.New, secret)
	// hash.Hash never reports a write error.
	_, _ = mac.Write([]byte(key))
	_, _ = mac.Write([]byte{'\n'})
	_, _ = mac.Write([]byte(strconv.FormatInt(expiresAt, 10)))
	return mac.Sum(nil)
}
