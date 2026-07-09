package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// Sentinel errors for the API-token flow.
var (
	// ErrAPITokenNotFound indicates no token row matched the given id.
	ErrAPITokenNotFound = errors.New("auth: api token not found")
	// ErrInvalidAPIToken is returned for every failed bearer authentication —
	// malformed, unknown, wrong secret, revoked, expired, or owned by a disabled
	// user. It is intentionally unspecific: the caller must not learn which of
	// those it was, so the HTTP layer answers 401 with one generic message.
	ErrInvalidAPIToken = errors.New("auth: invalid api token")
	// ErrAPITokenNameRequired indicates an empty human-readable token name.
	ErrAPITokenNameRequired = errors.New("auth: api token name is required")
	// ErrAPITokenExpiryInPast indicates a requested expiry that has already passed.
	ErrAPITokenExpiryInPast = errors.New("auth: api token expiry must be in the future")
)

const (
	// apiTokenScheme prefixes every plaintext API token, so a leaked credential
	// is recognisable on sight (and by secret scanners) as a Kukátko token.
	apiTokenScheme = "kkt"
	// apiTokenParts is the number of underscore-separated fields in a plaintext
	// token: scheme, id, secret. The secret's alphabet may itself contain "_",
	// so parsing splits at most this many times and keeps the remainder.
	apiTokenParts = 3
	// apiTokenNameMaxLen bounds the human-readable name.
	apiTokenNameMaxLen = 100
	// apiTokenUseInterval is the minimum time between two writes of a token's
	// last_used_at. It mirrors slidingRenewInterval: a busy client would
	// otherwise write on every single request just to record activity.
	apiTokenUseInterval = time.Minute
)

// APIToken is a long-lived bearer credential belonging to a user. The token
// inherits its owner's role at authentication time, so no role is stored here.
// SecretHash never leaves the server and the plaintext secret is returned
// exactly once, by Service.CreateAPIToken.
type APIToken struct {
	ID         string     `json:"id"`
	UserUID    string     `json:"user_uid"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	// SecretHash holds the hex-encoded SHA-256 of the secret; excluded from JSON.
	SecretHash string `json:"-"`
}

// Revoked reports whether the token has been revoked.
func (t APIToken) Revoked() bool {
	return t.RevokedAt != nil
}

// Expired reports whether the token's expiry has passed as of now. A token
// without an expiry never expires. The boundary instant counts as expired, so a
// token is usable strictly before expires_at.
func (t APIToken) Expired(now time.Time) bool {
	return t.ExpiresAt != nil && !t.ExpiresAt.After(now)
}

// Active reports whether the token may authenticate a request as of now: it is
// neither revoked nor expired.
func (t APIToken) Active(now time.Time) bool {
	return !t.Revoked() && !t.Expired(now)
}

// shouldRecordUse reports whether the token's last_used_at is stale enough to be
// rewritten as of now. A token that has never been used is always recorded.
func (t APIToken) shouldRecordUse(now time.Time) bool {
	return t.LastUsedAt == nil || now.Sub(*t.LastUsedAt) >= apiTokenUseInterval
}

// newAPITokenID returns a fresh id for an api_tokens row. It doubles as the
// lookup key embedded in the plaintext token, which is why it is drawn from
// uidAlphabet: no underscore, so the plaintext parses unambiguously.
func newAPITokenID() (string, error) {
	return newUID(apiTokenIDPrefix)
}

// generateAPIToken returns a fresh token id, the hex-encoded hash to store, and
// the plaintext credential to hand to the caller exactly once. The secret
// carries 256 bits from crypto/rand.
func generateAPIToken() (id, secretHash, plaintext string, err error) {
	id, err = newAPITokenID()
	if err != nil {
		return "", "", "", err
	}
	secret, err := newToken()
	if err != nil {
		return "", "", "", err
	}
	return id, hashAPITokenSecret(secret), formatAPIToken(id, secret), nil
}

// hashAPITokenSecret returns the hex-encoded SHA-256 of secret.
//
// This is deliberately *not* bcrypt, even though passwords in this package are.
// Bcrypt's cost exists to make dictionary attacks on low-entropy, human-chosen
// secrets expensive, and it is paid once per login. An API token's secret is 256
// bits from a cryptographically secure source — there is no dictionary to
// attack, so the cost buys nothing — and it is verified on *every* API request,
// where a deliberately slow hash would be a self-inflicted denial of service.
// A single SHA-256 over a full-entropy secret is the right primitive here.
// Please do not "fix" this to bcrypt.
func hashAPITokenSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// apiTokenSecretMatches reports whether secret hashes to storedHash, comparing
// in constant time so a caller cannot recover the stored hash byte by byte from
// response timing.
func apiTokenSecretMatches(storedHash, secret string) bool {
	candidate := hashAPITokenSecret(secret)
	return subtle.ConstantTimeCompare([]byte(storedHash), []byte(candidate)) == 1
}

// formatAPIToken assembles the plaintext credential handed to the client:
// "kkt_<id>_<secret>".
func formatAPIToken(id, secret string) string {
	return apiTokenScheme + "_" + id + "_" + secret
}

// parseAPIToken splits a plaintext credential into its token id and secret. It
// returns ErrInvalidAPIToken for anything that is not "kkt_<id>_<secret>" with
// both fields non-empty. The secret is the entire remainder after the second
// separator, so an underscore inside it is preserved.
func parseAPIToken(plaintext string) (id, secret string, err error) {
	parts := strings.SplitN(plaintext, "_", apiTokenParts)
	if len(parts) != apiTokenParts {
		return "", "", ErrInvalidAPIToken
	}
	if parts[0] != apiTokenScheme || parts[1] == "" || parts[2] == "" {
		return "", "", ErrInvalidAPIToken
	}
	return parts[1], parts[2], nil
}

// normalizeAPITokenName trims the name and validates it, returning
// ErrAPITokenNameRequired when it is empty and truncating at
// apiTokenNameMaxLen so an oversized label cannot bloat the row.
func normalizeAPITokenName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", ErrAPITokenNameRequired
	}
	if len(trimmed) > apiTokenNameMaxLen {
		trimmed = trimmed[:apiTokenNameMaxLen]
	}
	return trimmed, nil
}

// bearerPrefix is the RFC 6750 credential scheme, matched case-insensitively as
// RFC 7235 requires.
const bearerPrefix = "bearer "

// bearerToken extracts the credential from an "Authorization: Bearer <token>"
// header, reporting whether one was present. A missing header, a different
// scheme, or an empty credential all report false, which lets the caller fall
// through to cookie authentication unchanged.
func bearerToken(header string) (string, bool) {
	if len(header) < len(bearerPrefix) ||
		!strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(bearerPrefix):])
	return token, token != ""
}
