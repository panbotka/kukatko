package thumb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/storage"
)

// publishingFS wraps a real storage.FS so a Thumbnailer generates thumbnails from
// real originals on disk (via the embedded Materialize) while presenting the
// store as one that publishes object URLs. It records every Put the thumbnailer
// makes and can be told to fail the Put for a chosen size, standing in for an
// object-store backend such as R2.
type publishingFS struct {
	*storage.FS
	mu      sync.Mutex
	puts    map[string]storage.StoredFile
	putCall int    // how many times Put was called, successful or not
	lists   int    // how many times the published keys were listed
	failOn  string // size name whose Put fails, or "" to never fail
	listErr error  // when set, every prefix listing fails with it
}

// newPublishingFS wraps store as a publishing backend that records its Puts.
func newPublishingFS(store *storage.FS) *publishingFS {
	return &publishingFS{FS: store, puts: make(map[string]storage.StoredFile)}
}

// URL reports a non-empty published address for relPath, marking this backend as
// one whose thumbnails must be uploaded rather than served from the local cache.
func (p *publishingFS) URL(relPath string) string {
	return "https://cdn.example/" + relPath + "?sig=test"
}

// Put records the uploaded object's identity keyed by its RelPath, draining the
// stream to confirm it is readable and its length matches the declared size (as
// the real backends verify). It returns an error when file.RelPath names the
// configured failing size, simulating a backend that rejects the write.
func (p *publishingFS) Put(_ context.Context, src io.Reader, file storage.StoredFile) error {
	p.mu.Lock()
	p.putCall++
	p.mu.Unlock()
	if p.failOn != "" && strings.HasSuffix(file.RelPath, p.failOn+".jpg") {
		return errors.New("simulated put failure")
	}
	length, err := io.Copy(io.Discard, src)
	if err != nil {
		return err
	}
	if length != file.Size {
		return fmt.Errorf("stream length %d != declared size %d", length, file.Size)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.puts[file.RelPath] = file
	return nil
}

// recorded returns the identity of the object published at relPath.
func (p *publishingFS) recorded(relPath string) (storage.StoredFile, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	sf, ok := p.puts[relPath]
	return sf, ok
}

// KeysWithPrefix lists the objects this fake bucket holds — the recorded Puts,
// not the wrapped filesystem's originals — so the thumbnailer sees the bucket it
// publishes to rather than the disk it reads originals from. It shadows the
// embedded FS's own implementation on purpose.
func (p *publishingFS) KeysWithPrefix(_ context.Context, prefix string, yield func(key string) error) error {
	p.mu.Lock()
	p.lists++
	if p.listErr != nil {
		p.mu.Unlock()
		return p.listErr
	}
	keys := make([]string, 0, len(p.puts))
	for key := range p.puts {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	p.mu.Unlock()
	for _, key := range keys {
		if err := yield(key); err != nil {
			return err
		}
	}
	return nil
}

// counts returns how many Put calls and prefix listings the fake has seen.
func (p *publishingFS) counts() (puts, lists int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.putCall, p.lists
}

// nonListingFS is a publishing backend that cannot enumerate its keys, standing
// in for a store the thumbnailer may not ask "what do you already hold?". It
// hides the embedded FS's KeysWithPrefix behind a non-embedding wrapper, so the
// type-assertion in dropPublished fails.
type nonListingFS struct {
	inner *publishingFS
}

// URL reports a published address, marking this backend as one that uploads.
func (n *nonListingFS) URL(relPath string) string { return n.inner.URL(relPath) }

// Put delegates to the wrapped publishing fake.
func (n *nonListingFS) Put(ctx context.Context, src io.Reader, file storage.StoredFile) error {
	return n.inner.Put(ctx, src, file)
}

// Store delegates to the wrapped publishing fake.
func (n *nonListingFS) Store(
	ctx context.Context, src io.Reader, takenAt time.Time, originalName string,
) (storage.StoredFile, error) {
	return n.inner.Store(ctx, src, takenAt, originalName)
}

// Head delegates to the wrapped publishing fake.
func (n *nonListingFS) Head(ctx context.Context, relPath string) (storage.StoredFile, error) {
	return n.inner.Head(ctx, relPath)
}

// Check delegates to the wrapped publishing fake.
func (n *nonListingFS) Check(ctx context.Context) error { return n.inner.Check(ctx) }

// Open delegates to the wrapped publishing fake.
func (n *nonListingFS) Open(ctx context.Context, relPath string) (io.ReadCloser, error) {
	return n.inner.Open(ctx, relPath)
}

// Stat delegates to the wrapped publishing fake.
func (n *nonListingFS) Stat(ctx context.Context, relPath string) (os.FileInfo, error) {
	return n.inner.Stat(ctx, relPath)
}

// Delete delegates to the wrapped publishing fake.
func (n *nonListingFS) Delete(ctx context.Context, relPath string) error {
	return n.inner.Delete(ctx, relPath)
}

// Materialize delegates to the wrapped publishing fake.
func (n *nonListingFS) Materialize(ctx context.Context, relPath string) (string, func(), error) {
	return n.inner.Materialize(ctx, relPath)
}

// countingFS wraps a storage.FS to count Put calls while leaving URL empty, so it
// behaves as a non-publishing filesystem backend.
type countingFS struct {
	*storage.FS
	mu   sync.Mutex
	puts int
}

// Put increments the call counter and delegates to the wrapped filesystem store.
func (c *countingFS) Put(ctx context.Context, src io.Reader, file storage.StoredFile) error {
	c.mu.Lock()
	c.puts++
	c.mu.Unlock()
	return c.FS.Put(ctx, src, file)
}

// TestGenerate_publishesEachSizeToObjectStore proves that on a backend which
// publishes object URLs every generated size is uploaded under its canonical
// cache key, with the JPEG media type and an identity matching the bytes left in
// the local cache.
func TestGenerate_publishesEachSizeToObjectStore(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	pub := newPublishingFS(store)
	th := New(pub, filepath.Join(root, "cache"))
	photo := storeJPEG(t, store, 800, 600, 1)

	if _, err := th.GenerateAll(context.Background(), photo); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}

	for _, size := range SizeNames() {
		rel, err := RelPath(photo.FileHash, size)
		if err != nil {
			t.Fatalf("RelPath(%s): %v", size, err)
		}
		sf, ok := pub.recorded(rel)
		if !ok {
			t.Errorf("size %s was not published to %s", size, rel)
			continue
		}
		if sf.MIME != thumbMIME {
			t.Errorf("size %s published MIME = %q, want %q", size, sf.MIME, thumbMIME)
		}
		abs, err := th.Path(photo.FileHash, size)
		if err != nil {
			t.Fatalf("Path(%s): %v", size, err)
		}
		wantDigest, wantSize, err := hashAndSize(abs)
		if err != nil {
			t.Fatalf("hashAndSize(%s): %v", abs, err)
		}
		if sf.Hash != wantDigest {
			t.Errorf("size %s published Hash = %s, want %s", size, sf.Hash, wantDigest)
		}
		if sf.Size != wantSize {
			t.Errorf("size %s published Size = %d, want %d", size, sf.Size, wantSize)
		}
	}
}

// TestGenerate_publishFailureRemovesCachedFile proves a failed upload is not
// silently tolerated: Generate returns the error and removes the local cache
// file, so a later Generate re-encodes and re-uploads the size rather than
// leaving a thumbnail whose published URL would never resolve.
func TestGenerate_publishFailureRemovesCachedFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	pub := newPublishingFS(store)
	pub.failOn = GridSize
	th := New(pub, filepath.Join(root, "cache"))
	photo := storeJPEG(t, store, 400, 300, 1)

	if _, err := th.Generate(context.Background(), photo, GridSize); err == nil {
		t.Fatal("Generate: expected error from failed publish, got nil")
	}
	abs, err := th.Path(photo.FileHash, GridSize)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if fileExists(abs) {
		t.Errorf("cache file %s still present after failed publish; want removed", abs)
	}
}

