package metajob

import (
	"context"
	"fmt"
	"os"

	"github.com/panbotka/kukatko/internal/exif"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/video"
)

// StorageExtractor reads a photo's metadata out of its stored original. It goes
// through the storage abstraction rather than the filesystem, so it works whether
// the original sits on local disk (where materialising is free) or in R2 (where it
// is downloaded to a temp file and dropped again) — the metadata backfill has to
// cover both.
type StorageExtractor struct {
	storage storage.Storage
}

// compile-time assertion that *StorageExtractor satisfies Extractor.
var _ Extractor = (*StorageExtractor)(nil)

// NewStorageExtractor returns a StorageExtractor reading originals through store.
func NewStorageExtractor(store storage.Storage) *StorageExtractor {
	return &StorageExtractor{storage: store}
}

// ExtractOriginal materialises the photo's original and runs the metadata
// extractor over it. The temp copy an object store leaves behind is always
// released, including on failure.
//
// A missing original surfaces as an error wrapping os.ErrNotExist — from the
// object store directly, or from the extractor's own stat of a local path that
// resolves to nothing — which the caller reads as "skip this photo" rather than as
// a job failure.
func (e *StorageExtractor) ExtractOriginal(
	ctx context.Context, photo photos.Photo,
) (exif.Metadata, error) {
	abs, release, err := e.storage.Materialize(ctx, photo.FilePath)
	if err != nil {
		return exif.Metadata{}, fmt.Errorf("metajob: materializing original: %w", err)
	}
	defer release()

	meta, err := exif.Extract(ctx, abs)
	if err != nil {
		return exif.Metadata{}, fmt.Errorf("metajob: reading %s: %w", photo.FilePath, err)
	}
	return meta, nil
}

// ProbeVideo materialises the photo's original and probes it as a video container,
// returning what ffprobe (or the exiftool fallback) says about it. The temp copy an
// object store leaves behind is always released, including on failure.
//
// A missing original surfaces as an error wrapping os.ErrNotExist and a host with
// no probing tool as one wrapping video.ErrFFprobeMissing, which the caller reads
// as "skip this photo" rather than as a job failure.
func (e *StorageExtractor) ProbeVideo(
	ctx context.Context, photo photos.Photo,
) (video.Metadata, error) {
	abs, release, err := e.storage.Materialize(ctx, photo.FilePath)
	if err != nil {
		return video.Metadata{}, fmt.Errorf("metajob: materializing original: %w", err)
	}
	defer release()

	// The local store hands back a path without looking at it, so a photo whose
	// original is gone would reach ffprobe and come back as an unreadable file
	// rather than as a missing one. The caller branches on that difference.
	if _, err := os.Stat(abs); err != nil {
		return video.Metadata{}, fmt.Errorf("metajob: probing %s: %w", photo.FilePath, err)
	}
	meta, err := video.Probe(ctx, abs)
	if err != nil {
		return video.Metadata{}, fmt.Errorf("metajob: probing %s: %w", photo.FilePath, err)
	}
	return meta, nil
}
