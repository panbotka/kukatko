package thumb

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/imgconvert"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
)

// testHash is a 64-character lowercase hex string used where a real SHA256 is
// not otherwise available; cacheRelPath shards its first three byte-pairs.
const testHash = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

// newThumbnailer builds a Thumbnailer over an isolated originals store and
// cache root under t.TempDir(), returning the thumbnailer and the store.
func newThumbnailer(t *testing.T) (*Thumbnailer, *storage.FS) {
	t.Helper()
	root := t.TempDir()
	store, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	return New(store, filepath.Join(root, "cache")), store
}

// gradient renders a deterministic RGBA gradient at width × height.
func gradient(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	return img
}

// storeJPEG encodes a width × height gradient as JPEG, stores it through the
// originals store, and returns a Photo referencing it with the given EXIF
// orientation.
func storeJPEG(t *testing.T, store *storage.FS, width, height, orientation int) photos.Photo {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, gradient(width, height), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode source jpeg: %v", err)
	}
	sf, err := store.Store(context.Background(), &buf, time.Time{}, "source.jpg")
	if err != nil {
		t.Fatalf("store source: %v", err)
	}
	return photos.Photo{FileHash: sf.Hash, FilePath: sf.RelPath, FileOrientation: orientation}
}

// storePNG is storeJPEG's PNG-source counterpart, used to confirm the package
// decodes more than one pure-Go input format.
func storePNG(t *testing.T, store *storage.FS, width, height int) photos.Photo {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, gradient(width, height)); err != nil {
		t.Fatalf("encode source png: %v", err)
	}
	sf, err := store.Store(context.Background(), &buf, time.Time{}, "source.png")
	if err != nil {
		t.Fatalf("store source: %v", err)
	}
	return photos.Photo{FileHash: sf.Hash, FilePath: sf.RelPath}
}

// jpegBounds decodes the JPEG at path and returns its dimensions.
func jpegBounds(t *testing.T, path string) (width, height int) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %q: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("decode %q: %v", path, err)
	}
	b := img.Bounds()
	return b.Dx(), b.Dy()
}

// TestGenerate_rejectsOversizedSource confirms the decode pixel cap refuses a
// source whose dimensions exceed the bound before the bitmap is allocated, so a
// decompression bomb fails the job (with imgconvert.ErrImageTooLarge) instead of
// OOMing the worker. A 1-pixel cap puts any real image over the bound.
func TestGenerate_rejectsOversizedSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	th := New(store, filepath.Join(root, "cache"), WithMaxPixels(1))
	photo := storeJPEG(t, store, 64, 48, 0)

	if _, err := th.Generate(context.Background(), photo, "fit_720"); !errors.Is(err, imgconvert.ErrImageTooLarge) {
		t.Fatalf("Generate error = %v, want imgconvert.ErrImageTooLarge", err)
	}
}

// TestGenerate_maxPixelsZeroDisablesCap confirms a thumbnailer built without
// WithMaxPixels (cap 0) decodes normally, so the bound is opt-in and never
// rejects a legitimate source when unset.
func TestGenerate_maxPixelsZeroDisablesCap(t *testing.T) {
	t.Parallel()
	th, store := newThumbnailer(t) // no WithMaxPixels => cap 0 => disabled
	photo := storeJPEG(t, store, 64, 48, 0)
	if _, err := th.Generate(context.Background(), photo, "fit_720"); err != nil {
		t.Fatalf("Generate with disabled cap = %v, want nil", err)
	}
}

// TestGenerate_fitResizesAndBounds confirms a fit size scales the longest side
// down to the bound while preserving aspect ratio.
func TestGenerate_fitResizesAndBounds(t *testing.T) {
	t.Parallel()
	th, store := newThumbnailer(t)
	photo := storeJPEG(t, store, 1200, 900, 0)

	got, err := th.Generate(context.Background(), photo, "fit_720")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	w, h := jpegBounds(t, got["fit_720"])
	if w != 720 || h != 540 {
		t.Errorf("fit_720 of 1200x900 = %dx%d, want 720x540", w, h)
	}
}

// TestGenerate_tileSquare confirms a tile size produces an exact square.
func TestGenerate_tileSquare(t *testing.T) {
	t.Parallel()
	th, store := newThumbnailer(t)
	photo := storeJPEG(t, store, 1000, 600, 0)

	got, err := th.Generate(context.Background(), photo, "tile_224")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	w, h := jpegBounds(t, got["tile_224"])
	if w != 224 || h != 224 {
		t.Errorf("tile_224 = %dx%d, want 224x224", w, h)
	}
}

// TestGenerate_pngSource confirms PNG originals are decoded and resized.
func TestGenerate_pngSource(t *testing.T) {
	t.Parallel()
	th, store := newThumbnailer(t)
	photo := storePNG(t, store, 800, 800)

	got, err := th.Generate(context.Background(), photo, "fit_720")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	w, h := jpegBounds(t, got["fit_720"])
	if w != 720 || h != 720 {
		t.Errorf("fit_720 of 800x800 png = %dx%d, want 720x720", w, h)
	}
}

