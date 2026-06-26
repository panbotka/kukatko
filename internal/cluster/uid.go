package cluster

import (
	"crypto/rand"
	"fmt"
	"strings"
)

const (
	// clusterUIDPrefix marks UIDs that identify face_clusters rows.
	clusterUIDPrefix = "fc"
	// uidSuffixLen is the number of random characters appended after the prefix,
	// matching the people package so cluster uids share the 26-character, ~120-bit
	// shape and fit comfortably within VARCHAR(32).
	uidSuffixLen = 24
	// uidMaxLen is the database column width generated uids must fit in.
	uidMaxLen = 32
	// uidAlphabet is a 32-symbol lowercase base32 alphabet (digits + letters, no
	// padding) giving compact, URL-safe, case-stable uids.
	uidAlphabet = "0123456789abcdefghijklmnopqrstuv"
)

// newClusterUID returns a fresh uid for a face_clusters row, of the form "fc"
// followed by uidSuffixLen random base32 characters. It returns a wrapped error
// only if the system random source fails.
func newClusterUID() (string, error) {
	if len(clusterUIDPrefix)+uidSuffixLen > uidMaxLen {
		panic(fmt.Sprintf("cluster: uid prefix %q too long for VARCHAR(%d)", clusterUIDPrefix, uidMaxLen))
	}
	suffix, err := randomString(uidSuffixLen)
	if err != nil {
		return "", err
	}
	return clusterUIDPrefix + suffix, nil
}

// randomString returns a string of n characters drawn uniformly from uidAlphabet
// using crypto/rand. The alphabet length (32) divides 256 evenly, so masking the
// low 5 bits of each random byte yields an unbiased index.
func randomString(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("cluster: reading random bytes: %w", err)
	}
	var sb strings.Builder
	sb.Grow(n)
	for _, b := range buf {
		sb.WriteByte(uidAlphabet[b&0x1f])
	}
	return sb.String(), nil
}
