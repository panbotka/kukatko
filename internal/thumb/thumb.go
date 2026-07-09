// Package thumb generates and caches derived JPEG images (thumbnails and
// previews) for catalogued photos, keeping Kukátko's binary CGO-free.
//
// Sources in pure-Go formats (JPEG, PNG, WebP) are decoded directly; HEIC and
// RAW originals are pre-decoded to an intermediate JPEG by the imgconvert
// package (shelling out to heif-convert and exiftool) before resizing. EXIF
// orientation is applied automatically so every thumbnail is in display
// orientation.
//
// Derived images live under the configured cache root in a SHA256-sharded tree
//
//	thumb/<aa>/<bb>/<cc>/<hash>_<size>.jpg
//
// where aa/bb/cc are the first three byte-pairs of the original's hex file hash.
// The cache is fully regenerable from originals and generation is idempotent:
// a size already present on disk is never re-encoded or rewritten.
package thumb

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
)

// Sentinel errors returned by the thumbnailer so callers (HTTP handlers, the
// job worker, tests) can branch with errors.Is.
var (
	// ErrUnknownSize indicates a size name that is not in the registry.
	ErrUnknownSize = errors.New("thumb: unknown size")
	// ErrInvalidHash indicates a file hash that is empty or not a hex string of
	// at least the three byte-pairs needed to shard the cache tree.
	ErrInvalidHash = errors.New("thumb: invalid file hash")
	// ErrNotCached indicates a requested thumbnail is not present in the cache.
	ErrNotCached = errors.New("thumb: thumbnail not cached")
)

const (
	// cacheSubdir is the top-level directory under the cache root for thumbs.
	cacheSubdir = "thumb"
	// shardLen is the number of leading hex characters consumed by each of the
	// three cache-tree shard levels (aa/bb/cc).
	shardLen = 2
	// minHashLen is the shortest hash accepted: enough hex to form all three
	// shard levels.
	minHashLen = shardLen * 3
	// dirPerm and filePerm match the storage layer's owner-only permissions.
	dirPerm  = 0o750
	filePerm = 0o640
)

// Thumbnailer generates and caches derived images. It is safe for concurrent
// use; callers may invoke Generate/GenerateAll from many goroutines (e.g. one
// per photo in a job queue) and the bounded internal concurrency parallelises
// the per-size encode work for a single photo.
type Thumbnailer struct {
	// originals materializes a photo's stored original as a local file (the
	// HEIC/RAW shell-out and the vips engine both need a real file path).
	originals storage.Storage
	// cacheDir is the configured cache root (storage.cache_path).
	cacheDir string
	// workers bounds the number of sizes encoded concurrently per photo.
	workers int
	// vipsBin is the resolved vipsthumbnail path when the vips engine is enabled,
	// or "" for the pure-Go default. See WithVips.
	vipsBin string
	// observer receives per-size generation timing; never nil after New.
	observer Observer
}

// Observer receives the wall-clock time taken to generate one thumbnail size.
// It is satisfied by *metrics.Registry; tests use a fake. Implementations must
// be safe for concurrent use, since sizes are encoded in parallel.
type Observer interface {
	// ObserveThumbnail records that generating one size took d.
	ObserveThumbnail(d time.Duration)
}

// nopObserver is the default Observer when none is configured; it does nothing.
type nopObserver struct{}

// ObserveThumbnail does nothing.
func (nopObserver) ObserveThumbnail(time.Duration) {}

// Option customises a Thumbnailer at construction time.
type Option func(*Thumbnailer)

// WithObserver sets the Observer that receives per-size generation timing. A
// nil observer is ignored, leaving the no-op default in place.
func WithObserver(obs Observer) Option {
	return func(t *Thumbnailer) {
		if obs != nil {
			t.observer = obs
		}
	}
}

// WithConcurrency sets the maximum number of sizes encoded in parallel for a
// single photo. Values below 1 are ignored (the default is GOMAXPROCS).
func WithConcurrency(n int) Option {
	return func(t *Thumbnailer) {
		if n >= 1 {
			t.workers = n
		}
	}
}