// TestGenerateAll_allSizesWithinBounds checks every registered size is produced
// and respects its mode's bound. Not parallel: the decode + multi-size encode
// allocates enough memory that concurrent runs could strain a small device.
func TestGenerateAll_allSizesWithinBounds(t *testing.T) {
	th, store := newThumbnailer(t)
	photo := storeJPEG(t, store, 1600, 1200, 0)

	got, err := th.GenerateAll(context.Background(), photo)
	if err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	if len(got) != len(sizes) {
		t.Fatalf("GenerateAll returned %d sizes, want %d", len(got), len(sizes))
	}
	for name, spec := range sizes {
		abs, ok := got[name]
		if !ok {
			t.Errorf("size %q missing from result", name)
			continue
		}
		w, h := jpegBounds(t, abs)
		switch spec.Mode {
		case modeFit:
			if w > spec.Max || h > spec.Max {
				t.Errorf("size %q: %dx%d exceeds max side %d", name, w, h, spec.Max)
			}
		case modeCropSquare:
			if w != spec.Max || h != spec.Max {
				t.Errorf("size %q: %dx%d, want %dx%d", name, w, h, spec.Max, spec.Max)
			}
		}
	}
}

// TestRegenerateAll_overwritesStaleCache verifies RegenerateAll rebuilds a size
// whose cache file already exists (here corrupted to simulate a stale/broken
// thumbnail), where GenerateAll leaves a present size untouched. It underpins the
// on-demand "regenerate thumbnail" action, which must overwrite the cache.
func TestRegenerateAll_overwritesStaleCache(t *testing.T) {
	th, store := newThumbnailer(t)
	photo := storeJPEG(t, store, 800, 600, 0)
	ctx := context.Background()

	if _, err := th.GenerateAll(ctx, photo); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	size := SizeNames()[0]
	abs, err := th.Path(photo.FileHash, size)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := os.WriteFile(abs, []byte("stale"), 0o600); err != nil {
		t.Fatalf("corrupt cache: %v", err)
	}

	// GenerateAll must leave the present (corrupt) file untouched (idempotent skip).
	if _, err := th.GenerateAll(ctx, photo); err != nil {
		t.Fatalf("GenerateAll (second): %v", err)
	}
	if data, _ := os.ReadFile(abs); string(data) != "stale" {
		t.Fatal("GenerateAll must not overwrite a present size")
	}

	// RegenerateAll must rebuild it in place, replacing the stale bytes with a
	// valid JPEG.
	if _, err := th.RegenerateAll(ctx, photo); err != nil {
		t.Fatalf("RegenerateAll: %v", err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read regenerated: %v", err)
	}
	if string(data) == "stale" {
		t.Fatal("RegenerateAll did not overwrite the stale cache file")
	}
	if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("regenerated size is not a valid JPEG: %v", err)
	}
}

// TestGenerate_orientation6 confirms EXIF orientation 6 (rotate 90 CW) swaps
// the thumbnail's dimensions: an 800x600 landscape source becomes portrait.
func TestGenerate_orientation6(t *testing.T) {
	t.Parallel()
	th, store := newThumbnailer(t)
	photo := storeJPEG(t, store, 800, 600, 6)

	got, err := th.Generate(context.Background(), photo, "fit_720")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	w, h := jpegBounds(t, got["fit_720"])
	if h <= w {
		t.Errorf("orientation 6 should yield portrait, got %dx%d", w, h)
	}
	if w != 540 || h != 720 {
		t.Errorf("orientation 6 fit_720 = %dx%d, want 540x720", w, h)
	}
}

// TestGenerate_idempotent confirms a second run rewrites nothing: pre-existing
// thumbnails keep their modification time.
func TestGenerate_idempotent(t *testing.T) {
	t.Parallel()
	th, store := newThumbnailer(t)
	photo := storeJPEG(t, store, 600, 400, 0)

	first, err := th.Generate(context.Background(), photo, "fit_720", "tile_224")
	if err != nil {
		t.Fatalf("Generate first: %v", err)
	}
	mtimes := make(map[string]time.Time, len(first))
	for name, abs := range first {
		info, statErr := os.Stat(abs)
		if statErr != nil {
			t.Fatalf("stat %q: %v", abs, statErr)
		}
		mtimes[name] = info.ModTime()
	}

	time.Sleep(20 * time.Millisecond) // exceed mtime granularity

	second, err := th.Generate(context.Background(), photo, "fit_720", "tile_224")
	if err != nil {
		t.Fatalf("Generate second: %v", err)
	}
	for name, abs := range second {
		if abs != first[name] {
			t.Errorf("size %q path changed: %q → %q", name, first[name], abs)
		}
		info, statErr := os.Stat(abs)
		if statErr != nil {
			t.Fatalf("stat %q: %v", abs, statErr)
		}
		if !info.ModTime().Equal(mtimes[name]) {
			t.Errorf("size %q was rewritten (mtime %v → %v)", name, mtimes[name], info.ModTime())
		}
	}
}

