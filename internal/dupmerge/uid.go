package dupmerge

import (
	"crypto/rand"
	"fmt"
	"strings"
)

const (
	// markerUIDPrefix marks UIDs that identify marker rows, matching the scheme in
	// internal/people so a merge-created tag is indistinguishable from any other
	// marker.
	markerUIDPrefix = "mk"
	// uidSuffixLen is the number of random base32 characters after the prefix,
	// giving ~120 bits of entropy within the markers.uid VARCHAR(32) column.
	uidSuffixLen = 24
	// uidAlphabet is a 32-symbol lowercase base32 alphabet (digits + letters),
	// yielding compact, URL-safe, case-stable UIDs.
	uidAlphabet = "0123456789abcdefghijklmnopqrstuv"
)

// newMarkerUID returns a fresh marker UID of the form "mk" followed by
// uidSuffixLen random base32 characters. It returns a wrapped error only when the
// system random source fails.
func newMarkerUID() (string, error) {
	suffix, err := randomString(uidSuffixLen)
	if err != nil {
		return "", err
	}
	return markerUIDPrefix + suffix, nil
}

// randomString returns a string of n characters drawn uniformly from uidAlphabet
// using crypto/rand. Because the alphabet length (32) divides 256 evenly, masking
// the low 5 bits of each random byte yields an unbiased index.
func randomString(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("dupmerge: reading random bytes: %w", err)
	}
	var sb strings.Builder
	sb.Grow(n)
	for _, b := range buf {
		sb.WriteByte(uidAlphabet[b&0x1f])
	}
	return sb.String(), nil
}