// New returns a Thumbnailer that reads originals through store and writes the
// derived-image cache under cacheDir (the configured storage.cache_path).
func New(store storage.Storage, cacheDir string, opts ...Option) *Thumbnailer {
	t := &Thumbnailer{
		originals: store,
		cacheDir:  cacheDir,
		workers:   max(runtime.GOMAXPROCS(0), 1),
		observer:  nopObserver{},
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// RelPath returns the slash-separated cache path of the thumbnail for the given
// file hash and size — thumb/<aa>/<bb>/<cc>/<hash>_<size>.jpg — whether or not
// it exists yet. It doubles as the object key under which a remote storage
// backend keeps the thumbnail, which is why the layout is exported rather than
// derived a second time elsewhere. It returns ErrUnknownSize for an unregistered
// size or ErrInvalidHash for a malformed hash.
func RelPath(hash, size string) (string, error) {
	if !IsValidSize(size) {
		return "", fmt.Errorf("%w: %q", ErrUnknownSize, size)
	}
	return cacheRelPath(hash, size)
}

// Path returns the absolute filesystem path of the thumbnail for the given file
// hash and size, whether or not it exists yet. It returns ErrUnknownSize for an
// unregistered size or ErrInvalidHash for a malformed hash.
func (t *Thumbnailer) Path(hash, size string) (string, error) {
	rel, err := RelPath(hash, size)
	if err != nil {
		return "", err
	}
	return filepath.Join(t.cacheDir, filepath.FromSlash(rel)), nil
}

// Open opens the cached thumbnail for the given hash and size for reading. The
// caller owns the returned reader and must close it. It returns ErrNotCached
// (wrapping os.ErrNotExist) when the thumbnail has not been generated.
func (t *Thumbnailer) Open(hash, size string) (io.ReadCloser, error) {
	abs, err := t.Path(hash, size)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(abs) //nolint:gosec // G304: abs is built from a validated hex hash and registry size.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s/%s", ErrNotCached, hash, size)
		}
		return nil, fmt.Errorf("thumb: open cached %s/%s: %w", hash, size, err)
	}
	return f, nil
}

// Remove deletes every registered thumbnail size cached for the given file
// hash, leaving no derived images behind when its source photo is purged. It is
// idempotent: sizes that were never generated are skipped, so removing twice (or
// removing a hash with no cache) is not an error. It returns ErrInvalidHash for
// a malformed hash, or the first hard I/O error encountered while deleting (a
// missing file is not such an error).
func (t *Thumbnailer) Remove(hash string) error {
	if err := validateHash(hash); err != nil {
		return err
	}
	for _, size := range SizeNames() {
		abs, err := t.Path(hash, size)
		if err != nil {
			return err
		}
		if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("thumb: removing cached %s/%s: %w", hash, size, err)
		}
	}
	return nil
}

// GenerateAll generates every registered size for photo. It is a thin wrapper
// over Generate using SizeNames().
func (t *Thumbnailer) GenerateAll(ctx context.Context, photo photos.Photo) (map[string]string, error) {
	return t.Generate(ctx, photo, SizeNames()...)
}

// Generate produces the requested thumbnail sizes for photo and returns a map
// from each requested size name to its absolute cache path. Sizes already on
// disk are kept untouched (idempotent skip); only the missing ones are encoded,
// in parallel up to the configured concurrency, after decoding the original
// exactly once.
//
// It returns ErrUnknownSize if any requested size is unregistered (before any
// work is done), ErrInvalidHash for a malformed photo file hash, or a wrapped
// error from decoding/encoding/IO. With no sizes it returns an empty map.
func (t *Thumbnailer) Generate(
	ctx context.Context, photo photos.Photo, sizes ...string,
) (map[string]string, error) {
	if len(sizes) == 0 {
		return map[string]string{}, nil
	}

	result, needed, err := t.planSizes(photo.FileHash, sizes)
	if err != nil {
		return nil, err
	}
	if len(needed) == 0 {
		return result, nil
	}

	// Both engines shell out to tools that take a filename, so the original has to
	// exist as a local file for the rest of this call. Materializing it once here
	// keeps a remote backend from fetching the same original twice when vips
	// declines and the pure-Go engine takes over.
	src, cleanup, err := t.originals.Materialize(ctx, photo.FilePath)
	if err != nil {
		return nil, fmt.Errorf("thumb: materializing original: %w", err)
	}
	defer cleanup()

	// Fast path: shell out to vipsthumbnail for directly-supported originals. On
	// any failure it returns false and we fall through to the pure-Go engine, so
	// output never depends on vips succeeding — only speed does.
	if t.tryVips(ctx, photo, src, needed, result) {
		return result, nil
	}

	img, err := decodeAndOrient(ctx, src, photo.FileOrientation)
	if err != nil {
		return nil, err
	}

	group, gctx := errgroup.WithContext(ctx)
	group.SetLimit(t.workers)
	for _, name := range needed {
		group.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			return t.writeSize(img, name, result[name])
		})
	}
	if err := group.Wait(); err != nil {
		return nil, fmt.Errorf("thumb: generate sizes: %w", err)
	}
	return result, nil
}

