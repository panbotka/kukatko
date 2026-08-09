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
// It is the shared shape behind the library counts and the chart aggregates,
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
