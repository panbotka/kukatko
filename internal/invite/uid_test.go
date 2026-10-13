package invite

import (
	"strings"
	"testing"
)

// TestNewInviteUID_shape checks that a generated invite UID carries the "iv"
// prefix, fits the column width and uses only alphabet characters, and that
// successive UIDs differ.
func TestNewInviteUID_shape(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool, 50)
	for range 50 {
		uid, err := newInviteUID()
		if err != nil {
			t.Fatalf("newInviteUID: %v", err)
		}
		if !strings.HasPrefix(uid, inviteUIDPrefix) || len(uid) != len(inviteUIDPrefix)+uidSuffixLen ||
			len(uid) > uidMaxLen {
			t.Fatalf("uid %q has the wrong shape", uid)
		}
		for _, r := range uid[len(inviteUIDPrefix):] {
			if !strings.ContainsRune(uidAlphabet, r) {
				t.Fatalf("uid %q has character %q outside the alphabet", uid, r)
			}
		}
		if seen[uid] {
			t.Fatalf("duplicate uid %q", uid)
		}
		seen[uid] = true
	}
}

// TestNewUID_panicsOnLongPrefix asserts an over-long prefix panics, since
// prefixes are compile-time constants and a violation is a programming error.
func TestNewUID_panicsOnLongPrefix(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("newUID with an over-long prefix did not panic")
		}
	}()
	_, _ = newUID(strings.Repeat("x", uidMaxLen))
}
