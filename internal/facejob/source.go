package facejob

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/panbotka/kukatko/internal/imgconvert"
	"github.com/panbotka/kukatko/internal/photos"
)

// PathResolver maps a photo's stored relative path to an absolute filesystem
// path. It is the subset of storage.Storage StorageSource needs.
type PathResolver interface {
	// AbsPath returns the absolute filesystem path for relPath.
	AbsPath(relPath string) string
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
	storage PathResolver
	decode  Decoder
}

// NewStorageSource builds a StorageSource over storage, using
// imgconvert.EnsureDecodable as its decoder.
func NewStorageSource(storage PathResolver) *StorageSource {
	return &StorageSource{storage: storage, decode: imgconvert.EnsureDecodable}
}

// cleanupReadCloser wraps an open file with the temp-file cleanup returned by the
// decoder, so closing the reader both closes the file and removes any temporary
// converted copy.
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

// Close closes the file and then runs the decoder cleanup.
func (c *cleanupReadCloser) Close() error {
	err := c.file.Close()
	c.cleanup()
	if err != nil {
		return fmt.Errorf("facejob: closing image: %w", err)
	}
	return nil
}

// OpenDecodable resolves the photo's original path, ensures it is decodable
// (converting non-native formats to a temporary JPEG) and opens it for reading.
// The returned reader's Close removes any temporary converted file.
func (s *StorageSource) OpenDecodable(ctx context.Context, photo photos.Photo) (io.ReadCloser, error) {
	abs := s.storage.AbsPath(photo.FilePath)
	decodable, cleanup, err := s.decode(ctx, abs)
	if err != nil {
		return nil, fmt.Errorf("facejob: ensuring decodable image for %s: %w", photo.UID, err)
	}
	file, err := os.Open(decodable) //nolint:gosec // G304: path derived from storage-confined AbsPath.
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("facejob: opening image for %s: %w", photo.UID, err)
	}
	return &cleanupReadCloser{file: file, cleanup: cleanup}, nil
}
