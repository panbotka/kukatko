package placesjob

import (
	"sync"
	"testing"
	"time"
)

// fakeClock is a manually advanced clock so window rollover is deterministic.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// newFakeClock starts a clock at a fixed instant.
func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)}
}

// Now returns the current fake time.
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the fake clock forward by d.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// newBudget builds a WindowBudget of limit credits per window on a fake clock.
func newBudget(limit int, window time.Duration) (*WindowBudget, *fakeClock) {
	clock := newFakeClock()
	return NewWindowBudget(BudgetConfig{Limit: limit, Window: window, Clock: clock.Now}), clock
}

// TestWindowBudget_grantsUpToLimit verifies the budget grants exactly Limit
// credits per window and then denies.
func TestWindowBudget_grantsUpToLimit(t *testing.T) {
	t.Parallel()

	budget, _ := newBudget(3, time.Hour)
	for i := range 3 {
		if _, ok := budget.Reserve(); !ok {
			t.Fatalf("Reserve #%d denied, want granted", i+1)
		}
	}
	if _, ok := budget.Reserve(); ok {
		t.Error("Reserve #4 granted, want denied (budget spent)")
	}
}

// TestWindowBudget_deniedWaitsForRefill verifies an exhausted budget reports the
// time until the window rolls over — not a short retry — so a deferred job
// sleeps until credits actually exist instead of waking into an empty budget.
func TestWindowBudget_deniedWaitsForRefill(t *testing.T) {
	t.Parallel()

	budget, clock := newBudget(1, 6*time.Hour)
	if _, ok := budget.Reserve(); !ok {
		t.Fatal("first Reserve denied")
	}
	clock.Advance(time.Hour)

	retryAfter, ok := budget.Reserve()
	if ok {
		t.Fatal("Reserve granted with the budget spent")
	}
	if want := 5 * time.Hour; retryAfter != want {
		t.Errorf("retryAfter = %s, want %s (until the window refills)", retryAfter, want)
	}
}

// TestWindowBudget_deniedNeverSpins verifies repeated denials keep pointing at
// the same refill instant and never fall below the minimum delay, so a job
// deferred by an empty budget cannot be re-claimed in a tight loop.
func TestWindowBudget_deniedNeverSpins(t *testing.T) {
	t.Parallel()

	budget, clock := newBudget(1, time.Hour)
	if _, ok := budget.Reserve(); !ok {
		t.Fatal("first Reserve denied")
	}
	deadline := clock.Now().Add(time.Hour)

	// Walk right up to the window's end: every denial must still point at the
	// window's end, and never at a delay short enough to busy-loop.
	for elapsed := time.Duration(0); elapsed < time.Hour; elapsed += 10 * time.Minute {
		retryAfter, ok := budget.Reserve()
		if ok {
			t.Fatalf("Reserve granted %s into an exhausted window", elapsed)
		}
		if retryAfter < MinBudgetRetryDelay {
			t.Errorf("retryAfter = %s at %s, want >= %s", retryAfter, elapsed, MinBudgetRetryDelay)
		}
		if want := max(deadline.Sub(clock.Now()), MinBudgetRetryDelay); retryAfter != want {
			t.Errorf("retryAfter = %s at %s, want %s", retryAfter, elapsed, want)
		}
		clock.Advance(10 * time.Minute)
	}
	// The window has now elapsed, so the budget refills.
	if _, ok := budget.Reserve(); !ok {
		t.Error("Reserve denied after the window elapsed, want granted")
	}
}

// TestWindowBudget_rollsOverWindow verifies credits come back once the window
// has elapsed, and that a quiet period does not accumulate more than one
// window's worth.
func TestWindowBudget_rollsOverWindow(t *testing.T) {
	t.Parallel()

	budget, clock := newBudget(2, time.Hour)
	for range 2 {
		if _, ok := budget.Reserve(); !ok {
			t.Fatal("Reserve denied inside the limit")
		}
	}
	clock.Advance(3 * time.Hour) // three windows of silence

	for i := range 2 {
		if _, ok := budget.Reserve(); !ok {
			t.Fatalf("Reserve #%d after rollover denied, want granted", i+1)
		}
	}
	if _, ok := budget.Reserve(); ok {
		t.Error("Reserve granted a third credit; a quiet period must not accumulate credits")
	}
}

// TestWindowBudget_refundReturnsCredit verifies a refunded credit is spendable
// again and that refunding more than was spent cannot go negative.
func TestWindowBudget_refundReturnsCredit(t *testing.T) {
	t.Parallel()

	budget, _ := newBudget(1, time.Hour)
	if _, ok := budget.Reserve(); !ok {
		t.Fatal("first Reserve denied")
	}
	budget.Refund()
	if _, ok := budget.Reserve(); !ok {
		t.Error("Reserve after Refund denied, want granted")
	}

	budget.Refund()
	budget.Refund()
	budget.Refund()
	if got := budget.Snapshot().Spent; got != 0 {
		t.Errorf("spent after over-refunding = %d, want 0", got)
	}
	if _, ok := budget.Reserve(); !ok {
		t.Error("Reserve after over-refunding denied, want granted")
	}
}