// planSizes validates every requested size and the hash, builds the full
// size→absolute-path result map, and returns the subset of sizes whose cache
// file is not yet present (in canonical order, deduplicated).
func (t *Thumbnailer) planSizes(hash string, sizes []string) (result map[string]string, needed []string, err error) {
	result = make(map[string]string, len(sizes))
	needed = make([]string, 0, len(sizes))
	for _, name := range sizes {
		if !IsValidSize(name) {
			return nil, nil, fmt.Errorf("%w: %q", ErrUnknownSize, name)
		}
		abs, pathErr := t.Path(hash, name)
		if pathErr != nil {
			return nil, nil, pathErr
		}
		if _, seen := result[name]; seen {
			continue
		}
		result[name] = abs
		if !fileExists(abs) {
			needed = append(needed, name)
		}
	}
	return result, needed, nil
}

// writeSize resizes the already-decoded image for the named size, JPEG-encodes
// it, and writes it atomically to absPath.
func (t *Thumbnailer) writeSize(img image.Image, name, absPath string) error {
	start := time.Now()
	resized, err := resizeForSpec(img, sizes[name])
	if err != nil {
		return err
	}
	data, err := encodeJPEG(resized, sizes[name].Quality)
	if err != nil {
		return fmt.Errorf("thumb: %s: %w", name, err)
	}
	if err := writeFileAtomic(absPath, data); err != nil {
		return fmt.Errorf("thumb: write %s: %w", name, err)
	}
	t.observer.ObserveThumbnail(time.Since(start))
	return nil
}

// cacheRelPath returns the slash-separated cache path
// thumb/<aa>/<bb>/<cc>/<hash>_<size>.jpg for a validated hash, or ErrInvalidHash.
func cacheRelPath(hash, size string) (string, error) {
	if err := validateHash(hash); err != nil {
		return "", err
	}
	name := hash + "_" + size + ".jpg"
	return path.Join(cacheSubdir, hash[0:shardLen], hash[shardLen:shardLen*2], hash[shardLen*2:shardLen*3], name), nil
}

// validateHash reports whether hash is a lowercase hex string long enough to
// shard, returning ErrInvalidHash otherwise.
func validateHash(hash string) error {
	if len(hash) < minHashLen {
		return fmt.Errorf("%w: %q too short", ErrInvalidHash, hash)
	}
	for _, r := range hash {
		if !isHexDigit(r) {
			return fmt.Errorf("%w: %q not hex", ErrInvalidHash, hash)
		}
	}
	return nil
}

// isHexDigit reports whether r is a lowercase hexadecimal digit.
func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
}

// fileExists reports whether a regular file exists at absPath.
func fileExists(absPath string) bool {
	info, err := os.Stat(absPath)
	return err == nil && info.Mode().IsRegular()
}

// writeFileAtomic writes data to absPath via a temp file in the same directory
// followed by an atomic rename, creating parent directories as needed. The
// rename makes concurrent writers of identical content converge race-free and
// guarantees no half-written thumbnail is ever observed at its final path.
func writeFileAtomic(absPath string, data []byte) error {
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("create cache dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(absPath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	if err := writeAndClose(tmp, data); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, filePerm); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmpPath, absPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// writeAndClose writes data to f and closes it, returning the first error.
func writeAndClose(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	return nil
}
