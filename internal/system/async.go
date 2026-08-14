package system

import (
	"context"
	"sync"
	"time"
)

// asyncCache memoises one aggregation that is too expensive to ever run on a
// request path. Unlike snapshotCache it never computes inline: a read returns
// whatever was last computed (with "nothing yet" as a distinct answer) and, when
// that is older than the TTL, starts a refresh in the background for the *next*
// reader. The polled status endpoint therefore always answers immediately, at
// the price of a number that lags by up to one TTL — which is the right trade
// for a backlog figure nobody acts on within seconds.
//
// A failed refresh keeps the last good value and is retried no sooner than the
// next TTL, so an aggregation that is failing (or has become slow enough to time
// out) cannot be re-run on every poll. At most one refresh runs at a time.
//
// It is safe for concurrent use.
type asyncCache[T any] struct {
	compute func(context.Context) (T, error)
	ttl     time.Duration
	timeout time.Duration
	now     func() time.Time
	// spawn starts a refresh. It is a field so a test can run the refresh
	// inline and stay deterministic; production leaves it as `go f()`.
	spawn func(func())

	mu sync.Mutex
	// value and computedAt are the last successful computation.
	value      T
	computedAt time.Time
	valid      bool
	// attemptedAt is when a refresh last finished, successfully or not; it is
	// what the TTL is measured against, so a failure backs off like a success.
	attemptedAt time.Time
	attempted   bool
	running     bool
}

// newAsyncCache returns a background-refreshed cache over compute. A non-positive
// ttl falls back to fallbackTTL, a non-positive timeout to fallbackTimeout and a
// nil now to time.Now, so callers may leave them unset.
func newAsyncCache[T any](
	compute func(context.Context) (T, error),
	ttl, fallbackTTL, timeout, fallbackTimeout time.Duration,
	now func() time.Time,
) *asyncCache[T] {
	if ttl <= 0 {
		ttl = fallbackTTL
	}
	if timeout <= 0 {
		timeout = fallbackTimeout
	}
	if now == nil {
		now = time.Now
	}
	return &asyncCache[T]{
		compute: compute,
		ttl:     ttl,
		timeout: timeout,
		now:     now,
		spawn:   func(f func()) { go f() },
	}
}

// get returns the last successfully computed value and when it was computed,
// with ok=false when none has been computed yet. It never blocks on the
// computation; when the value is missing or stale it schedules a refresh whose
// result the next reader will see.
func (c *asyncCache[T]) get() (value T, computedAt time.Time, ok bool) {
	c.mu.Lock()
	value, computedAt, ok = c.value, c.computedAt, c.valid
	stale := !c.attempted || c.now().Sub(c.attemptedAt) >= c.ttl
	start := stale && !c.running
	if start {
		c.running = true
	}
	c.mu.Unlock()

	if start {
		c.spawn(c.refresh)
	}
	return value, computedAt, ok
}

// refresh recomputes the value under its own timeout, detached from any request:
// the reader that scheduled it is long gone, and a cancelled request must not
// abort the aggregation everyone else is waiting for. A failure leaves the last
// good value in place.
func (c *asyncCache[T]) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	fresh, err := c.compute(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.running = false
	c.attemptedAt = c.now()
	c.attempted = true
	if err != nil {
		return
	}
	c.value = fresh
	c.computedAt = c.attemptedAt
	c.valid = true
}
