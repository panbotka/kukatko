package thumbjob

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"path/filepath"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/imgconvert"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
)

// storedPNG stores a w×h PNG in a fresh filesystem store and returns the store
// and the photo referencing it.
func storedPNG(t *testing.T, w, h int) (*storage.FS, photos.Photo) {
	t.Helper()
	store, err := storage.NewFS(filepath.Join(t.TempDir(), "originals"))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	sf, err := store.Store(t.Context(), &buf, time.Time{}, "source.png")
	if err != nil {
		t.Fatal(err)
	}
	return store, photos.Photo{UID: "ph1", FileHash: sf.Hash, FilePath: sf.RelPath}
}

// TestStorageDecoder_DecodeOriginal_gated verifies the pHash decode is gated
// like ingest's: over the pixel cap or the whole decode budget it fails with
// ErrImageTooLarge instead of rasterizing, and within both it decodes and its
// cleanup gives the reservation back.
func TestStorageDecoder_DecodeOriginal_gated(t *testing.T) {
	t.Parallel()
	store, photo := storedPNG(t, 40, 30) // 4800 bytes decoded

	if _, _, err := NewStorageDecoder(store, 100, nil).DecodeOriginal(t.Context(), photo); !errors.Is(
		err, imgconvert.ErrImageTooLarge) {
		t.Errorf("over the pixel cap: error = %v, want ErrImageTooLarge", err)
	}
	tiny := imgconvert.NewDecodeBudget(1000)
	if _, _, err := NewStorageDecoder(store, 0, tiny).DecodeOriginal(t.Context(), photo); !errors.Is(
		err, imgconvert.ErrOverBudget) {
		t.Errorf("over the budget: error = %v, want ErrOverBudget", err)
	}

	budget := imgconvert.NewDecodeBudget(4800)
	img, cleanup, err := NewStorageDecoder(store, 0, budget).DecodeOriginal(t.Context(), photo)
	if err != nil {
		t.Fatalf("within both: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 40 || b.Dy() != 30 {
		t.Errorf("decoded %v, want 40x30", b)
	}
	cleanup()
	release, err := budget.Reserve(t.Context(), budget.Capacity())
	if err != nil {
		t.Fatalf("the budget is not whole again after cleanup: %v", err)
	}
	release()
}
