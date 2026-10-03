package uploadlink

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// CodeLen is the length of a link's short code.
	CodeLen = 8
	// codeAlphabet is the code's 55 symbols: digits and both letter cases with
	// every glyph a person could misread dropped (0/O/o, 1/l/I). Eight of them
	// carry ~46 bits — far beyond what a rate-limited caller can guess, and
	// short enough to read off a screen.
	codeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz"
	// codeRejectAbove is the largest multiple of len(codeAlphabet) that fits in
	// a byte; a random byte at or above it is redrawn, so every symbol is
	// equally likely (no modulo bias).
	codeRejectAbove = 256 / len(codeAlphabet) * len(codeAlphabet)
	// sessionTokenBytes is the entropy of an anonymous uploader's session token.
	sessionTokenBytes = 32
)

// NewCode returns a fresh random short code of CodeLen symbols from the
// unambiguous alphabet. It fails only when the system random source does.
func NewCode() (string, error) {
	var sb strings.Builder
	sb.Grow(CodeLen)
	buf := make([]byte, CodeLen*2)
	for sb.Len() < CodeLen {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("uploadlink: reading random bytes: %w", err)
		}
		for _, b := range buf {
			if int(b) >= codeRejectAbove {
				continue
			}
			sb.WriteByte(codeAlphabet[int(b)%len(codeAlphabet)])
			if sb.Len() == CodeLen {
				break
			}
		}
	}
	return sb.String(), nil
}

// ValidCode reports whether code has the shape of a short code — the right
// length, only alphabet symbols. A malformed code is refused before it costs a
// database lookup.
func ValidCode(code string) bool {
	if len(code) != CodeLen {
		return false
	}
	for i := range len(code) {
		if !strings.ContainsRune(codeAlphabet, rune(code[i])) {
			return false
		}
	}
	return true
}

// HashSecret returns the lowercase hex SHA-256 of secret. It hashes both a
// link's code and an anonymous session token: each is random and high-entropy
// enough that a plain digest (no salt, no work factor) is the right tool, as it
// is for API tokens, and it keeps the lookup an indexed equality.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// NewSessionToken returns a fresh random token identifying one anonymous
// uploader's browser session, base64url without padding. It fails only when the
// system random source does.
func NewSessionToken() (string, error) {
	buf := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("uploadlink: reading random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// NormalizeUploaderName trims the "from whom" name an uploader typed, collapses
// its inner whitespace to single spaces and cuts it to MaxUploaderNameLen
// characters. Invalid UTF-8 is dropped rather than stored.
func NormalizeUploaderName(name string) string {
	name = strings.ToValidUTF8(name, "")
	name = strings.Join(strings.Fields(name), " ")
	if utf8.RuneCountInString(name) <= MaxUploaderNameLen {
		return name
	}
	return strings.TrimSpace(string([]rune(name)[:MaxUploaderNameLen]))
}
