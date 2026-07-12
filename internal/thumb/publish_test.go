package thumb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/panbotka/kukatko/internal/storage"
)

// publishingFS wraps a real storage.FS so a Thumbnailer generates thumbnails from
// real originals on disk (via the embedded Materialize) while presenting the
// store as one that publishes object URLs. It records every Put the thumbnailer
// makes and can be told to fail the Put for a chosen size, standing in for an
// object-store backend such as R2.
type publishingFS struct {
	*storage.FS
	mu     sync.Mutex
	puts   map[string]storage.StoredFile
	failOn string // size name whose Put fails, or "" to never fail
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
