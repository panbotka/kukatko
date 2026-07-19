package reachability

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// fakeHealth is a HealthChecker whose answer can be toggled between probes and
// which counts how many times it was probed, so a test can assert both the
// cached result and that a disabled Checker never probes at all.
type fakeHealth struct {
	healthy atomic.Bool
	calls   atomic.Int32
}

// Healthy records the call and returns the currently configured answer.
func (f *fakeHealth) Healthy(context.Context) bool {
	f.calls.Add(1)
	return f.healthy.Load()
}

// TestChecker_TickReflectsHealth verifies the cached flag starts false, follows
// the probe result after each Tick, and flips back when the dependency goes
// offline.
func TestChecker_TickReflectsHealth(t *testing.T) {
	t.Parallel()

	health := &fakeHealth{}
	health.healthy.Store(true)
	checker := New(Config{Health: health, Enabled: true})

	if checker.Reachable() {
		t.Fatal("Reachable() = true before first probe, want false")
	}

	checker.Tick(context.Background())
	if !checker.Reachable() {
		t.Fatal("Reachable() = false after a healthy probe, want true")
	}

	health.healthy.Store(false)
	checker.Tick(context.Background())
	if checker.Reachable() {
		t.Fatal("Reachable() = true after an unhealthy probe, want false")
	}
}

// TestChecker_DisabledAlwaysUnreachable verifies a disabled Checker (the shape
// used when no embedding URL is configured) reports false and never probes, even
// with a nil Health backend.
func TestChecker_DisabledAlwaysUnreachable(t *testing.T) {
	t.Parallel()

	// Health is intentionally nil: a disabled Checker must never dereference it.
	checker := New(Config{Enabled: false})

	if checker.Reachable() {
		t.Fatal("disabled Reachable() = true, want false")
	}
	checker.Tick(context.Background())
	if checker.Reachable() {
		t.Fatal("disabled Reachable() = true after Tick, want false")
	}
}

// TestChecker_DisabledDoesNotProbe verifies Tick on a disabled Checker never
// calls the health backend.
func TestChecker_DisabledDoesNotProbe(t *testing.T) {
	t.Parallel()

	health := &fakeHealth{}
	health.healthy.Store(true)
	checker := New(Config{Health: health, Enabled: false})

	checker.Tick(context.Background())
	if got := health.calls.Load(); got != 0 {
		t.Fatalf("disabled Checker probed %d time(s), want 0", got)
	}
}

// TestChecker_RunProbesThenStops verifies Run performs its immediate probe
// (flipping the flag) and then returns promptly when the context is cancelled.
func TestChecker_RunProbesThenStops(t *testing.T) {
	t.Parallel()

	health := &fakeHealth{}
	health.healthy.Store(true)
	checker := New(Config{Health: health, Enabled: true})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	// A long interval means only the immediate probe runs before we cancel.
	go func() {
		checker.Run(ctx, time.Hour)
		close(done)
	}()

	waitUntil(t, func() bool { return checker.Reachable() }, "Run did not perform its initial probe")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

// TestChecker_RunDisabledReturnsImmediately verifies Run on a disabled Checker
// returns without blocking, even given an un-cancelled context.
func TestChecker_RunDisabledReturnsImmediately(t *testing.T) {
	t.Parallel()

	checker := New(Config{Enabled: false})

	done := make(chan struct{})
	go func() {
		checker.Run(context.Background(), time.Hour)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("disabled Run did not return immediately")
	}
}

// waitUntil polls cond up to a fixed deadline, failing with msg if it never
// becomes true. It exists because Run's initial probe happens on another
// goroutine, so the flag flips slightly after Run is started.
func waitUntil(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal(msg)
}