// TestWindowBudget_snapshot verifies the readout the dashboard and the metrics
// collector consume: limit, spend, remaining and the refill instant.
func TestWindowBudget_snapshot(t *testing.T) {
	t.Parallel()

	budget, clock := newBudget(5, time.Hour)
	start := clock.Now()
	if got := budget.Snapshot(); got.Spent != 0 || got.Remaining != 5 || !got.ResetsAt.IsZero() {
		t.Errorf("fresh snapshot = %+v, want spent 0, remaining 5, no reset instant", got)
	}

	for range 2 {
		budget.Reserve()
	}
	got := budget.Snapshot()
	if !got.Enabled || got.Limit != 5 || got.Spent != 2 || got.Remaining != 3 || got.Window != time.Hour {
		t.Errorf("snapshot = %+v, want enabled 5/2/3 over 1h", got)
	}
	if want := start.Add(time.Hour); !got.ResetsAt.Equal(want) {
		t.Errorf("ResetsAt = %s, want %s", got.ResetsAt, want)
	}

	// An elapsed window reads as empty even before anything reserves again — and
	// the readout must not shift the refill instant the deferred jobs wait for.
	clock.Advance(time.Hour)
	if got := budget.Snapshot(); got.Spent != 0 || got.Remaining != 5 {
		t.Errorf("snapshot after the window elapsed = %+v, want spent 0, remaining 5", got)
	}
	if _, ok := budget.Reserve(); !ok {
		t.Error("Reserve after the window elapsed denied, want granted")
	}
	if want := clock.Now().Add(time.Hour); !budget.Snapshot().ResetsAt.Equal(want) {
		t.Errorf("ResetsAt after rollover = %s, want %s", budget.Snapshot().ResetsAt, want)
	}
}

// TestWindowBudget_disabled verifies a non-positive limit means no budget at
// all: every reservation is granted and the snapshot says so.
func TestWindowBudget_disabled(t *testing.T) {
	t.Parallel()

	budget, _ := newBudget(0, time.Hour)
	for i := range 100 {
		if _, ok := budget.Reserve(); !ok {
			t.Fatalf("Reserve #%d denied on a disabled budget", i+1)
		}
	}
	budget.Refund()
	if got := budget.Snapshot(); got.Enabled || got.Limit != 0 || got.Remaining != 0 {
		t.Errorf("disabled snapshot = %+v, want the zero value", got)
	}
}

// TestWindowBudget_defaultWindow verifies an unset window falls back to the
// package default rather than to zero (which would refill continuously).
func TestWindowBudget_defaultWindow(t *testing.T) {
	t.Parallel()

	budget := NewWindowBudget(BudgetConfig{Limit: 1})
	if _, ok := budget.Reserve(); !ok {
		t.Fatal("first Reserve denied")
	}
	retryAfter, ok := budget.Reserve()
	if ok {
		t.Fatal("second Reserve granted, want denied")
	}
	if retryAfter > DefaultBudgetWindow || retryAfter < DefaultBudgetWindow-time.Minute {
		t.Errorf("retryAfter = %s, want about %s", retryAfter, DefaultBudgetWindow)
	}
	if got := budget.Snapshot().Window; got != DefaultBudgetWindow {
		t.Errorf("window = %s, want %s", got, DefaultBudgetWindow)
	}
}

// TestWindowBudget_concurrentReserveRespectsLimit verifies the limit holds under
// concurrent workers — the budget is shared by every worker goroutine.
func TestWindowBudget_concurrentReserveRespectsLimit(t *testing.T) {
	t.Parallel()

	const limit = 50
	budget, _ := newBudget(limit, time.Hour)

	var mu sync.Mutex
	granted := 0
	var wg sync.WaitGroup
	for range 200 {
		wg.Go(func() {
			if _, ok := budget.Reserve(); ok {
				mu.Lock()
				granted++
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	if granted != limit {
		t.Errorf("granted = %d, want exactly %d", granted, limit)
	}
}

// TestUnlimitedBudget verifies the default budget used when none is configured
// never denies and reports itself as not enforced.
func TestUnlimitedBudget(t *testing.T) {
	t.Parallel()

	var budget CreditBudget = unlimitedBudget{}
	retryAfter, ok := budget.Reserve()
	if !ok || retryAfter != 0 {
		t.Errorf("Reserve = (%s, %v), want (0s, true)", retryAfter, ok)
	}
	budget.Refund()
	if got := budget.Snapshot(); got.Enabled {
		t.Errorf("snapshot = %+v, want not enabled", got)
	}
}
