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
//
// On a backend that publishes its objects the same relative path is the object
// key, and the bucket — not the local disk — is where the size durably lives. So
// "already present" is asked of the bucket too: before encoding anything,
// Generate lists the photo's own key prefix once and drops every size the store
// already holds. That is what keeps a cold cache cheap. A library whose cache was
// pruned (thumbnails cost megabytes per photo, and an import can outgrow the
// disk) is then re-thumbnailed for the price of one listing per photo instead of
// a full re-encode and re-upload.
package thumb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// CacheSubdir is the top-level directory that holds every cached thumbnail: a
// subdirectory of the local cache root, and — on a publishing backend, where the
// thumbnails are uploaded under the same relative path — the object-key prefix
// that owns them in the bucket. It is exported so the operations that reason
// about whole prefixes rather than single files (a library wipe) can name the one
// this package owns instead of hardcoding the string.
const CacheSubdir = "thumb"

const (
	// thumbMIME is the media type of every cached thumbnail; the thumbnailer
	// encodes nothing but JPEG. It is the type a publishing backend serves the
	// uploaded object as.
	thumbMIME = "image/jpeg"
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
	// maxPixels caps the width×height of a source the pure-Go engine will fully
	// decode; a larger source is rejected before its bitmap is allocated so a
	// decompression bomb cannot OOM a worker. 0 disables the cap. See
	// WithMaxPixels.
	maxPixels int64
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

// WithMaxPixels caps the width×height of a source the pure-Go engine will fully
// decode: a larger original is rejected (with imgconvert.ErrImageTooLarge)
// before its bitmap is allocated, so a decompression bomb or an accidentally
// enormous panorama fails one thumbnail job instead of OOMing the worker. A
// non-positive value disables the cap.
func WithMaxPixels(n int64) Option {
	return func(t *Thumbnailer) {
		t.maxPixels = n
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

// GenerateAll generates every registered size for photo, skipping any already
// cached on disk. It is a thin wrapper over Generate using SizeNames().
func (t *Thumbnailer) GenerateAll(ctx context.Context, photo photos.Photo) (map[string]string, error) {
	return t.Generate(ctx, photo, SizeNames()...)
}

// RegenerateAll forces regeneration of every registered size for photo,
// overwriting any already-cached sizes in place (and republishing them to the
// object store on a publishing backend). Unlike GenerateAll it does not skip
// sizes already present on disk, so it rebuilds a stale or corrupt cache from
// the original; it backs the on-demand "regenerate thumbnail" service action.
// Each size is written atomically, so a failure leaves the previous cache
// intact. It returns the same size→path map and the same errors as Generate.
func (t *Thumbnailer) RegenerateAll(ctx context.Context, photo photos.Photo) (map[string]string, error) {
	return t.generate(ctx, photo, SizeNames(), true)
}

// Generate produces the requested thumbnail sizes for photo and returns a map
// from each requested size name to its absolute cache path. Sizes already on
// disk are kept untouched (idempotent skip), as are — on a backend that publishes
// its objects — sizes the store already holds, which one prefix listing per photo
// establishes; only the rest are encoded, in parallel up to the configured
// concurrency, after decoding the original exactly once. Use RegenerateAll to
// force-overwrite cached sizes instead.
//
// A size skipped because the object is already published leaves no local cache
// file behind, so its returned path need not exist: on such a backend the client
// fetches the object, and the application never reads the cache file. Only a
// backend that mints no URLs serves thumbnails from the cache, and there nothing
// is ever skipped on the strength of the store.
//
// It returns ErrUnknownSize if any requested size is unregistered (before any
// work is done), ErrInvalidHash for a malformed photo file hash, or a wrapped
// error from decoding/encoding/IO. With no sizes it returns an empty map.
func (t *Thumbnailer) Generate(
	ctx context.Context, photo photos.Photo, sizes ...string,
) (map[string]string, error) {
	return t.generate(ctx, photo, sizes, false)
}

// generate is the shared implementation of Generate and RegenerateAll. When
// force is true every requested size is (re)encoded even if a cache file already
// exists, overwriting it in place; otherwise cached sizes are skipped. See
// Generate for the returned map and error semantics.
func (t *Thumbnailer) generate(
	ctx context.Context, photo photos.Photo, sizes []string, force bool,
) (map[string]string, error) {
	if len(sizes) == 0 {
		return map[string]string{}, nil
	}

	result, needed, err := t.plan(ctx, photo.FileHash, sizes, force)
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

	img, err := decodeAndOrient(ctx, src, photo.FileOrientation, t.maxPixels)
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
			return t.writeSize(gctx, img, photo.FileHash, name, result[name])
		})
	}
	if err := group.Wait(); err != nil {
		return nil, fmt.Errorf("thumb: generate sizes: %w", err)
	}
	return result, nil
}

