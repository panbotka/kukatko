package familyexport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/panbotka/kukatko/internal/storage"
)

// ObjectStore is the subset of storage.Storage a Writer needs. It is declared
// here, narrow, so the package can be tested against a fake rather than a bucket
// — and so a Writer works unchanged over the filesystem and R2 backends alike,
// which is the whole point: the tree must land wherever the originals live.
type ObjectStore interface {
	// Put writes src at file.RelPath, verifying it against file's declared size
	// and digest, and replacing whatever occupies the key.
	Put(ctx context.Context, src io.Reader, file storage.StoredFile) error
}

// Writer's store is satisfied by the real storage backends.
var _ ObjectStore = (storage.Storage)(nil)

// Writer writes the genealogy document into an object store.
type Writer struct {
	store ObjectStore
}

// NewWriter returns a Writer that writes into store. It panics on a nil store,
// which is a wiring bug and should fail at startup rather than on the first job.
func NewWriter(store ObjectStore) *Writer {
	if store == nil {
		panic("familyexport: store is required")
	}
	return &Writer{store: store}
}

// Write renders doc and writes it to Key, replacing whatever was there. It
// returns the key written.
//
// The write is atomic, and that is not this function's doing: it hands the store
// the exact size and digest of the bytes it is promising, and the store is
// obliged to land those bytes at the key or leave the key alone. That matters
// more here than for most files — this is one document for the whole library, so
// a truncated write is not a slightly worse tree but every relation in the
// archive gone.
//
// It marshals the whole document into memory first, which the store's contract
// requires (it verifies against a size and digest declared up front) and which
// costs nothing: a family archive's genealogy is a few kilobytes of text.
func (w *Writer) Write(ctx context.Context, doc Document) (string, error) {
	data, err := Marshal(doc)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	file := storage.StoredFile{
		Hash:    hex.EncodeToString(sum[:]),
		RelPath: Key,
		Size:    int64(len(data)),
		MIME:    MIME,
	}
	if err := w.store.Put(ctx, bytes.NewReader(data), file); err != nil {
		return "", fmt.Errorf("familyexport: writing %s: %w", Key, err)
	}
	return Key, nil
}
