package organize

import (
	"strings"
	"testing"
)

// TestNewUIDShape checks that generated album and label UIDs carry the right
// prefix, fit the column width, and use only alphabet characters.
func TestNewUIDShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		gen    func() (string, error)
		prefix string
	}{
		{name: "album", gen: newAlbumUID, prefix: albumUIDPrefix},
		{name: "label", gen: newLabelUID, prefix: labelUIDPrefix},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			uid, err := tt.gen()
			if err != nil {
				t.Fatalf("%s uid: %v", tt.name, err)
			}
			if !strings.HasPrefix(uid, tt.prefix) {
				t.Errorf("uid %q missing prefix %q", uid, tt.prefix)
			}
			if len(uid) != len(tt.prefix)+uidSuffixLen || len(uid) > uidMaxLen {
				t.Errorf("uid %q has length %d, want %d", uid, len(uid), len(tt.prefix)+uidSuffixLen)
			}
			for _, r := range uid[len(tt.prefix):] {
				if !strings.ContainsRune(uidAlphabet, r) {
					t.Errorf("uid %q has character %q outside alphabet", uid, r)
				}
			}
		})
	}
}

// TestNewUIDUnique checks that successive UIDs differ (collisions are astronomically
// unlikely with 120 bits of entropy).
func TestNewUIDUnique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool, 100)
	for range 100 {
		uid, err := newAlbumUID()
		if err != nil {
			t.Fatalf("newAlbumUID: %v", err)
		}
		if seen[uid] {
			t.Fatalf("duplicate uid %q", uid)
		}
		seen[uid] = true
	}
}
