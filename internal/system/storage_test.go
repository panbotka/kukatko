package system

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDirSize covers the empty path, a missing directory, and a populated tree
// with nested files and a non-regular entry.
func TestDirSize(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	if got, err := dirSize(ctx, ""); err != nil || got != 0 {
		t.Errorf("dirSize(empty) = (%d, %v), want (0, nil)", got, err)
	}

	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if got, err := dirSize(ctx, missing); err != nil || got != 0 {
		t.Errorf("dirSize(missing) = (%d, %v), want (0, nil)", got, err)
	}

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.bin"), 100)
	writeFile(t, filepath.Join(root, "sub", "b.bin"), 250)
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatalf("mkdir empty: %v", err)
	}

	got, err := dirSize(ctx, root)
	if err != nil {
		t.Fatalf("dirSize(root): %v", err)
	}
	if got != 350 {
		t.Errorf("dirSize(root) = %d, want 350", got)
	}
}

// TestDirSizeCancelled verifies a cancelled context aborts the walk with an
// error rather than completing silently.
func TestDirSizeCancelled(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.bin"), 10)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := dirSize(ctx, root); err == nil {
		t.Error("dirSize with cancelled context = nil error, want error")
	}
}

// TestFreeSpace verifies a real directory reports a positive capacity and that a
// not-yet-created path falls back to its existing ancestor.
func TestFreeSpace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	free, total, err := freeSpace(root)
	if err != nil {
		t.Fatalf("freeSpace(%s): %v", root, err)
	}
	if total <= 0 || free < 0 || free > total {
		t.Errorf("freeSpace(%s) = (free=%d, total=%d), want 0 <= free <= total and total > 0", root, free, total)
	}

	// A path under root that does not exist yet resolves to root's filesystem.
	nested := filepath.Join(root, "not", "created", "yet")
	if _, total2, err := freeSpace(nested); err != nil || total2 <= 0 {
		t.Errorf("freeSpace(nested missing) = (total=%d, err=%v), want a positive total and nil error", total2, err)
	}

	if free, total, err := freeSpace(""); err != nil || free != 0 || total != 0 {
		t.Errorf("freeSpace(empty) = (%d, %d, %v), want (0, 0, nil)", free, total, err)
	}
}

// TestStorageCacheMemoises verifies the usage measurement is cached for the TTL
// and recomputed once it elapses, using an injected clock.
func TestStorageCacheMemoises(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.bin"), 100)

	now := time.Unix(0, 0)
	clock := func() time.Time { return now }
	cache := newStorageCache(root, "", time.Minute, clock)

	first, err := cache.usage(t.Context())
	if err != nil {
		t.Fatalf("usage first: %v", err)
	}
	if first.OriginalsBytes != 100 {
		t.Fatalf("first originals = %d, want 100", first.OriginalsBytes)
	}

	// Grow the tree but stay within the TTL: the cached value is returned.
	writeFile(t, filepath.Join(root, "b.bin"), 50)
	now = now.Add(30 * time.Second)
	cached, err := cache.usage(t.Context())
	if err != nil {
		t.Fatalf("usage cached: %v", err)
	}
	if cached.OriginalsBytes != 100 {
		t.Errorf("cached originals = %d, want 100 (still memoised)", cached.OriginalsBytes)
	}

	// Past the TTL: the measurement is refreshed.
	now = now.Add(time.Minute)
	fresh, err := cache.usage(t.Context())
	if err != nil {
		t.Fatalf("usage fresh: %v", err)
	}
	if fresh.OriginalsBytes != 150 {
		t.Errorf("fresh originals = %d, want 150", fresh.OriginalsBytes)
	}
}

// writeFile writes size bytes to path, creating parent directories, failing the
// test on any error.
func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
