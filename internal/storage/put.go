package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
)

// Errors reported when a store does not hold the content the caller declared.
// They are the reason a bulk migration may never delete a source file on the
// strength of a successful write alone: a Put that fails with one of these has
// left no trustworthy object behind, and a Head that disagrees with the
// catalogue means the object cannot stand in for the local file.
var (
	// ErrHashMismatch indicates the bytes that reached the store hashed to a
	// different SHA256 digest than the caller declared.
	ErrHashMismatch = errors.New("storage: content hash mismatch")
	// ErrSizeMismatch indicates the store holds a different number of bytes than
	// the caller declared.
	ErrSizeMismatch = errors.New("storage: content size mismatch")
)

// hashingReader wraps a reader, computing the SHA256 digest of every byte read
// through it and counting them, so a stream can be verified against its declared
// identity without a second pass over it — which matters when the stream is a
// multi-gigabyte video on its way to a bucket.
type hashingReader struct {
	src    io.Reader
	digest hash.Hash
	read   int64
}

// newHashingReader returns a hashingReader over src.
func newHashingReader(src io.Reader) *hashingReader {
	return &hashingReader{src: src, digest: sha256.New()}
}

// Read implements io.Reader, feeding everything it returns into the digest.
func (h *hashingReader) Read(p []byte) (int, error) {
	n, err := h.src.Read(p)
	if n > 0 {
		// hash.Hash.Write never returns an error.
		_, _ = h.digest.Write(p[:n])
		h.read += int64(n)
	}
	return n, err //nolint:wrapcheck // io.EOF must pass through unwrapped for io.Copy.
}

// sum returns the lowercase hex SHA256 digest of everything read so far.
func (h *hashingReader) sum() string {
	return hex.EncodeToString(h.digest.Sum(nil))
}

// verifyContent reports whether a stream of gotSize bytes digesting to gotHash
// matches the identity want promises, returning ErrSizeMismatch or
// ErrHashMismatch (in that order — a truncated stream explains a wrong digest,
// so the size is the more useful diagnosis) and nil when they agree.
//
// A want.Hash of "" means the identity is unknown and only the size is checked;
// that is what a backend which records no digest of its own (some other tool
// wrote the object) reports from Head.
func verifyContent(want StoredFile, gotHash string, gotSize int64) error {
	if gotSize != want.Size {
		return fmt.Errorf("%w: %s: %d bytes, expected %d", ErrSizeMismatch, want.RelPath, gotSize, want.Size)
	}
	if want.Hash != "" && gotHash != want.Hash {
		return fmt.Errorf("%w: %s: digest %s, expected %s", ErrHashMismatch, want.RelPath, gotHash, want.Hash)
	}
	return nil
}
