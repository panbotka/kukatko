package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// tokenBytes is the number of random bytes behind each opaque token. 32 bytes
// (256 bits) is comfortably beyond brute-force reach and matches common
// session-token sizing.
const tokenBytes = 32

// newToken returns a cryptographically random, URL-safe opaque token. It is used
// for both the session cookie token and the separate media download token; each
// session gets two independently generated tokens. It returns a wrapped error
// only if the system random source fails.
func newToken() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: reading random bytes for token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
