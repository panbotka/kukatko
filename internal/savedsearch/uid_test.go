package savedsearch

import (
	"strings"
	"testing"
)

// TestNewSavedSearchUIDShape checks that a generated saved-search UID carries the
// right prefix, fits the column width, and uses only alphabet characters.
func TestNewSavedSearchUIDShape(t *testing.T) {
	t.Parallel()

	uid, err := newSavedSearchUID()
	if err != nil {
		t.Fatalf("newSavedSearchUID: %v", err)
	}
	if !strings.HasPrefix(uid, savedSearchUIDPrefix) {
		t.Errorf("uid %q missing prefix %q", uid, savedSearchUIDPrefix)
	}
	if len(uid) != len(savedSearchUIDPrefix)+uidSuffixLen || len(uid) > uidMaxLen {
		t.Errorf("uid %q has length %d, want %d", uid, len(uid), len(savedSearchUIDPrefix)+uidSuffixLen)
	}
	for _, r := range uid[len(savedSearchUIDPrefix):] {
		if !strings.ContainsRune(uidAlphabet, r) {
			t.Errorf("uid %q has character %q outside alphabet", uid, r)
		}
	}
}

// TestNewSavedSearchUIDUnique checks that successive UIDs differ (collisions are
// astronomically unlikely with 120 bits of entropy).
func TestNewSavedSearchUIDUnique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool, 100)
	for range 100 {
		uid, err := newSavedSearchUID()
		if err != nil {
			t.Fatalf("newSavedSearchUID: %v", err)
		}
		if seen[uid] {
			t.Fatalf("duplicate uid %q", uid)
		}
		seen[uid] = true
	}
}

// TestNewUIDPanicsOnLongPrefix asserts that an over-long prefix panics, since
// prefixes are compile-time constants and a violation is a programming error.
func TestNewUIDPanicsOnLongPrefix(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("newUID did not panic on an over-long prefix")
		}
	}()
	_, _ = newUID(strings.Repeat("x", uidMaxLen))
}
