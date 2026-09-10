package backup

import (
	"testing"

	"github.com/panbotka/kukatko/internal/storekeys"
)

// TestBackedUp_decidesEveryKind is the regression guard against a new prefix
// appearing in the store that the backup never learned about: every kind of
// object the store can hold must have a verdict here, chosen deliberately rather
// than inherited from a default clause. A new kind fails this test — and the
// exhaustive linter over backedUp — until someone says whether it is worth
// backing up.
func TestBackedUp_decidesEveryKind(t *testing.T) {
	t.Parallel()

	want := map[storekeys.Kind]bool{
		storekeys.KindOriginal:  true,
		storekeys.KindSidecar:   true,
		storekeys.KindForeign:   true,
		storekeys.KindThumbnail: false,
		storekeys.KindHLS:       false,
		storekeys.KindDump:      false,
		storekeys.KindPartial:   false,
	}
	kinds := storekeys.Kinds()
	if len(want) != len(kinds) {
		t.Fatalf("the store holds %d kinds but %d are decided here: decide the new one",
			len(kinds), len(want))
	}
	for _, kind := range kinds {
		expected, ok := want[kind]
		if !ok {
			t.Fatalf("kind %v has no backup verdict", kind)
		}
		if got := backedUp(kind); got != expected {
			t.Errorf("backedUp(%v) = %v, want %v", kind, got, expected)
		}
	}
}

// TestBackedUpKey covers the verdict as the two listers actually reach it: from
// a key alone.
func TestBackedUpKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  string
		want bool
	}{
		{key: "2026/01/a.jpg", want: true},
		{key: "sidecars/2026/01/a.jpg.yaml", want: true},
		{key: "thumb/ab/cd/ef/abcdef_tile_500.jpg", want: false},
		{key: "hls/abcdef0123456789/1080p/init.mp4", want: false},
		{key: "hls/abcdef0123456789/1080p/00003.m4s", want: false},
		{key: "db/kukatko-20260101T000000Z.dump", want: false},
		{key: ".tmp/upload-123", want: false},
		{key: "dbx/not-a-dump.jpg", want: true},
		{key: ".tmpfile.jpg", want: true},
		{key: "hlsx/not-a-segment.jpg", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()
			if got := backedUpKey(tt.key); got != tt.want {
				t.Errorf("backedUpKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

// TestSkippedDir verifies the walk turns away only at the root of a derived or
// temporary prefix. Every directory on the way to an original — the year, the
// month, and the store root itself — must be descended into, or the backup would
// walk away from the library.
func TestSkippedDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		dir  string
		want bool
	}{
		{dir: "thumb", want: true},
		{dir: "hls", want: true},
		{dir: "hls/abcdef0123456789", want: true},
		{dir: "db", want: true},
		{dir: ".tmp", want: true},
		{dir: "2026", want: false},
		{dir: "2026/01", want: false},
		{dir: "sidecars", want: false},
		{dir: "sidecars/2026", want: false},
		{dir: "somebody-elses-folder", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.dir, func(t *testing.T) {
			t.Parallel()
			if got := skippedDir(tt.dir); got != tt.want {
				t.Errorf("skippedDir(%q) = %v, want %v", tt.dir, got, tt.want)
			}
		})
	}
}
