package storekeys_test

import (
	"errors"
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
	storekeys.KindFamilies:  "families.yaml",
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
		{"family tree export", "families.yaml", storekeys.KindFamilies},
		{"family tree export with a leading slash", "/families.yaml", storekeys.KindFamilies},
		{"a name that merely starts like it", "families.yaml.bak", storekeys.KindForeign},
		{"a directory of that name", "families.yaml/inner.txt", storekeys.KindForeign},
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

// testUID is a well-formed project uid; testPrefix is its storage prefix.
const (
	testUID    = "pj0123456789abcdefghijklmn"
	testPrefix = "projects/" + testUID + "/"
)

// TestClassifyWithProject_prefixedTwins is TestClassify's corpus again under a
// project prefix: every per-project kind is recognised exactly as at the root
// and reports the project's uid, while the store-wide kinds (dumps, partial
// uploads) and every foreign shape stay foreign and name no project.
func TestClassifyWithProject_prefixedTwins(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rest string
		want storekeys.Kind
	}{
		{"original", "2024/05/IMG_0001.jpg", storekeys.KindOriginal},
		{"month directory alone", "2024/05/", storekeys.KindForeign},
		{"deeper tree under a month", "2024/05/sub/IMG_0001.jpg", storekeys.KindForeign},
		{"four digits that are not a year layout", "12345/05/x.jpg", storekeys.KindForeign},
		{"thumbnail", "thumb/ab/cd/ef/abcdef_fit_1280.jpg", storekeys.KindThumbnail},
		{"prefix that merely starts like thumb", "thumbs-of-mine/x.jpg", storekeys.KindForeign},
		{"sidecar", "sidecars/2024/05/IMG_0001.jpg.yml", storekeys.KindSidecar},
		{"hls init segment", "hls/abcdef0123456789/1080p/init.mp4", storekeys.KindHLS},
		{"hls media segment", "hls/abcdef0123456789/1080p/00042.m4s", storekeys.KindHLS},
		{"prefix that merely starts like hls", "hlsx/abcdef/1080p/init.mp4", storekeys.KindForeign},
		{"family tree export", "families.yaml", storekeys.KindFamilies},
		{"a name that merely starts like it", "families.yaml.bak", storekeys.KindForeign},
		{"a directory of that name", "families.yaml/inner.txt", storekeys.KindForeign},
		{"database dump is store-wide", "db/kukatko-20260901T020000Z.dump", storekeys.KindForeign},
		{"partial upload is store-wide", ".tmp/upload-123456", storekeys.KindForeign},
		{"the prefix alone", "", storekeys.KindForeign},
		{"foreign object", "some-other-tool/state.json", storekeys.KindForeign},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, key := range []string{testPrefix + tt.rest, "/" + testPrefix + tt.rest} {
				kind, uid := storekeys.ClassifyWithProject(key)
				wantUID := testUID
				if tt.want == storekeys.KindForeign {
					wantUID = ""
				}
				if kind != tt.want || uid != wantUID {
					t.Errorf("ClassifyWithProject(%q) = %v, %q; want %v, %q", key, kind, uid, tt.want, wantUID)
				}
				if got := storekeys.Classify(key); got != tt.want {
					t.Errorf("Classify(%q) = %v, want %v", key, got, tt.want)
				}
			}
		})
	}
}

// TestClassifyWithProject_rootLayoutNamesNoProject verifies every sample key at
// the root keeps its kind and reports no project.
func TestClassifyWithProject_rootLayoutNamesNoProject(t *testing.T) {
	t.Parallel()

	for kind, key := range sampleKeys {
		gotKind, uid := storekeys.ClassifyWithProject(key)
		if gotKind != kind || uid != "" {
			t.Errorf("ClassifyWithProject(%q) = %v, %q; want %v, \"\"", key, gotKind, uid, kind)
		}
	}
}

