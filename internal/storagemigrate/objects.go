package storagemigrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/panbotka/kukatko/internal/familyexport"
	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/sidecarexport"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/storekeys"
	"github.com/panbotka/kukatko/internal/thumb"
)

// thumbMIME is the media type of every cached thumbnail; the thumbnailer encodes
// nothing but JPEG.
const thumbMIME = "image/jpeg"

// object is one file the migration moves: where its bytes come from, and the
// identity the destination must hold once they arrive.
type object struct {
	// relPath is the object key. Source and destination share one layout, so it
	// is the photos.file_path, the thumbnail cache path or the streaming segment
	// key verbatim.
	relPath string
	// kind is which of the store's layouts owns it, which is what decides whether
	// the local copy may be removed once the object is durable elsewhere.
	kind storekeys.Kind
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

// plan lists everything one photo contributes to the object store, asking the
// question once per kind of object the store can hold rather than once per kind
// its author remembered. That is why it walks storekeys.Kinds() instead of
// calling the planners in a row: a new prefix in the store is a new kind, and
// planFor then fails to compile until this package says what happens to it.
//
// What comes back is the original, the metadata sidecar when one exists on the
// local disk, every thumbnail size that currently sits in the local cache, and
// every streaming segment the store already holds for the photo's video. Nothing
// missing is generated here — a migration is the wrong place to spend an
// afternoon of CPU on regenerable artifacts — but everything present travels, so
// no catalogue row is left pointing at bytes that stayed behind.
func (m *Migrator) plan(ctx context.Context, item Item) ([]object, error) {
	var objects []object
	for _, kind := range storekeys.Kinds() {
		planned, err := m.planKind(ctx, kind, item)
		if err != nil {
			return nil, err
		}
		objects = append(objects, planned...)
	}
	return objects, nil
}

// planKind returns the objects of one kind that item contributes.
//
// The switch has no default clause on purpose: a new kind of object in the store
// must be planned — or explicitly declined — here before this package compiles
// cleanly again. The kinds that belong to nobody's photo are declined by name: a
// database dump and a half-written upload are not library content, and a foreign
// object belongs to whoever put it there.
func (m *Migrator) planKind(ctx context.Context, kind storekeys.Kind, item Item) ([]object, error) {
	switch kind {
	case storekeys.KindOriginal:
		return []object{m.planOriginal(item)}, nil
	case storekeys.KindSidecar:
		return m.planSidecar(ctx, item)
	case storekeys.KindFamilies:
		// The genealogy export belongs to the library, not to any one photo, so it
		// is declined here and moved once per run by planLibrary. Planning it per
		// photo would upload the same file once for every photo in the archive.
		return nil, nil
	case storekeys.KindThumbnail:
		return m.planThumbs(item.FileHash, thumb.SizeNames())
	case storekeys.KindHLS:
		return m.planHLS(ctx, item)
	case storekeys.KindDump, storekeys.KindPartial, storekeys.KindForeign:
		return nil, nil
	}
	return nil, nil // unreachable: every kind is decided above.
}

// planOriginal returns the one object every photo has: the media file itself,
// whose size and digest the catalogue already knows, so planning it costs no
// disk access at all.
func (m *Migrator) planOriginal(item Item) object {
	return object{
		relPath: item.FilePath,
		kind:    storekeys.KindOriginal,
		size:    item.FileSize,
		mime:    mimeOr(item.FileMIME),
		digest:  func(context.Context) (string, error) { return item.FileHash, nil },
		open: func(ctx context.Context) (io.ReadCloser, error) {
			return m.cfg.Source.Open(ctx, item.FilePath)
		},
	}
}

// planHLS lists the streaming segments the source already holds for the photo's
// video: the initialisation segment and the media segments of every rendition,
// under hls/<file_hash>/.
//
// They are moved rather than left behind because the catalogue's rendition rows
// are not touched by a migration. A row is a promise that a player can fetch
// what it describes, so segments that stayed on the emptied disk would turn
// every video in the library into a playlist of 404s — the library looking
// perfectly healthy the whole time. Re-encoding them instead would be hours of
// ffmpeg for bytes that already exist.
//
// A photo that is not a video, or a video that was never encoded, has no such
// keys and contributes nothing; the listing is asked for unconditionally because
// it is one cheap prefix lookup and the catalogue row does not reliably say
// which photos have been encoded.
func (m *Migrator) planHLS(ctx context.Context, item Item) ([]object, error) {
	prefix, err := hls.PrefixFor(item.FileHash)
	if err != nil {
		return nil, fmt.Errorf("storagemigrate: streaming prefix for %s: %w", item.UID, err)
	}
	var planned []object
	err = m.cfg.Source.KeysWithPrefix(ctx, prefix, func(key string) error {
		info, statErr := m.cfg.Source.Stat(ctx, key)
		if statErr != nil {
			return fmt.Errorf("storagemigrate: stat segment %s: %w", key, statErr)
		}
		planned = append(planned, object{
			relPath: key,
			kind:    storekeys.KindHLS,
			size:    info.Size(),
			mime:    hls.MIMEFor(path.Base(key)),
			digest:  func(ctx context.Context) (string, error) { return hashSource(ctx, m.cfg.Source, key) },
			open:    func(ctx context.Context) (io.ReadCloser, error) { return m.cfg.Source.Open(ctx, key) },
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("storagemigrate: listing segments of %s: %w", item.UID, err)
	}
	return planned, nil
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
		kind:    storekeys.KindSidecar,
		size:    info.Size(),
		mime:    sidecarexport.MIME,
		digest:  func(ctx context.Context) (string, error) { return hashSource(ctx, m.cfg.Source, key) },
		open:    func(ctx context.Context) (io.ReadCloser, error) { return m.cfg.Source.Open(ctx, key) },
	}}, nil
}

// planLibrary returns the objects that belong to the library as a whole rather
// than to any photo: today only the genealogy export, families.yaml at the root
// of the store.
//
// It is planned separately from every photo and moved once per run. Like a
// sidecar it is disaster-recovery data rather than a regenerable artifact — it
// is the only copy of the family tree outside the database — so it travels with
// the originals instead of being left on the emptied disk. A library that has
// never written one has nothing to move, which is not an error.
func (m *Migrator) planLibrary(ctx context.Context) ([]object, error) {
	key := familyexport.Key
	info, err := m.cfg.Source.Stat(ctx, key)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storagemigrate: stat family export %s: %w", key, err)
	}
	return []object{{
		relPath: key,
		kind:    storekeys.KindFamilies,
		size:    info.Size(),
		mime:    familyexport.MIME,
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
			kind:    storekeys.KindThumbnail,
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
