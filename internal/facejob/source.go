package facejob

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/panbotka/kukatko/internal/imgconvert"
	"github.com/panbotka/kukatko/internal/photos"
)

// Materializer yields a real local file for a photo's stored relative path. It
// is the subset of storage.Storage StorageSource needs.
type Materializer interface {
	// Materialize returns a local path for relPath together with a cleanup the
	// caller must always run once it is done with the file.
	Materialize(ctx context.Context, relPath string) (path string, cleanup func(), err error)
}

// Decoder turns a media path into a path the standard image decoders can read,
// converting HEIC/RAW/video as needed. It is satisfied by imgconvert.EnsureDecodable
// and is injectable so StorageSource can be tested without the external tools.
type Decoder func(ctx context.Context, srcPath string) (path string, cleanup func(), err error)

// StorageSource opens a decodable copy of a photo's original from on-disk
// storage. The full-resolution original (decoded if HEIC/RAW/video) is what the
// face_detect handler streams to the sidecar so detected pixel boxes line up with
// the photo's stored dimensions.
type StorageSource struct {
	storage Materializer
	decode  Decoder
}

// NewStorageSource builds a StorageSource over storage, using
// imgconvert.EnsureDecodable as its decoder.
func NewStorageSource(storage Materializer) *StorageSource {
	return &StorageSource{storage: storage, decode: imgconvert.EnsureDecodable}
}

// cleanupReadCloser wraps an open file with the cleanup that releases the
// materialized original and any temporary converted copy, so closing the reader
// both closes the file and frees everything behind it.
type cleanupReadCloser struct {
	file    *os.File
	cleanup func()
}

// Read reads from the underlying file.
func (c *cleanupReadCloser) Read(p []byte) (int, error) {
	n, err := c.file.Read(p)
	if err != nil && err != io.EOF {
		return n, fmt.Errorf("facejob: reading image: %w", err)
	}
	return n, err //nolint:wrapcheck // io.EOF must pass through unwrapped for callers.
}

// Close closes the file and then runs the cleanup.
func (c *cleanupReadCloser) Close() error {
	err := c.file.Close()
	c.cleanup()
	if err != nil {
		return fmt.Errorf("facejob: closing image: %w", err)
	}
	return nil
}

// OpenDecodable materializes the photo's original as a local file, ensures it is
// decodable (converting non-native formats to a temporary JPEG) and opens it for
// reading. The returned reader's Close releases both the temporary converted file
// and the materialized original; every error path here releases them too.
func (s *StorageSource) OpenDecodable(ctx context.Context, photo photos.Photo) (io.ReadCloser, error) {
	abs, releaseOriginal, err := s.storage.Materialize(ctx, photo.FilePath)
	if err != nil {
		return nil, fmt.Errorf("facejob: materializing image for %s: %w", photo.UID, err)
	}
	decodable, releaseDecoded, err := s.decode(ctx, abs)
	if err != nil {
		releaseOriginal()
		return nil, fmt.Errorf("facejob: ensuring decodable image for %s: %w", photo.UID, err)
	}
	// The decoded file may be derived from the original, so drop it first.
	cleanup := func() { releaseDecoded(); releaseOriginal() }

	file, err := os.Open(decodable) //nolint:gosec // G304: path derived from the storage-confined original.
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("facejob: opening image for %s: %w", photo.UID, err)
	}
	return &cleanupReadCloser{file: file, cleanup: cleanup}, nil
}