// TestGenerate_skipsSizesAlreadyInTheBucket proves the published object — not the
// local disk alone — decides whether a size is done. After the cache is wiped (as
// the production import's pruner does, thumbnails being far larger than the disk
// they would fill), a second GenerateAll re-encodes and re-uploads nothing: it
// asks the store once and finds every size already there.
func TestGenerate_skipsSizesAlreadyInTheBucket(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	pub := newPublishingFS(store)
	cache := filepath.Join(root, "cache")
	th := New(pub, cache)
	photo := storeJPEG(t, store, 800, 600, 1)

	if _, err := th.GenerateAll(t.Context(), photo); err != nil {
		t.Fatalf("first GenerateAll: %v", err)
	}
	firstPuts, firstLists := pub.counts()
	if firstPuts != len(SizeNames()) {
		t.Fatalf("first GenerateAll published %d size(s), want %d", firstPuts, len(SizeNames()))
	}
	if err := os.RemoveAll(cache); err != nil {
		t.Fatalf("pruning the cache: %v", err)
	}

	if _, err := th.GenerateAll(t.Context(), photo); err != nil {
		t.Fatalf("second GenerateAll: %v", err)
	}

	puts, lists := pub.counts()
	if puts != firstPuts {
		t.Errorf("second GenerateAll made %d further Put(s), want 0", puts-firstPuts)
	}
	if lists-firstLists != 1 {
		t.Errorf("second GenerateAll listed the store %d time(s) for one photo, want exactly 1", lists-firstLists)
	}
	for _, size := range SizeNames() {
		abs, err := th.Path(photo.FileHash, size)
		if err != nil {
			t.Fatalf("Path(%s): %v", size, err)
		}
		if fileExists(abs) {
			t.Errorf("size %s was re-encoded into the cache; want left to the bucket", size)
		}
	}
}