// TestRemove_deletesAllSizesIdempotently generates several sizes, removes them
// all by hash, confirms they are gone, and that removing again (or removing a
// hash with no cache) is not an error.
func TestRemove_deletesAllSizesIdempotently(t *testing.T) {
	t.Parallel()
	th, store := newThumbnailer(t)
	photo := storeJPEG(t, store, 600, 400, 0)

	generated, err := th.GenerateAll(context.Background(), photo)
	if err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}

	if err := th.Remove(photo.FileHash); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	for size, abs := range generated {
		if _, statErr := os.Stat(abs); !errors.Is(statErr, os.ErrNotExist) {
			t.Errorf("size %q still present after Remove: %v", size, statErr)
		}
	}

	// Removing again, with nothing cached, is a no-op.
	if err := th.Remove(photo.FileHash); err != nil {
		t.Errorf("second Remove: %v", err)
	}
}

// TestRemove_invalidHash rejects a malformed hash with ErrInvalidHash.
func TestRemove_invalidHash(t *testing.T) {
	t.Parallel()
	th, _ := newThumbnailer(t)
	if err := th.Remove("xyz"); !errors.Is(err, ErrInvalidHash) {
		t.Errorf("Remove(\"xyz\") error = %v, want ErrInvalidHash", err)
	}
}

// TestGenerate_errors covers unknown sizes, invalid hashes, and the empty-size
// no-op.
func TestGenerate_errors(t *testing.T) {
	t.Parallel()
	th, store := newThumbnailer(t)
	photo := storeJPEG(t, store, 100, 100, 0)

	if _, err := th.Generate(context.Background(), photo, "bogus"); !errors.Is(err, ErrUnknownSize) {
		t.Errorf("unknown size error = %v, want ErrUnknownSize", err)
	}

	bad := photos.Photo{FileHash: "xyz", FilePath: photo.FilePath}
	if _, err := th.Generate(context.Background(), bad, "fit_720"); !errors.Is(err, ErrInvalidHash) {
		t.Errorf("invalid hash error = %v, want ErrInvalidHash", err)
	}

	got, err := th.Generate(context.Background(), photo)
	if err != nil {
		t.Fatalf("Generate with no sizes: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("no-size result = %v, want empty", got)
	}
}

// TestPath_sharding verifies the cache path shards the hash into aa/bb/cc and
// embeds hash and size in the filename.
func TestPath_sharding(t *testing.T) {
	t.Parallel()
	th, _ := newThumbnailer(t)
	abs, err := th.Path(testHash, "fit_1280")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	want := filepath.Join("thumb", "ab", "cd", "ef", testHash+"_fit_1280.jpg")
	if !strings.HasSuffix(abs, want) {
		t.Errorf("Path = %q, want suffix %q", abs, want)
	}
}

// TestPath_validation covers unknown sizes and malformed hashes.
func TestPath_validation(t *testing.T) {
	t.Parallel()
	th, _ := newThumbnailer(t)
	tests := []struct {
		name    string
		hash    string
		size    string
		wantErr error
	}{
		{"unknown size", testHash, "nope", ErrUnknownSize},
		{"short hash", "abcd", "fit_720", ErrInvalidHash},
		{"non-hex hash", "ghijklmnopqrst", "fit_720", ErrInvalidHash},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := th.Path(tc.hash, tc.size); !errors.Is(err, tc.wantErr) {
				t.Errorf("Path(%q,%q) error = %v, want %v", tc.hash, tc.size, err, tc.wantErr)
			}
		})
	}
}

// TestOpen covers the not-cached path and a successful open after generation.
func TestOpen(t *testing.T) {
	t.Parallel()
	th, store := newThumbnailer(t)
	photo := storeJPEG(t, store, 400, 300, 0)

	if _, err := th.Open(photo.FileHash, "fit_720"); !errors.Is(err, ErrNotCached) {
		t.Errorf("Open before generate error = %v, want ErrNotCached", err)
	}

	if _, err := th.Generate(context.Background(), photo, "fit_720"); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	rc, err := th.Open(photo.FileHash, "fit_720")
	if err != nil {
		t.Fatalf("Open after generate: %v", err)
	}
	defer func() { _ = rc.Close() }()
	if _, _, err := image.Decode(rc); err != nil {
		t.Errorf("decode opened thumb: %v", err)
	}
}

// TestSizeRegistry covers SizeNames (fresh copy, full coverage) and IsValidSize.
func TestSizeRegistry(t *testing.T) {
	t.Parallel()
	names := SizeNames()
	if len(names) != len(sizes) {
		t.Errorf("SizeNames returned %d, want %d", len(names), len(sizes))
	}
	for _, n := range names {
		if !IsValidSize(n) {
			t.Errorf("SizeNames returned unregistered %q", n)
		}
	}
	names[0] = "mutated"
	if SizeNames()[0] == "mutated" {
		t.Error("SizeNames must return a fresh copy")
	}
	if IsValidSize("not_a_size") {
		t.Error("IsValidSize accepted an unregistered name")
	}
}
