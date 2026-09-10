package storekeys_test

import (
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/storekeys"
)

// sampleKeys is one representative key per kind: the corpus every operation's
// own test classifies its behaviour over. It is deliberately exhaustive, and the
// test below fails when it falls behind the enum.
var sampleKeys = map[storekeys.Kind]string{
	storekeys.KindOriginal:  "2024/05/IMG_0001.jpg",
	storekeys.KindSidecar:   "sidecars/2024/05/IMG_0001.jpg.yaml",
	storekeys.KindThumbnail: "thumb/ab/cd/ef/abcdef_tile_500.jpg",
	storekeys.KindHLS:       "hls/abcdef0123456789/1080p/00000.m4s",
	storekeys.KindDump:      "db/kukatko-20260901T020000Z.dump",
	storekeys.KindPartial:   ".tmp/upload-123456",
	storekeys.KindForeign:   "some-other-tool/state.json",
}

// TestKinds_everySampleClassifies is the regression guard the whole package
// exists for: a new kind of object in the store must be classified, and every
// operation that reasons about the store as a whole must then decide what to do
// with it. A new Kind fails this test until it has a sample key that Classify
// recognises, and fails the exhaustive linter in each operation until that
// operation switches on it.
func TestKinds_everySampleClassifies(t *testing.T) {
	t.Parallel()

	kinds := storekeys.Kinds()
	if len(kinds) != len(sampleKeys) {
		t.Fatalf("Kinds() has %d kinds but the sample corpus has %d: add the new kind to both",
			len(kinds), len(sampleKeys))
	}
	for _, kind := range kinds {
		key, ok := sampleKeys[kind]
		if !ok {
			t.Fatalf("kind %v has no sample key", kind)
		}
		if got := storekeys.Classify(key); got != kind {
			t.Errorf("Classify(%q) = %v, want %v", key, got, kind)
		}
	}
}

// TestKinds_hasNoDuplicates guards the enumeration itself: a kind listed twice
// would let an operation's per-kind test pass while another kind went untested.
func TestKinds_hasNoDuplicates(t *testing.T) {
	t.Parallel()

	kinds := storekeys.Kinds()
	sorted := slices.Clone(kinds)
	slices.Sort(sorted)
	if compacted := slices.Compact(sorted); len(compacted) != len(kinds) {
		t.Errorf("Kinds() = %v, want each kind exactly once", kinds)
	}
}

// TestKinds_returnsACopy proves a caller cannot corrupt the enumeration for
// everyone else by sorting or overwriting what it was handed.
func TestKinds_returnsACopy(t *testing.T) {
	t.Parallel()

	first := storekeys.Kinds()
	first[0] = storekeys.KindForeign
	if second := storekeys.Kinds(); second[0] != storekeys.KindOriginal {
		t.Errorf("Kinds()[0] = %v after a caller overwrote its copy, want %v",
			second[0], storekeys.KindOriginal)
	}
}

// TestClassify covers the boundaries between the layouts: the shapes that are
// nearly an original, the prefixes that are nearly a Kukátko prefix, and the
// tolerated spellings of a key.
func TestClassify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
		want storekeys.Kind
	}{
		{"original", "2024/05/IMG_0001.jpg", storekeys.KindOriginal},
		{"original with a leading slash", "/2024/05/IMG_0001.jpg", storekeys.KindOriginal},
		{"original with surrounding space", "  2024/05/IMG_0001.jpg  ", storekeys.KindOriginal},
		{"month directory alone", "2024/05/", storekeys.KindForeign},
		{"deeper tree under a month", "2024/05/sub/IMG_0001.jpg", storekeys.KindForeign},
		{"four digits that are not a year layout", "12345/05/x.jpg", storekeys.KindForeign},
		{"thumbnail", "thumb/ab/cd/ef/abcdef_fit_1280.jpg", storekeys.KindThumbnail},
		{"prefix that merely starts like thumb", "thumbs-of-mine/x.jpg", storekeys.KindForeign},
		{"sidecar", "sidecars/2024/05/IMG_0001.jpg.yaml", storekeys.KindSidecar},
		{"hls init segment", "hls/abcdef0123456789/1080p/init.mp4", storekeys.KindHLS},
		{"hls media segment", "hls/abcdef0123456789/1080p/00042.m4s", storekeys.KindHLS},
		{"prefix that merely starts like hls", "hlsx/abcdef/1080p/init.mp4", storekeys.KindForeign},
		{"database dump", "db/kukatko-20260901T020000Z.dump", storekeys.KindDump},
		{"partial upload", ".tmp/upload-123456", storekeys.KindPartial},
		{"empty key", "", storekeys.KindForeign},
		{"whitespace only", "   ", storekeys.KindForeign},
		{"foreign object", "some-other-tool/state.json", storekeys.KindForeign},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := storekeys.Classify(tt.key); got != tt.want {
				t.Errorf("Classify(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

// TestKind_String checks every kind renders as its own name, since the string is
// what a test failure or a log line shows.
func TestKind_String(t *testing.T) {
	t.Parallel()

	want := map[storekeys.Kind]string{
		storekeys.KindOriginal:  "original",
		storekeys.KindSidecar:   "sidecar",
		storekeys.KindThumbnail: "thumbnail",
		storekeys.KindHLS:       "hls",
		storekeys.KindDump:      "dump",
		storekeys.KindPartial:   "partial",
		storekeys.KindForeign:   "foreign",
	}
	for _, kind := range storekeys.Kinds() {
		if got := kind.String(); got != want[kind] {
			t.Errorf("Kind(%d).String() = %q, want %q", int(kind), got, want[kind])
		}
	}
}
