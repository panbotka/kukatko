package thumb

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/storage"
)

// countingObserver counts ObserveThumbnail calls. It is safe for concurrent use
// because sizes are encoded in parallel.
type countingObserver struct {
	calls atomic.Int64
	mu    sync.Mutex
	total time.Duration
}

// ObserveThumbnail records one generation timing.
func (o *countingObserver) ObserveThumbnail(d time.Duration) {
	o.calls.Add(1)
	o.mu.Lock()
	o.total += d
	o.mu.Unlock()
}

// TestWithObserver_recordsEachGeneratedSize verifies the observer fires once per
// generated size and is not fired for sizes skipped as already cached.
func TestWithObserver_recordsEachGeneratedSize(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	obs := &countingObserver{}
	th := New(store, filepath.Join(root, "cache"), WithObserver(obs))
	photo := storeJPEG(t, store, 800, 600, 0)

	if _, err := th.Generate(context.Background(), photo, "fit_720", "tile_224"); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got := obs.calls.Load(); got != 2 {
		t.Fatalf("observer calls after first generate = %d, want 2", got)
	}

	// Re-generating the same sizes is an idempotent skip: the observer must not
	// fire again for cache hits.
	if _, err := th.Generate(context.Background(), photo, "fit_720", "tile_224"); err != nil {
		t.Fatalf("second Generate: %v", err)
	}
	if got := obs.calls.Load(); got != 2 {
		t.Errorf("observer calls after cache-hit generate = %d, want 2 (no new work)", got)
	}
}

// TestWithObserver_nilIgnored verifies passing a nil observer keeps the no-op
// default so generation does not panic.
func TestWithObserver_nilIgnored(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	th := New(store, filepath.Join(root, "cache"), WithObserver(nil))
	photo := storeJPEG(t, store, 400, 300, 0)

	if _, err := th.Generate(context.Background(), photo, "tile_100"); err != nil {
		t.Fatalf("Generate with nil observer: %v", err)
	}
}
