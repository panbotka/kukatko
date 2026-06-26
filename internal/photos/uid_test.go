package photos

import (
	"strings"
	"testing"
)

// TestNewPhotoUID_shape verifies the generated UID has the photo prefix, the
// expected length, and only alphabet characters.
func TestNewPhotoUID_shape(t *testing.T) {
	t.Parallel()

	uid, err := newPhotoUID()
	if err != nil {
		t.Fatalf("newPhotoUID() error = %v", err)
	}
	if want := len(photoUIDPrefix) + uidSuffixLen; len(uid) != want {
		t.Errorf("len(uid) = %d, want %d (uid=%q)", len(uid), want, uid)
	}
	if len(uid) > uidMaxLen {
		t.Errorf("uid %q longer than VARCHAR(%d)", uid, uidMaxLen)
	}
	if !strings.HasPrefix(uid, photoUIDPrefix) {
		t.Errorf("uid %q does not start with %q", uid, photoUIDPrefix)
	}
	for _, r := range uid[len(photoUIDPrefix):] {
		if !strings.ContainsRune(uidAlphabet, r) {
			t.Errorf("uid %q contains out-of-alphabet rune %q", uid, r)
		}
	}
}

// TestNewPhotoUID_unique verifies that successive UIDs differ.
func TestNewPhotoUID_unique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{}, 100)
	for range 100 {
		uid, err := newPhotoUID()
		if err != nil {
			t.Fatalf("newPhotoUID() error = %v", err)
		}
		if _, dup := seen[uid]; dup {
			t.Fatalf("duplicate uid generated: %q", uid)
		}
		seen[uid] = struct{}{}
	}
}

// TestRandomString_length verifies randomString returns exactly n alphabet
// characters, including the empty-string edge case.
func TestRandomString_length(t *testing.T) {
	t.Parallel()

	for _, n := range []int{0, 1, 24, 64} {
		got, err := randomString(n)
		if err != nil {
			t.Fatalf("randomString(%d) error = %v", n, err)
		}
		if len(got) != n {
			t.Errorf("len(randomString(%d)) = %d, want %d", n, len(got), n)
		}
		for _, r := range got {
			if !strings.ContainsRune(uidAlphabet, r) {
				t.Errorf("randomString(%d) = %q contains out-of-alphabet rune %q", n, got, r)
			}
		}
	}
}