// plan resolves which of the requested sizes actually have to be encoded, and
// returns them together with the full size→absolute-path map. A size counts as
// done when its cache file is on disk or — unless force is set — when the storage
// backend already holds its object. force skips the store entirely: the point of
// a forced rebuild is to replace what is there.
func (t *Thumbnailer) plan(
	ctx context.Context, hash string, sizes []string, force bool,
) (result map[string]string, needed []string, err error) {
	result, needed, err = t.planSizes(hash, sizes, force)
	if err != nil || force {
		return result, needed, err
	}
	return result, t.dropPublished(ctx, hash, needed), nil
}

// planSizes validates every requested size and the hash, builds the full
// size→absolute-path result map, and returns the subset of sizes that must be
// encoded (in canonical order, deduplicated). When force is false that subset is
// the sizes whose cache file is not yet present; when force is true it is every
// requested size, so an already-cached size is rebuilt in place.
func (t *Thumbnailer) planSizes(
	hash string, sizes []string, force bool,
) (result map[string]string, needed []string, err error) {
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
		if force || !fileExists(abs) {
			needed = append(needed, name)
		}
	}
	return result, needed, nil
}

// dropPublished returns needed without the sizes the storage backend already
// holds as objects, so a cold local cache does not re-encode and re-upload what
// is durably in the bucket already. It answers for all of a photo's sizes with a
// single prefix listing — the sizes share the sharded key prefix derived from the
// file hash — rather than one Head per size, which would cost a round trip per
// size and could easily exceed the encode it saves.
//
// It applies only where a published object is what a client actually fetches:
// a backend that mints no URLs serves thumbnails from the local cache, so an
// object there would not make the size available and the cache file must be
// written. A backend that cannot list by prefix, an unusable hash, or a failed
// listing all fall back to encoding: being slower than necessary is a cost, while
// skipping a size that is not really there would leave a thumbnail no one can
// fetch.
//
// Only a completed upload puts an object under the key — Put verifies the stream
// against its declared identity and removes the object when it disagrees, and a
// failed upload additionally un-caches the local file — so an object's presence
// really does mean the size is published.
func (t *Thumbnailer) dropPublished(ctx context.Context, hash string, needed []string) []string {
	if len(needed) == 0 {
		return needed
	}
	lister, ok := t.originals.(storage.PrefixLister)
	if !ok {
		return needed
	}
	probe, err := RelPath(hash, needed[0])
	if err != nil || t.originals.URL(probe) == "" {
		return needed
	}
	published, err := t.publishedKeys(ctx, lister, hash)
	if err != nil {
		return needed
	}
	kept := make([]string, 0, len(needed))
	for _, name := range needed {
		rel, relErr := RelPath(hash, name)
		if relErr != nil || !published[rel] {
			kept = append(kept, name)
		}
	}
	return kept
}

// publishedKeys returns the set of object keys the store holds under the photo's
// cache prefix, from one listing. The prefix ends mid-filename (at the hash
// followed by the size separator), so the listing covers exactly this photo's
// sizes and none of the shard directory's other tenants.
func (t *Thumbnailer) publishedKeys(
	ctx context.Context, lister storage.PrefixLister, hash string,
) (map[string]bool, error) {
	prefix, err := objectPrefix(hash)
	if err != nil {
		return nil, err
	}
	keys := make(map[string]bool, len(SizeNames()))
	if err := lister.KeysWithPrefix(ctx, prefix, func(key string) error {
		keys[key] = true
		return nil
	}); err != nil {
		return nil, fmt.Errorf("thumb: listing published sizes for %s: %w", hash, err)
	}
	return keys, nil
}