// TestGenerate_warmCacheIsNotListed proves the store is only consulted when
// something is actually missing locally: a fully cached photo answers from disk
// and costs no round trip at all. A photo whose sizes are missing pays exactly one
// listing — the price of not re-encoding and re-uploading all of them.
func TestGenerate_warmCacheIsNotListed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	pub := newPublishingFS(store)
	th := New(pub, filepath.Join(root, "cache"))
	photo := storeJPEG(t, store, 400, 300, 1)

	if _, err := th.GenerateAll(t.Context(), photo); err != nil {
		t.Fatalf("first GenerateAll: %v", err)
	}
	if _, lists := pub.counts(); lists != 1 {
		t.Fatalf("cold GenerateAll listed the store %d time(s), want exactly 1", lists)
	}

	if _, err := th.GenerateAll(t.Context(), photo); err != nil {
		t.Fatalf("second GenerateAll: %v", err)
	}
	if _, lists := pub.counts(); lists != 1 {
		t.Errorf("store was listed %d time(s) in total, want no listing at all with a warm cache", lists)
	}
}

// TestGenerate_failedPublishLeavesTheSizeUngenerated proves the invariant the
// bucket check rests on: a size whose upload failed is in neither the cache nor
// the store, so the next Generate encodes and uploads it again rather than
// treating it as done.
func TestGenerate_failedPublishLeavesTheSizeUngenerated(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	pub := newPublishingFS(store)
	pub.failOn = GridSize
	th := New(pub, filepath.Join(root, "cache"))
	photo := storeJPEG(t, store, 400, 300, 1)

	if _, err := th.Generate(t.Context(), photo, GridSize); err == nil {
		t.Fatal("Generate: expected error from failed publish, got nil")
	}
	rel, err := RelPath(photo.FileHash, GridSize)
	if err != nil {
		t.Fatalf("RelPath: %v", err)
	}
	if _, ok := pub.recorded(rel); ok {
		t.Fatalf("failed publish left an object at %s", rel)
	}

	// The retry must do the work, not skip it: the size is neither cached nor
	// published, so an empty listing has to send it back through the encoder.
	pub.failOn = ""
	if _, err := th.Generate(t.Context(), photo, GridSize); err != nil {
		t.Fatalf("retry Generate: %v", err)
	}
	if _, ok := pub.recorded(rel); !ok {
		t.Errorf("retry did not publish %s; a failed upload must not count as generated", rel)
	}
	abs, err := th.Path(photo.FileHash, GridSize)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if !fileExists(abs) {
		t.Errorf("retry left no cache file at %s", abs)
	}
}

