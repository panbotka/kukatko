package system

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// StorageUsage reports the on-disk footprint of the originals and cache trees
// plus the free and total capacity of the filesystem backing the originals. The
// byte counts are best-effort: a missing directory contributes zero rather than
// an error, so the dashboard renders before any photos are imported.
type StorageUsage struct {
	// OriginalsBytes is the total size of the regular files under the originals
	// root.
	OriginalsBytes int64 `json:"originals_bytes"`
	// CacheBytes is the total size of the regular files under the cache root
	// (thumbnails and other derived data).
	CacheBytes int64 `json:"cache_bytes"`
	// FreeBytes is the space available to an unprivileged user on the filesystem
	// holding the originals.
	FreeBytes int64 `json:"free_bytes"`
	// TotalBytes is the total capacity of that filesystem.
	TotalBytes int64 `json:"total_bytes"`
}

// storageCache computes the storage usage on demand and memoises it for a short
// TTL, so the polled status endpoint does not walk a large originals tree on
// every request. It is safe for concurrent use.
type storageCache struct {
	originals string
	cachePath string
	ttl       time.Duration
	now       func() time.Time

	mu         sync.Mutex
	cached     StorageUsage
	computedAt time.Time
	valid      bool
}

// newStorageCache returns a storageCache for the given originals and cache
// directories. A non-positive ttl defaults to defaultStorageTTL and a nil now
// defaults to time.Now, so callers may leave them unset.
func newStorageCache(originals, cachePath string, ttl time.Duration, now func() time.Time) *storageCache {
	if ttl <= 0 {
		ttl = defaultStorageTTL
	}
	if now == nil {
		now = time.Now
	}
	return &storageCache{originals: originals, cachePath: cachePath, ttl: ttl, now: now}
}

// usage returns the storage usage, recomputing it when the memoised value is
// older than the TTL (or has never been computed). A recompute error is
// returned alongside the freshly gathered partial value; the caller may treat
// the byte counts as best-effort.
func (c *storageCache) usage(ctx context.Context) (StorageUsage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.valid && c.now().Sub(c.computedAt) < c.ttl {
		return c.cached, nil
	}
	usage, err := c.compute(ctx)
	c.cached = usage
	c.computedAt = c.now()
	c.valid = true
	return usage, err
}

// compute gathers the originals and cache sizes and the filesystem capacity. It
// returns the first error encountered while still filling in whatever it could,
// so a single failing measurement does not blank the whole readout.
func (c *storageCache) compute(ctx context.Context) (StorageUsage, error) {
	var usage StorageUsage
	var firstErr error

	originals, err := dirSize(ctx, c.originals)
	if err != nil {
		firstErr = err
	}
	usage.OriginalsBytes = originals

	cacheBytes, err := dirSize(ctx, c.cachePath)
	if err != nil && firstErr == nil {
		firstErr = err
	}
	usage.CacheBytes = cacheBytes

	free, total, err := freeSpace(c.originals)
	if err != nil && firstErr == nil {
		firstErr = err
	}
	usage.FreeBytes = free
	usage.TotalBytes = total

	return usage, firstErr
}

// dirSize sums the sizes of the regular files under root, descending into
// subdirectories. A non-existent root yields zero with no error (the directory
// may not have been created yet); the walk is aborted if ctx is cancelled.
func dirSize(ctx context.Context, root string) (int64, error) {
	if root == "" {
		return 0, nil
	}
	var total int64
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("dir size walk interrupted: %w", ctxErr)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return fmt.Errorf("stat %s: %w", path, infoErr)
		}
		total += info.Size()
		return nil
	})
	if walkErr != nil {
		if os.IsNotExist(walkErr) {
			return 0, nil
		}
		return total, fmt.Errorf("measuring %s: %w", root, walkErr)
	}
	return total, nil
}

// freeSpace returns the available and total bytes of the filesystem backing
// path. Because the directory may not exist yet, it measures the nearest
// existing ancestor instead, which is on the same filesystem. It returns zeroes
// with no error when no ancestor exists (only possible for an empty path).
func freeSpace(path string) (free, total int64, err error) {
	dir := firstExisting(path)
	if dir == "" {
		return 0, 0, nil
	}
	var st unix.Statfs_t
	if statErr := unix.Statfs(dir, &st); statErr != nil {
		return 0, 0, fmt.Errorf("statfs %s: %w", dir, statErr)
	}
	blockSize := st.Bsize
	// Real disk byte counts fit comfortably within int64 (a signed 64-bit byte
	// count tops out at 8 EiB), so the uint64->int64 conversions cannot overflow
	// in practice.
	free = int64(st.Bavail) * blockSize  //nolint:gosec // see comment above
	total = int64(st.Blocks) * blockSize //nolint:gosec // see comment above
	return free, total, nil
}

// firstExisting walks path up its parents and returns the first one that exists,
// or "" when path is empty. The filesystem root always exists, so a non-empty
// absolute path always resolves to a real directory.
func firstExisting(path string) string {
	for path != "" {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return ""
		}
		path = parent
	}
	return ""
}
