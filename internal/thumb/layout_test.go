package thumb

import (
	"bytes"
	"context"
	"errors"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/storekeys"
)

// testPrefix is a well-formed project storage prefix: projects/<uid>/.
const testPrefix = "projects/pj0123456789abcdefghijklmn/"

// storeJPEGUnder is storeJPEG for a project with its own storage prefix: the
// original lands at <prefix>YYYY/MM/<name>, as ingest stores it.
func storeJPEGUnder(t *testing.T, store *storage.FS, prefix string, width, height int) photos.Photo {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, gradient(width, height), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode source jpeg: %v", err)
	}
	sf, err := store.StoreUnder(context.Background(), &buf, prefix, time.Time{}, "source.jpg")
	if err != nil {
		t.Fatalf("store source under %q: %v", prefix, err)
	}
	return photos.Photo{FileHash: sf.Hash, FilePath: sf.RelPath}
}

// TestRelPath_layouts covers the key of one size in both layouts: the root
// layout keeps the historical thumb/… key, a project prefix goes in front of it,
// and a malformed prefix is refused rather than turned into a path.
func TestRelPath_layouts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		prefix  string
		want    string
		wantErr error
	}{
		{"root layout", "", "thumb/ab/cd/ef/" + testHash + "_tile_224.jpg", nil},
		{"project prefix", testPrefix, testPrefix + "thumb/ab/cd/ef/" + testHash + "_tile_224.jpg", nil},
		{"traversal prefix", "../", "", storekeys.ErrInvalidPrefix},
		{"prefix without the slash", "projects/pj0123456789abcdefghijklmn", "", storekeys.ErrInvalidPrefix},
		{"foreign prefix", "elsewhere/", "", storekeys.ErrInvalidPrefix},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := RelPath(tt.prefix, testHash, "tile_224")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("RelPath(%q) error = %v, want %v", tt.prefix, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("RelPath(%q) = %q, want %q", tt.prefix, got, tt.want)
			}
		})
	}
}

// TestGenerate_followsTheOriginalsLayout proves the cache path of every size is
// taken from the photo's own file_path: a root-layout photo and a prefixed photo
// holding the very same bytes (one hash) get separate thumbnails, each under its
// own layout, and removing one leaves the other untouched.
func TestGenerate_followsTheOriginalsLayout(t *testing.T) {
	t.Parallel()
	th, store := newThumbnailer(t)
	tests := []struct {
		name   string
		prefix string
		photo  photos.Photo
	}{
		{"root layout", "", storeJPEG(t, store, 300, 200, 0)},
		{"project prefix", testPrefix, storeJPEGUnder(t, store, testPrefix, 300, 200)},
	}
	if tests[0].photo.FileHash != tests[1].photo.FileHash {
		t.Fatalf("fixtures differ in hash; the test needs identical bytes")
	}
	for _, tt := range tests {
		generated, err := th.Generate(context.Background(), tt.photo, GridSize)
		if err != nil {
			t.Fatalf("%s: Generate: %v", tt.name, err)
		}
		want, err := th.Path(tt.prefix, tt.photo.FileHash, GridSize)
		if err != nil {
			t.Fatalf("%s: Path: %v", tt.name, err)
		}
		if generated[GridSize] != want {
			t.Errorf("%s: generated at %q, want %q", tt.name, generated[GridSize], want)
		}
		if !fileExists(want) {
			t.Errorf("%s: no cache file at %q", tt.name, want)
		}
	}

	if err := th.Remove(testPrefix, tests[1].photo.FileHash); err != nil {
		t.Fatalf("Remove(prefixed): %v", err)
	}
	prefixed, _ := th.Path(testPrefix, tests[1].photo.FileHash, GridSize)
	if _, err := os.Stat(prefixed); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("prefixed size survived Remove: %v", err)
	}
	root, _ := th.Path("", tests[0].photo.FileHash, GridSize)
	if !fileExists(root) {
		t.Errorf("removing the prefixed photo's thumbnails removed the root-layout ones at %q", root)
	}
}

// TestGenerate_publishesUnderThePhotosPrefix proves a publishing backend gets a
// prefixed photo's sizes under the photo's own prefix, and that the size is then
// read back from there on a cold cache.
func TestGenerate_publishesUnderThePhotosPrefix(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	pub := newPublishingFS(store)
	th := New(pub, filepath.Join(root, "cache"))
	photo := storeJPEGUnder(t, store, testPrefix, 300, 200)

	if _, err := th.Generate(context.Background(), photo, GridSize); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for key := range pub.puts {
		if !strings.HasPrefix(key, testPrefix+CacheSubdir+"/") {
			t.Errorf("published %q outside the photo's prefix", key)
		}
	}
	rel, err := RelPath(testPrefix, photo.FileHash, GridSize)
	if err != nil {
		t.Fatalf("RelPath: %v", err)
	}
	if _, ok := pub.recorded(rel); !ok {
		t.Fatalf("size not published at %q", rel)
	}

	// A cold cache reads the published object back from the prefixed key.
	if err := th.Remove(testPrefix, photo.FileHash); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	reader, err := th.OpenOrGenerate(context.Background(), photo, GridSize)
	if err != nil {
		t.Fatalf("OpenOrGenerate: %v", err)
	}
	_ = reader.Close()
	if puts, _ := pub.counts(); puts != 1 {
		t.Errorf("Put called %d times, want 1 (the cold read must come from the bucket)", puts)
	}
}
