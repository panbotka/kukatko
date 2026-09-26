package notification

import (
	"strings"
	"testing"
)

// TestNewNotificationUIDShape checks that a generated notification UID carries the
// right prefix, fits the column width, and uses only alphabet characters.
func TestNewNotificationUIDShape(t *testing.T) {
	t.Parallel()

	uid, err := newNotificationUID()
	if err != nil {
		t.Fatalf("newNotificationUID: %v", err)
	}
	if !strings.HasPrefix(uid, notificationUIDPrefix) {
		t.Errorf("uid %q missing prefix %q", uid, notificationUIDPrefix)
	}
	if len(uid) != len(notificationUIDPrefix)+uidSuffixLen || len(uid) > uidMaxLen {
		t.Errorf("uid %q has length %d, want %d", uid, len(uid), len(notificationUIDPrefix)+uidSuffixLen)
	}
	for _, r := range uid[len(notificationUIDPrefix):] {
		if !strings.ContainsRune(uidAlphabet, r) {
			t.Errorf("uid %q has character %q outside alphabet", uid, r)
		}
	}
}

// TestNewNotificationUIDUnique checks that successive UIDs differ (collisions are
// astronomically unlikely with 120 bits of entropy).
func TestNewNotificationUIDUnique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool, 100)
	for range 100 {
		uid, err := newNotificationUID()
		if err != nil {
			t.Fatalf("newNotificationUID: %v", err)
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
