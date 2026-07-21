package storagemigrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/panbotka/kukatko/internal/sidecarexport"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
)

// thumbMIME is the media type of every cached thumbnail; the thumbnailer encodes
// nothing but JPEG.
const thumbMIME = "image/jpeg"

// object is one file the migration moves: where its bytes come from, and the
// identity the destination must hold once they arrive.
type object struct {
	// relPath is the object key. Source and destination share one layout, so it
	// is the photos.file_path or the thumbnail cache path verbatim.
	relPath string
	// size is how many bytes it holds.
	size int64
	// mime is the media type the destination serves it as.
	mime string
	// digest returns the object's lowercase hex SHA256, computed on demand: for an
	// original the catalogue already knows it; for a thumbnail or a sidecar it is
	// read off the disk. A dry run never asks, and so never re-reads the thumbnail
	// cache or a sidecar. It takes a context so a source-backed read (the sidecar)
	// honours cancellation, mirroring open.
	digest func(ctx context.Context) (string, error)
	// open yields the bytes. The caller closes the reader.
	open func(ctx context.Context) (io.ReadCloser, error)
}

// stored returns the identity the destination must end up holding for o.
func (o object) stored(digest string) storage.StoredFile {
	return storage.StoredFile{Hash: digest, RelPath: o.relPath, Size: o.size, MIME: o.mime}
}

// plan lists what one photo contributes to the object store: its original, its
// metadata sidecar when one exists on the local disk, plus every thumbnail size
// that currently sits in the local cache. Sizes that were never generated are
// not generated here — the cache is regenerable from the original, and a
// migration is the wrong place to spend an afternoon of CPU on it. The sidecar,
// by contrast, is not regenerable and must travel with the original (see
// planSidecar).
func (m *Migrator) plan(ctx context.Context, item Item) ([]object, error) {
	sizes := thumb.SizeNames()
	objects := make([]object, 0, 2+len(sizes))
	objects = append(objects, object{
		relPath: item.FilePath,
		size:    item.FileSize,
		mime:    mimeOr(item.FileMIME),
		digest:  func(context.Context) (string, error) { return item.FileHash, nil },
		open: func(ctx context.Context) (io.ReadCloser, error) {
			return m.cfg.Source.Open(ctx, item.FilePath)
		},
	})
	sidecar, err := m.planSidecar(ctx, item)
	if err != nil {
		return nil, err
	}
	objects = append(objects, sidecar...)
	thumbs, err := m.planThumbs(item.FileHash, sizes)
	if err != nil {
		return nil, err
	}
	return append(objects, thumbs...), nil
}

// planSidecar returns the one object for the photo's metadata sidecar, or an
// empty slice when no sidecar exists on the local disk yet. Unlike a regenerable
// thumbnail, the sidecar is the disaster-recovery artifact a rebuild reads the
// catalogue back out of — so it must be uploaded and verified alongside the
// original, and the original must not be deleted locally until it has been. A
// photo whose curation never changed has no sidecar, which is not an error:
// there is simply nothing to move.
//
// Its size comes from a cheap stat so a dry run stays cheap; its digest is read
// lazily, only when the object is actually transferred, exactly as a thumbnail's
// is.
func (m *Migrator) planSidecar(ctx context.Context, item Item) ([]object, error) {
	key, err := sidecarexport.KeyFor(item.FilePath)
	if err != nil {
		return nil, fmt.Errorf("storagemigrate: sidecar key for %s: %w", item.FilePath, err)
	}
	info, err := m.cfg.Source.Stat(ctx, key)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storagemigrate: stat sidecar %s: %w", key, err)
	}
	return []object{{
		relPath: key,
		size:    info.Size(),
		mime:    sidecarexport.MIME,
		digest:  func(ctx context.Context) (string, error) { return hashSource(ctx, m.cfg.Source, key) },
		open:    func(ctx context.Context) (io.ReadCloser, error) { return m.cfg.Source.Open(ctx, key) },
	}}, nil
}

// planThumbs lists the cached thumbnails of the photo with the given file hash.
// A size that was never generated is skipped, not an error: an incomplete cache
// is the normal state of a library that has only ever rendered the grid tile.
func (m *Migrator) planThumbs(fileHash string, sizes []string) ([]object, error) {
	planned := make([]object, 0, len(sizes))
	for _, size := range sizes {
		relPath, err := thumb.RelPath(fileHash, size)
		if err != nil {
			return nil, fmt.Errorf("storagemigrate: thumbnail key for %s/%s: %w", fileHash, size, err)
		}
		absPath := filepath.Join(m.cfg.CacheDir, filepath.FromSlash(relPath))
		info, err := os.Stat(absPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("storagemigrate: stat thumbnail %s: %w", relPath, err)
		}
		planned = append(planned, object{
			relPath: relPath,
			size:    info.Size(),
			mime:    thumbMIME,
			digest:  func(context.Context) (string, error) { return hashFile(absPath) },
			open:    openFile(absPath),
		})
	}
	return planned, nil
}

// mimeOr returns mime, or the generic octet-stream type when the catalogue
// recorded none.
func mimeOr(mime string) string {
	if mime == "" {
		return fallbackMIME
	}
	return mime
}

// openFile returns an opener for the local file at absPath. The context is
// unused: opening a local file does not block.
func openFile(absPath string) func(context.Context) (io.ReadCloser, error) {
	return func(context.Context) (io.ReadCloser, error) {
		file, err := os.Open(absPath) //nolint:gosec // G304: absPath is the configured cache dir plus a validated key.
		if err != nil {
			return nil, fmt.Errorf("storagemigrate: opening %s: %w", absPath, err)
		}
		return file, nil
	}
}

// hashSource returns the lowercase hex SHA256 of the object at relPath, read out
// of the source store and streamed through the hasher. It is the source-backed
// counterpart of hashFile: the sidecar lives under the originals root the
// migration reads through the Source interface, not in the local thumbnail
// cache, so its digest is read the same way its bytes are later uploaded.
func hashSource(ctx context.Context, source Source, relPath string) (string, error) {
	reader, err := source.Open(ctx, relPath)
	if err != nil {
		return "", fmt.Errorf("storagemigrate: opening %s: %w", relPath, err)
	}
	defer func() { _ = reader.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, reader); err != nil {
		return "", fmt.Errorf("storagemigrate: hashing %s: %w", relPath, err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// hashFile returns the lowercase hex SHA256 of the file at absPath, streaming it
// through the hasher rather than reading it into memory.
func hashFile(absPath string) (string, error) {
	file, err := os.Open(absPath) //nolint:gosec // G304: absPath is the configured cache dir plus a validated key.
	if err != nil {
		return "", fmt.Errorf("storagemigrate: opening %s: %w", absPath, err)
	}
	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("storagemigrate: hashing %s: %w", absPath, err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
