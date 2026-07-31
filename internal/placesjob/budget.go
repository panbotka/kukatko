package placesjob

import (
	"sync"
	"time"
)

const (
	// DefaultBudgetWindow is the period one credit budget covers when the
	// configuration leaves the window unset.
	DefaultBudgetWindow = 24 * time.Hour
	// MinBudgetRetryDelay is the floor on how long a job waits when the credit
	// budget is exhausted. The deferral is normally "until the window refills",
	// which is far longer; the floor only guarantees that a clock oddity (or a
	// window that ends in the same instant) can never turn the deferral into a
	// tight claim/defer loop.
	MinBudgetRetryDelay = time.Minute
)

// CreditBudget caps how many reverse geocodes the `places` job may actually
// spend in a period. It is a bound on *how many* credits are spent, orthogonal
// to RateLimiter's bound on *how fast* — a full-library import would otherwise
// drain a whole mapy.com quota at the limiter's pace with nothing to stop it.
//
// It is an interface so the Service unit-tests with a deterministic fake and so
// the default (no budget at all) needs no clock.
type CreditBudget interface {
	// Reserve claims one geocode credit. It reports ok=true when the budget had
	// one to spare, and otherwise ok=false plus how long the caller should wait
	// before trying again (until the budget refills).
	Reserve() (retryAfter time.Duration, ok bool)
	// Refund hands a reserved credit back, for a call that never reached
	// mapy.com and so cost nothing.
	Refund()
	// Snapshot reports the budget's current state for the status dashboard and
	// the metrics collector.
	Snapshot() BudgetSnapshot
}

// BudgetSnapshot is a point-in-time readout of a credit budget: what it allows,
// what has been spent against it in the current window, and when that window
// ends. A zero value means no budget is enforced.
type BudgetSnapshot struct {
	// Enabled is false when no budget caps the spend (every Reserve succeeds).
	Enabled bool
	// Limit is how many geocodes one window allows.
	Limit int
	// Spent is how many have been spent in the current window.
	Spent int
	// Remaining is Limit-Spent, never negative.
	Remaining int
	// Window is the length of one budget period.
	Window time.Duration
	// ResetsAt is when the current window ends and the budget refills. It is the
	// zero time while no budget is enforced or nothing has been spent yet.
	ResetsAt time.Time
}

// unlimitedBudget is the budget used when none is configured: every geocode is
// allowed, matching the behaviour before budgets existed.
type unlimitedBudget struct{}

// Reserve always grants a credit.
func (unlimitedBudget) Reserve() (time.Duration, bool) { return 0, true }

// Refund is a no-op: nothing is counted.
func (unlimitedBudget) Refund() {}

// Snapshot reports that no budget is enforced.
func (unlimitedBudget) Snapshot() BudgetSnapshot { return BudgetSnapshot{} }

// CreditMeter counts the mapy.com geocode credits the job actually spends, so a
// run's credit spend can be watched while it happens instead of being inferred
// afterwards. *metrics.Registry satisfies it; the default is a no-op.
type CreditMeter interface {
	// GeocodeCreditSpent records that one reverse-geocode credit was spent.
	GeocodeCreditSpent()
}

// noopMeter is the meter used when none is configured.
type noopMeter struct{}

// GeocodeCreditSpent discards the observation.
func (noopMeter) GeocodeCreditSpent() {}

// BudgetConfig configures a WindowBudget.
type BudgetConfig struct {
	// Limit is the maximum number of geocodes one window allows. <= 0 disables
	// the budget (every Reserve succeeds).
	Limit int
	// Window is the length of one budget period; <= 0 uses DefaultBudgetWindow.
	Window time.Duration
	// Clock supplies the current time; nil uses time.Now. Tests inject a fake so
	// window rollover is deterministic.
	Clock func() time.Time
}

// WindowBudget is a concurrency-safe fixed-window credit counter: it grants at
// most Limit geocodes per Window, then denies every further request until the
// window rolls over. The window starts with the first credit spent in it and is
// rolled lazily, so a quiet period does not accumulate credits.
//
// The count lives in memory, like the rate limiter's token bucket: restarting
// the process starts a fresh window. That is deliberate — the budget guards
// against a runaway import draining the quota unattended, not against an
// operator who restarts the server to lift it.
type WindowBudget struct {
	mu          sync.Mutex
	limit       int
	window      time.Duration
	now         func() time.Time
	spent       int
	windowStart time.Time
}

// NewWindowBudget returns a budget of cfg.Limit geocodes per cfg.Window. A
// non-positive limit disables the budget so every Reserve succeeds.
func NewWindowBudget(cfg BudgetConfig) *WindowBudget {
	window := cfg.Window
	if window <= 0 {
		window = DefaultBudgetWindow
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	return &WindowBudget{limit: cfg.Limit, window: window, now: clock}
}

// Reserve claims one credit from the current window, rolling the window over
// first when it has elapsed. When the window's credits are spent it returns how
// long until it refills (at least MinBudgetRetryDelay), so the caller can defer
// its work until the budget actually exists rather than retrying into an empty
// budget for the rest of the window.
func (b *WindowBudget) Reserve() (time.Duration, bool) {
	if b.limit <= 0 {
		return 0, true
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	b.spent, b.windowStart = b.stateLocked(now)
	if b.spent >= b.limit {
		return max(b.windowStart.Add(b.window).Sub(now), MinBudgetRetryDelay), false
	}
	b.spent++
	return 0, true
}

// Refund returns one unspent credit to the current window. It never pushes the
// count below zero, so a refund that arrives after the window rolled over is
// harmless.
func (b *WindowBudget) Refund() {
	if b.limit <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	b.spent, b.windowStart = b.stateLocked(b.now())
	if b.spent > 0 {
		b.spent--
	}
}

// Snapshot reports the budget state effective now. It does not roll the window
// over — a scrape must not shift the reset instant the deferred jobs are waiting
// for — it only reports an elapsed window as already empty.
func (b *WindowBudget) Snapshot() BudgetSnapshot {
	if b.limit <= 0 {
		return BudgetSnapshot{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	spent, start := b.stateLocked(b.now())
	snapshot := BudgetSnapshot{
		Enabled:   true,
		Limit:     b.limit,
		Spent:     spent,
		Remaining: max(b.limit-spent, 0),
		Window:    b.window,
	}
	if spent > 0 {
		snapshot.ResetsAt = start.Add(b.window)
	}
	return snapshot
}

// stateLocked returns the spend and window start effective at now: the stored
// pair while the window is still running, and a fresh window when it has
// elapsed (or none has started yet). The caller holds b.mu.
func (b *WindowBudget) stateLocked(now time.Time) (spent int, start time.Time) {
	if b.windowStart.IsZero() || !now.Before(b.windowStart.Add(b.window)) {
		return 0, now
	}
	return b.spent, b.windowStart
}