// TestClassify_malformedProjectPrefix verifies a projects/ tree whose uid is not
// a project uid is not taken for a project: its keys are foreign, never an
// original of some project that does not exist.
func TestClassify_malformedProjectPrefix(t *testing.T) {
	t.Parallel()

	for _, key := range []string{
		"projects/notauid/2024/05/x.jpg",
		"projects/pj0123456789ABCDEFGHIJKLMN/2024/05/x.jpg",
		"projects/pj0123456789abcdefghijklm/2024/05/x.jpg",
		"projects/pj0123456789abcdefghijklmno/2024/05/x.jpg",
		"projects/xx0123456789abcdefghijklmn/2024/05/x.jpg",
		"projects/" + testUID,
		"projects//2024/05/x.jpg",
	} {
		kind, uid := storekeys.ClassifyWithProject(key)
		if kind != storekeys.KindForeign || uid != "" {
			t.Errorf("ClassifyWithProject(%q) = %v, %q; want foreign, no project", key, kind, uid)
		}
	}
}

// TestPrefixOf covers the prefix of an original's key in both layouts and the
// shapes that only look like a project prefix.
func TestPrefixOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
		want string
	}{
		{"root layout", "2024/05/x.jpg", ""},
		{"project prefix", testPrefix + "2024/05/x.jpg", testPrefix},
		{"leading slash", "/" + testPrefix + "2024/05/x.jpg", testPrefix},
		{"surrounding space", "  " + testPrefix + "2024/05/x.jpg ", testPrefix},
		{"the bootstrap project's uid", "projects/pj0000000000000000veselice/2024/05/x.jpg",
			"projects/pj0000000000000000veselice/"},
		{"malformed uid", "projects/notauid/2024/05/x.jpg", ""},
		{"prefix without the slash", "projects/" + testUID, ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := storekeys.PrefixOf(tt.key); got != tt.want {
				t.Errorf("PrefixOf(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

// TestStripJoin verifies Strip splits a key into its prefix and the rest, and
// Join puts them back together into the cleaned key.
func TestStripJoin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key        string
		wantPrefix string
		wantRest   string
		wantJoined string
	}{
		{"2024/05/x.jpg", "", "2024/05/x.jpg", "2024/05/x.jpg"},
		{testPrefix + "sidecars/2024/05/x.jpg.yml", testPrefix, "sidecars/2024/05/x.jpg.yml",
			testPrefix + "sidecars/2024/05/x.jpg.yml"},
		{"/" + testPrefix + "thumb/a.jpg", testPrefix, "thumb/a.jpg", testPrefix + "thumb/a.jpg"},
		{testPrefix, testPrefix, "", testPrefix},
		{"projects/notauid/x", "", "projects/notauid/x", "projects/notauid/x"},
	}
	for _, tt := range tests {
		prefix, rest := storekeys.Strip(tt.key)
		if prefix != tt.wantPrefix || rest != tt.wantRest {
			t.Errorf("Strip(%q) = %q, %q; want %q, %q", tt.key, prefix, rest, tt.wantPrefix, tt.wantRest)
		}
		if joined := storekeys.Join(prefix, rest); joined != tt.wantJoined {
			t.Errorf("Join(Strip(%q)) = %q, want %q", tt.key, joined, tt.wantJoined)
		}
	}
}

// TestValidatePrefix accepts exactly the two shapes a storage prefix may have.
func TestValidatePrefix(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"", testPrefix} {
		if err := storekeys.ValidatePrefix(ok); err != nil {
			t.Errorf("ValidatePrefix(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{
		"/", "../", "projects/", "projects/" + testUID, "/" + testPrefix, testPrefix + "x/",
		"projects/notauid/", "other/",
	} {
		if err := storekeys.ValidatePrefix(bad); !errors.Is(err, storekeys.ErrInvalidPrefix) {
			t.Errorf("ValidatePrefix(%q) = %v, want ErrInvalidPrefix", bad, err)
		}
	}
}

// TestProjectUID reads the uid back out of a prefix, and nothing out of
// anything that is not exactly one.
func TestProjectUID(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		testPrefix:                   testUID,
		"":                           "",
		testPrefix + "2024/05/x.jpg": "",
		"projects/notauid/":          "",
	}
	for prefix, want := range tests {
		if got := storekeys.ProjectUID(prefix); got != want {
			t.Errorf("ProjectUID(%q) = %q, want %q", prefix, got, want)
		}
	}
}

// TestKind_String checks every kind renders as its own name, since the string is
// what a test failure or a log line shows.
func TestKind_String(t *testing.T) {
	t.Parallel()

	want := map[storekeys.Kind]string{
		storekeys.KindOriginal:  "original",
		storekeys.KindSidecar:   "sidecar",
		storekeys.KindFamilies:  "families",
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