// TestGenerate_listingFailureFallsBackToEncoding proves a store that cannot
// answer "do you hold this?" costs speed, never correctness: the sizes are
// encoded and uploaded as they were before the check existed.
func TestGenerate_listingFailureFallsBackToEncoding(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	pub := newPublishingFS(store)
	pub.listErr = errors.New("bucket unreachable")
	th := New(pub, filepath.Join(root, "cache"))
	photo := storeJPEG(t, store, 400, 300, 1)

	if _, err := th.Generate(t.Context(), photo, GridSize); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	rel, err := RelPath(photo.FileHash, GridSize)
	if err != nil {
		t.Fatalf("RelPath: %v", err)
	}
	if _, ok := pub.recorded(rel); !ok {
		t.Errorf("size was not published after a failed listing; want the encode to proceed")
	}
}

// TestGenerate_backendThatCannotListStillGenerates proves the check is optional:
// a publishing store with no prefix listing falls back to encoding rather than
// skipping a size it cannot vouch for.
func TestGenerate_backendThatCannotListStillGenerates(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	pub := newPublishingFS(store)
	th := New(&nonListingFS{inner: pub}, filepath.Join(root, "cache"))
	photo := storeJPEG(t, store, 400, 300, 1)

	if _, err := th.Generate(t.Context(), photo, GridSize); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, lists := pub.counts(); lists != 0 {
		t.Errorf("store was listed %d time(s) through a backend that cannot list, want 0", lists)
	}
	rel, err := RelPath(photo.FileHash, GridSize)
	if err != nil {
		t.Fatalf("RelPath: %v", err)
	}
	if _, ok := pub.recorded(rel); !ok {
		t.Errorf("size was not published; a store that cannot list must still be written to")
	}
}

// TestRegenerateAll_rebuildsEvenWhenPublished proves the force path is not
// weakened by the bucket check: rebuilding a stale or corrupt thumbnail must
// overwrite the object that is already there.
func TestRegenerateAll_rebuildsEvenWhenPublished(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	pub := newPublishingFS(store)
	cache := filepath.Join(root, "cache")
	th := New(pub, cache)
	photo := storeJPEG(t, store, 400, 300, 1)

	if _, err := th.GenerateAll(t.Context(), photo); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	if err := os.RemoveAll(cache); err != nil {
		t.Fatalf("pruning the cache: %v", err)
	}
	firstPuts, _ := pub.counts()

	if _, err := th.RegenerateAll(t.Context(), photo); err != nil {
		t.Fatalf("RegenerateAll: %v", err)
	}
	if puts, _ := pub.counts(); puts != firstPuts+len(SizeNames()) {
		t.Errorf("RegenerateAll made %d Put(s), want %d", puts-firstPuts, len(SizeNames()))
	}
}

// TestGenerate_filesystemBackendDoesNotPublish proves the upload is gated on the
// backend publishing URLs: a filesystem backend, which serves thumbnails from the
// cache directory, receives no Put calls.
func TestGenerate_filesystemBackendDoesNotPublish(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	counting := &countingFS{FS: store}
	th := New(counting, filepath.Join(root, "cache"))
	photo := storeJPEG(t, store, 400, 300, 1)

	if _, err := th.GenerateAll(context.Background(), photo); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	counting.mu.Lock()
	defer counting.mu.Unlock()
	if counting.puts != 0 {
		t.Errorf("filesystem backend received %d Put calls, want 0", counting.puts)
	}
}
