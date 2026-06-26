package auth

import (
	"encoding/base64"
	"testing"
)

// TestNewToken_uniqueAndDecodable verifies tokens are unique across a batch and
// decode back to the expected number of random bytes.
func TestNewToken_uniqueAndDecodable(t *testing.T) {
	t.Parallel()

	const n = 1000
	seen := make(map[string]bool, n)
	for range n {
		tok, err := newToken()
		if err != nil {
			t.Fatalf("newToken: %v", err)
		}
		if seen[tok] {
			t.Fatalf("duplicate token generated: %q", tok)
		}
		seen[tok] = true

		raw, err := base64.RawURLEncoding.DecodeString(tok)
		if err != nil {
			t.Fatalf("token %q is not valid base64url: %v", tok, err)
		}
		if len(raw) != tokenBytes {
			t.Errorf("decoded token length = %d, want %d", len(raw), tokenBytes)
		}
	}
}
