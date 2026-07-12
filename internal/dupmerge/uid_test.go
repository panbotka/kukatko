package dupmerge

import (
	"strings"
	"testing"
)

func TestNewMarkerUID(t *testing.T) {
	t.Parallel()
	uid, err := newMarkerUID()
	if err != nil {
		t.Fatalf("newMarkerUID() error = %v", err)
	}
	if !strings.HasPrefix(uid, markerUIDPrefix) {
		t.Errorf("uid %q does not start with %q", uid, markerUIDPrefix)
	}
	if want := len(markerUIDPrefix) + uidSuffixLen; len(uid) != want {
		t.Errorf("uid %q length = %d, want %d", uid, len(uid), want)
	}
	suffix := strings.TrimPrefix(uid, markerUIDPrefix)
	for _, r := range suffix {
		if !strings.ContainsRune(uidAlphabet, r) {
			t.Errorf("uid suffix contains %q, outside the alphabet", r)
		}
	}
}

func TestNewMarkerUID_unique(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool, 100)
	for range 100 {
		uid, err := newMarkerUID()
		if err != nil {
			t.Fatalf("newMarkerUID() error = %v", err)
		}
		if seen[uid] {
			t.Fatalf("newMarkerUID() produced a duplicate: %q", uid)
		}
		seen[uid] = true
	}
}
