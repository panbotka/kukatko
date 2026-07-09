package thumbjob

import (
	"context"
	"fmt"
	"image"
	"os"

	// Register the pure-Go image decoders so image.Decode handles the formats the
	// pipeline hashes directly; HEIC/RAW are pre-converted by imgconvert.
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"

	"github.com/panbotka/kukatko/internal/imgconvert"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
)

// StorageDecoder decodes a photo's stored original into an image.Image, shelling
// out via imgconvert for HEIC/RAW and decoding pure-Go formats directly. It
// mirrors the upload pipeline's decode path so a regenerated pHash matches the
// one ingest would have produced.
type StorageDecoder struct {
	storage storage.Storage
}

// compile-time assertion that *StorageDecoder satisfies Decoder.
var _ Decoder = (*StorageDecoder)(nil)

// NewStorageDecoder returns a StorageDecoder reading originals through store.
func NewStorageDecoder(store storage.Storage) *StorageDecoder {
	return &StorageDecoder{storage: store}
}

// DecodeOriginal resolves the photo's stored original to a decodable image and
// decodes it. The returned cleanup releases the materialized original and removes
// any intermediate file produced for HEIC/RAW; the caller must defer it. The
// image is decoded without applying EXIF orientation, matching the pHash the
// upload pipeline computes.
func (d *StorageDecoder) DecodeOriginal(
	ctx context.Context, photo photos.Photo,
) (image.Image, func(), error) {
	abs, releaseOriginal, err := d.storage.Materialize(ctx, photo.FilePath)
	if err != nil {
		return nil, nil, fmt.Errorf("thumbjob: materializing original: %w", err)
	}
	decPath, releaseDecoded, err := imgconvert.EnsureDecodable(ctx, abs)
	if err != nil {
		releaseOriginal()
		return nil, nil, fmt.Errorf("thumbjob: ensuring decodable: %w", err)
	}
	// The decoded file may be derived from the original, so drop it first.
	cleanup := func() { releaseDecoded(); releaseOriginal() }

	file, err := os.Open(decPath) //nolint:gosec // G304: decPath comes from storage/imgconvert, not user input.
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("thumbjob: opening original: %w", err)
	}
	img, _, err := image.Decode(file)
	_ = file.Close()
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("thumbjob: decoding original: %w", err)
	}
	return img, cleanup, nil
}
