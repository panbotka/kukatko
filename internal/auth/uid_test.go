package auth

import (
	"strings"
	"testing"
)

// TestNewUID_formatAndLength verifies generated UIDs keep their prefix, fit the
// column width, and use only the defined alphabet.
func TestNewUID_formatAndLength(t *testing.T) {
	t.Parallel()

	generators := []struct {
		name   string
		prefix string
		gen    func() (string, error)
	}{
		{"user", userUIDPrefix, newUserUID},
		{"session", sessionIDPrefix, newSessionID},
	}
	for _, g := range generators {
		t.Run(g.name, func(t *testing.T) {
			t.Parallel()
			uid, err := g.gen()
			if err != nil {
				t.Fatalf("%s uid: %v", g.name, err)
			}
			if !strings.HasPrefix(uid, g.prefix) {
				t.Errorf("uid %q does not start with prefix %q", uid, g.prefix)
			}
			if want := len(g.prefix) + uidSuffixLen; len(uid) != want {
				t.Errorf("len(uid) = %d, want %d", len(uid), want)
			}
			if len(uid) > uidMaxLen {
				t.Errorf("uid %q exceeds VARCHAR(%d)", uid, uidMaxLen)
			}
			for _, c := range uid[len(g.prefix):] {
				if !strings.ContainsRune(uidAlphabet, c) {
					t.Errorf("uid suffix contains char %q outside alphabet", c)
				}
			}
		})
	}
}

// TestNewUID_unique verifies a batch of generated UIDs contains no collisions.
func TestNewUID_unique(t *testing.T) {
	t.Parallel()

	const n = 1000
	seen := make(map[string]bool, n)
	for range n {
		uid, err := newUserUID()
		if err != nil {
			t.Fatalf("newUserUID: %v", err)
		}
		if seen[uid] {
			t.Fatalf("duplicate uid generated: %q", uid)
		}
		seen[uid] = true
	}
}

// TestRandomString_length verifies the helper returns exactly n characters.
func TestRandomString_length(t *testing.T) {
	t.Parallel()

	for _, n := range []int{0, 1, 8, 32} {
		got, err := randomString(n)
		if err != nil {
			t.Fatalf("randomString(%d): %v", n, err)
		}
		if len(got) != n {
			t.Errorf("len(randomString(%d)) = %d, want %d", n, len(got), n)
		}
	}
}
