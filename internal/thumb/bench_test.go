package thumb

import (
	"bytes"
	"context"
	"image/jpeg"
	"path/filepath"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
)

// benchSource builds a Thumbnailer over a fresh cache and stores a width×height
// JPEG gradient original through it, returning the thumbnailer and the photo.
// It is the shared setup for the thumbnail benchmarks.
func benchSource(b *testing.B, width, height int) (*Thumbnailer, photos.Photo) {
	b.Helper()
	root := b.TempDir()
	store, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		b.Fatalf("storage.NewFS: %v", err)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, gradient(width, height), &jpeg.Options{Quality: 90}); err != nil {
		b.Fatalf("encode source jpeg: %v", err)
	}
	sf, err := store.Store(context.Background(), &buf, time.Time{}, "bench.jpg")
	if err != nil {
		b.Fatalf("store source: %v", err)
	}
	th := New(store, filepath.Join(root, "cache"))
	return th, photos.Photo{FileHash: sf.Hash, FilePath: sf.RelPath, FileMime: "image/jpeg"}
}

// BenchmarkGenerateFit720 measures pure-Go fit_720 generation (decode + resize +
// JPEG encode + atomic write) from a 12-megapixel source — the representative
// per-photo cost of the grid/preview cache. The cache is cleared each iteration
// (untimed) so every iteration does the full work.
func BenchmarkGenerateFit720(b *testing.B) {
	th, photo := benchSource(b, 4000, 3000)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		if err := th.Remove(photo.FileHash); err != nil {
			b.Fatalf("Remove: %v", err)
		}
		b.StartTimer()
		if _, err := th.Generate(ctx, photo, "fit_720"); err != nil {
			b.Fatalf("Generate: %v", err)
		}
	}
}

// BenchmarkGenerateAll measures generating every registered size for a 12-MP
// source (one decode, all sizes encoded in parallel) — the per-photo cost paid
// at import/backfill time.
func BenchmarkGenerateAll(b *testing.B) {
	th, photo := benchSource(b, 4000, 3000)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		if err := th.Remove(photo.FileHash); err != nil {
			b.Fatalf("Remove: %v", err)
		}
		b.StartTimer()
		if _, err := th.GenerateAll(ctx, photo); err != nil {
			b.Fatalf("GenerateAll: %v", err)
		}
	}
}
