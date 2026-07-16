package sidecarexport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/panbotka/kukatko/internal/storage"
)

// Prefix is the storage key prefix under which every sidecar lives. Sidecars form
// a parallel tree mirroring the originals' layout: the original at
// 2024/05/IMG_1234.jpg has its sidecar at sidecars/2024/05/IMG_1234.jpg.yml.
//
// A parallel tree rather than a file literally beside the original, for three
// reasons. The originals tree stays purely media, so the importers and scanners
// that walk it never have to learn to ignore a second kind of file. The whole
// export is one prefix, so it can be excluded, listed, rsynced or thrown away as
// a unit. And it still lives on the same storage as the originals — under the
// same root, on both the filesystem and the R2 backends — which is the property
// that actually matters: the curation travels with the photos, and the backup
// that syncs the originals picks the sidecars up with them.
const Prefix = "sidecars"

// Extension is appended to the original's key to form the sidecar's. It is
// appended rather than substituted for the original's extension so that
// IMG_1.jpg and IMG_1.png — the same base name, two distinct photos, a mundane
// occurrence — cannot collide on one sidecar and silently overwrite each other.
const Extension = ".yml"

// MIME is the media type sidecars are stored as.
const MIME = "application/yaml"

// ErrEmptyKey indicates a photo whose FilePath is empty, for which no sidecar key
// can be derived. It is a corrupt catalogue row rather than a missing file, so it
// is an error rather than a skip.
var ErrEmptyKey = errors.New("sidecarexport: photo has no file path")

// KeyFor returns the storage key of the sidecar describing the original stored at
// fileKey. It returns ErrEmptyKey when fileKey is empty.
//
// The mapping is total and reversible: strip the prefix and the extension and the
// original's key is back, which is what lets a rebuild walk the sidecar tree and
// find each photo's bytes.
func KeyFor(fileKey string) (string, error) {
	clean := strings.TrimPrefix(path.Clean(strings.TrimSpace(fileKey)), "/")
	if clean == "" || clean == "." {
		return "", ErrEmptyKey
	}
	return path.Join(Prefix, clean+Extension), nil
}

// ObjectStore is the subset of storage.Storage a Writer needs. It is declared
// here, narrow, so the package can be tested against a fake rather than a bucket
// — and so a Writer works unchanged over the filesystem and R2 backends alike,
// which is the whole point: sidecars must land wherever the originals live.
type ObjectStore interface {
	// Put writes src at file.RelPath, verifying it against file's declared size
	// and digest, and replacing whatever occupies the key.
	Put(ctx context.Context, src io.Reader, file storage.StoredFile) error
	// Delete removes the object at relPath, returning an error wrapping
	// os.ErrNotExist when it is absent.
	Delete(ctx context.Context, relPath string) error
}

// Writer's store is satisfied by the real storage backends.
var _ ObjectStore = (storage.Storage)(nil)

// Writer writes sidecar documents into an object store.
type Writer struct {
	store ObjectStore
}

// NewWriter returns a Writer that writes into store.
func NewWriter(store ObjectStore) *Writer {
	if store == nil {
		panic("sidecarexport: store is required")
	}
	return &Writer{store: store}
}

// Write renders doc and writes it to the sidecar key for fileKey, replacing any
// sidecar already there. It returns the key written.
//
// The write is atomic, and that is not this function's doing: it hands the store
// the exact size and digest of the bytes it is promising, and the store is
// obliged to land those bytes at the key or leave the key alone. The filesystem
// backend streams to a temp file on the same filesystem and renames; the object
// backend verifies the upload and removes it on mismatch. Either way a reader
// never sees a half-written sidecar — which matters more here than for most
// files, since a truncated YAML document is not a slightly worse sidecar but an
// unparseable one, and a sidecar that cannot be parsed is a photo's curation
// gone.
//
// It marshals the whole document into memory first, which the store's contract
// requires (it verifies against a size and digest declared up front) and which
// costs nothing: a sidecar is a few kilobytes of text.
func (w *Writer) Write(ctx context.Context, fileKey string, doc Document) (string, error) {
	key, err := KeyFor(fileKey)
	if err != nil {
		return "", err
	}
	data, err := Marshal(doc)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	file := storage.StoredFile{
		Hash:    hex.EncodeToString(sum[:]),
		RelPath: key,
		Size:    int64(len(data)),
		MIME:    MIME,
	}
	if err := w.store.Put(ctx, bytes.NewReader(data), file); err != nil {
		return "", fmt.Errorf("sidecarexport: writing sidecar %s: %w", key, err)
	}
	return key, nil
}

// Delete removes the sidecar describing the original at fileKey. A sidecar that
// is already absent is not an error: the point is that no sidecar remains, and
// two callers racing to purge the same photo should both succeed.
//
// It exists because a sidecar outliving its photo is not a harmless leftover but
// an active hazard — a rebuild that reads it resurrects a photo the user deleted.
func (w *Writer) Delete(ctx context.Context, fileKey string) error {
	key, err := KeyFor(fileKey)
	if err != nil {
		return err
	}
	if err := w.store.Delete(ctx, key); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("sidecarexport: deleting sidecar %s: %w", key, err)
	}
	return nil
}
