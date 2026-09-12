package family

import (
	"crypto/rand"
	"fmt"
	"strings"
)

const (
	// familyUIDPrefix marks UIDs that identify subject_families rows.
	familyUIDPrefix = "fm"
	// uidSuffixLen is the number of random characters appended after the prefix.
	// At ~5 bits per character this yields ~120 bits of entropy, and with the
	// two-character prefix the total length stays at 26 — within VARCHAR(32).
	uidSuffixLen = 24
	// uidMaxLen is the database column width that generated UIDs must fit in.
	uidMaxLen = 32
	// uidAlphabet is a 32-symbol lowercase base32 alphabet (digits + letters, no
	// padding) giving compact, URL-safe, case-stable UIDs.
	uidAlphabet = "0123456789abcdefghijklmnopqrstuv"
)

// newFamilyUID returns a fresh UID for a family row, of the form "fm" followed
// by uidSuffixLen random base32 characters. It returns a wrapped error only if
// the system random source fails.
func newFamilyUID() (string, error) {
	return newUID(familyUIDPrefix)
}

// newUID returns a UID of the form prefix + uidSuffixLen random base32 chars. It
// panics if the prefix is too long to fit within uidMaxLen, since prefixes are
// compile-time constants and a violation is a programming error.
func newUID(prefix string) (string, error) {
	if len(prefix)+uidSuffixLen > uidMaxLen {
		panic(fmt.Sprintf("family: uid prefix %q too long for VARCHAR(%d)", prefix, uidMaxLen))
	}
	suffix, err := randomString(uidSuffixLen)
	if err != nil {
		return "", err
	}
	return prefix + suffix, nil
}

// randomString returns a string of n characters drawn uniformly from uidAlphabet
// using crypto/rand. Because the alphabet length (32) divides 256 evenly, masking
// the low 5 bits of each random byte yields an unbiased index.
func randomString(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("family: reading random bytes: %w", err)
	}
	var sb strings.Builder
	sb.Grow(n)
	for _, b := range buf {
		sb.WriteByte(uidAlphabet[b&0x1f])
	}
	return sb.String(), nil
}
