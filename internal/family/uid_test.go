package family

import (
	"strings"
	"testing"
)

func TestNewFamilyUID_shapeAndUniqueness(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool, 100)
	for range 100 {
		uid, err := newFamilyUID()
		if err != nil {
			t.Fatalf("newFamilyUID: %v", err)
		}
		if !strings.HasPrefix(uid, familyUIDPrefix) {
			t.Fatalf("uid %q does not start with %q", uid, familyUIDPrefix)
		}
		if len(uid) != len(familyUIDPrefix)+uidSuffixLen {
			t.Fatalf("uid %q has length %d, want %d", uid, len(uid), len(familyUIDPrefix)+uidSuffixLen)
		}
		if len(uid) > uidMaxLen {
			t.Fatalf("uid %q is longer than the VARCHAR(%d) column", uid, uidMaxLen)
		}
		for _, r := range uid[len(familyUIDPrefix):] {
			if !strings.ContainsRune(uidAlphabet, r) {
				t.Fatalf("uid %q carries %q, which is outside the alphabet", uid, r)
			}
		}
		if seen[uid] {
			t.Fatalf("newFamilyUID returned %q twice", uid)
		}
		seen[uid] = true
	}
}

func TestNewUID_panicsOnAnOverlongPrefix(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("newUID with an overlong prefix did not panic")
		}
	}()
	_, _ = newUID(strings.Repeat("x", uidMaxLen))
}
