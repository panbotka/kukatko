package system

import (
	"context"
	"sync"
	"time"
)

// snapshotCache memoises one aggregation for a TTL so a page that is opened
// often — or polled — cannot turn into a query storm. Only successful
// computations are cached: a failure must reach the caller, which reports an
// error rather than letting the page render zeroes as if they were real numbers,
// and the next read retries. A stale value is never served past its TTL. It is
// safe for concurrent use.
//
// It is the shared shape behind the library counts and the chart aggregates
// (each held per project by projectSnapshotCache) and the dashboard,
// which differ only in what they compute and how long the answer stays good
// enough (see defaultLibraryTTL and defaultChartsTTL). The storage-usage cache is
// deliberately not built on it: that one memoises failures too, because a partial
// measurement is still worth showing.
type snapshotCache[T any] struct {
	compute func(context.Context) (T, error)
	ttl     time.Duration
	now     func() time.Time

	mu         sync.Mutex
	cached     T
	computedAt time.Time
	valid      bool
}

// newSnapshotCache returns a cache over compute. A non-positive ttl falls back to
// fallbackTTL and a nil now to time.Now, so callers may leave both unset and
// tests may drive a fake clock.
func newSnapshotCache[T any](
	compute func(context.Context) (T, error), ttl, fallbackTTL time.Duration, now func() time.Time,
) *snapshotCache[T] {
	if ttl <= 0 {
		ttl = fallbackTTL
	}
	if now == nil {
		now = time.Now
	}
	return &snapshotCache[T]{compute: compute, ttl: ttl, now: now}
}

// get returns the memoised value, recomputing it when the cached one is older
// than the TTL (or has never been computed). It returns the zero value alongside
// any error and leaves the cache untouched, so the next read tries again.
func (c *snapshotCache[T]) get(ctx context.Context) (T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.valid && c.now().Sub(c.computedAt) < c.ttl {
		return c.cached, nil
	}
	fresh, err := c.compute(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	c.cached = fresh
	c.computedAt = c.now()
	c.valid = true
	return c.cached, nil
}

// projectSnapshotCache memoises one aggregation per project: a snapshotCache
// for each project uid it has been asked about, so one project's snapshot is
// never served to another and each project's TTL runs on its own. The entries
// are created on first use and kept for the life of the process; their number
// is bounded by the projects of the instance. An empty project uid is
// ErrNoProject and is never computed, so no caller can reach an unscoped
// aggregation through it. It is safe for concurrent use.
type projectSnapshotCache[T any] struct {
	compute     func(ctx context.Context, projectUID string) (T, error)
	ttl         time.Duration
	fallbackTTL time.Duration
	now         func() time.Time

	mu        sync.Mutex
	byProject map[string]*snapshotCache[T]
}

// newProjectSnapshotCache returns a per-project cache over compute, with ttl,
// fallbackTTL and now applied to every project's entry as newSnapshotCache
// applies them.
func newProjectSnapshotCache[T any](
	compute func(context.Context, string) (T, error), ttl, fallbackTTL time.Duration, now func() time.Time,
) *projectSnapshotCache[T] {
	return &projectSnapshotCache[T]{
		compute:     compute,
		ttl:         ttl,
		fallbackTTL: fallbackTTL,
		now:         now,
		byProject:   map[string]*snapshotCache[T]{},
	}
}

// get returns the memoised value of the project projectUID, recomputing it on
// that project's own TTL (see snapshotCache.get). An empty projectUID returns
// ErrNoProject.
func (c *projectSnapshotCache[T]) get(ctx context.Context, projectUID string) (T, error) {
	if projectUID == "" {
		var zero T
		return zero, ErrNoProject
	}
	c.mu.Lock()
	entry, ok := c.byProject[projectUID]
	if !ok {
		entry = newSnapshotCache(func(ctx context.Context) (T, error) {
			return c.compute(ctx, projectUID)
		}, c.ttl, c.fallbackTTL, c.now)
		c.byProject[projectUID] = entry
	}
	c.mu.Unlock()
	return entry.get(ctx)
}