// objectPrefix returns the key prefix every cached size of the given file hash
// shares — thumb/<aa>/<bb>/<cc>/<hash>_ — for a validated hash, or ErrInvalidHash.
// It is a literal string prefix, not a directory: the shard directory also holds
// the sizes of every other hash sharing those three byte-pairs.
func objectPrefix(hash string) (string, error) {
	dir, err := cacheDirRel(hash)
	if err != nil {
		return "", err
	}
	return dir + "/" + hash + "_", nil
}

// writeSize resizes the already-decoded image for the named size, JPEG-encodes
// it, writes it atomically to absPath (the local cache), and publishes it to the
// storage backend when that backend serves thumbnails from object URLs. hash is
// the photo's file hash, which keys the published object.
func (t *Thumbnailer) writeSize(ctx context.Context, img image.Image, hash, name, absPath string) error {
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
	return t.publishSize(ctx, hash, name, absPath)
}

// publishSize uploads the freshly written thumbnail at absPath to the storage
// backend under its canonical object key (RelPath(hash, size)), but only for a
// backend that publishes client-fetchable URLs — an object store such as R2,
// where mediaurl hands the client the object URL directly and the bytes must
// therefore live in the bucket. A local filesystem backend serves thumbnails
// from the cache directory and needs no upload, so its URL is empty and this is
// a no-op.
//
// When the upload fails the local cache file is removed, so the size counts as
// ungenerated and a later Generate re-encodes and re-uploads it. That preserves
// the invariant that on a publishing backend every cached size is also in the
// bucket — which is what lets a client's object URL resolve.
func (t *Thumbnailer) publishSize(ctx context.Context, hash, size, absPath string) error {
	rel, err := RelPath(hash, size)
	if err != nil {
		return err
	}
	if t.originals.URL(rel) == "" {
		return nil
	}
	digest, byteLen, err := hashAndSize(absPath)
	if err != nil {
		return err
	}
	file, err := os.Open(absPath) //nolint:gosec // G304: absPath is built from a validated hex hash and registry size.
	if err != nil {
		return fmt.Errorf("thumb: reopening %s for upload: %w", size, err)
	}
	defer func() { _ = file.Close() }()
	want := storage.StoredFile{Hash: digest, RelPath: rel, Size: byteLen, MIME: thumbMIME}
	if err := t.originals.Put(ctx, file, want); err != nil {
		_ = os.Remove(absPath)
		return fmt.Errorf("thumb: publishing %s: %w", size, err)
	}
	return nil
}

// hashAndSize returns the lowercase hex SHA256 digest and byte length of the file
// at absPath, streaming it so nothing is buffered whole in memory. Storage.Put
// verifies the uploaded stream against exactly these two values.
func hashAndSize(absPath string) (digest string, size int64, err error) {
	file, err := os.Open(absPath) //nolint:gosec // G304: absPath is built from a validated hex hash and registry size.
	if err != nil {
		return "", 0, fmt.Errorf("thumb: opening %s for hashing: %w", absPath, err)
	}
	defer func() { _ = file.Close() }()
	hasher := sha256.New()
	size, err = io.Copy(hasher, file)
	if err != nil {
		return "", 0, fmt.Errorf("thumb: hashing %s: %w", absPath, err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), size, nil
}

// cacheRelPath returns the slash-separated cache path
// thumb/<aa>/<bb>/<cc>/<hash>_<size>.jpg for a validated hash, or ErrInvalidHash.
func cacheRelPath(hash, size string) (string, error) {
	dir, err := cacheDirRel(hash)
	if err != nil {
		return "", err
	}
	return path.Join(dir, hash+"_"+size+".jpg"), nil
}

// cacheDirRel returns the slash-separated shard directory thumb/<aa>/<bb>/<cc>
// that holds every cached size of the given file hash, or ErrInvalidHash. It is
// the single definition of the shard layout; both the per-size path and the
// per-photo key prefix are built from it.
func cacheDirRel(hash string) (string, error) {
	if err := validateHash(hash); err != nil {
		return "", err
	}
	return path.Join(CacheSubdir, hash[0:shardLen], hash[shardLen:shardLen*2], hash[shardLen*2:shardLen*3]), nil
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
