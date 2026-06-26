package auth

import (
	"testing"
	"time"
)

// TestLimiter_blocksAfterMax verifies the limiter permits exactly max attempts
// in a window and blocks the next one.
func TestLimiter_blocksAfterMax(t *testing.T) {
	t.Parallel()

	l := NewLimiter(3, time.Minute)
	base := time.Unix(1_700_000_000, 0)

	for i := range 3 {
		if !l.Allow("user|ip", base.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("attempt %d should be allowed", i+1)
		}
	}
	if l.Allow("user|ip", base.Add(3*time.Second)) {
		t.Error("4th attempt within window should be blocked")
	}
}

// TestLimiter_windowExpiry verifies attempts older than the window stop counting.
func TestLimiter_windowExpiry(t *testing.T) {
	t.Parallel()

	l := NewLimiter(2, time.Minute)
	base := time.Unix(1_700_000_000, 0)

	if !l.Allow("k", base) {
		t.Fatal("first attempt should be allowed")
	}
	if !l.Allow("k", base.Add(10*time.Second)) {
		t.Fatal("second attempt should be allowed")
	}
	if l.Allow("k", base.Add(20*time.Second)) {
		t.Fatal("third attempt within window should be blocked")
	}
	// Past the window relative to the earlier attempts, room frees up again.
	if !l.Allow("k", base.Add(2*time.Minute)) {
		t.Error("attempt after window should be allowed again")
	}
}

// TestLimiter_resetClearsKey verifies Reset frees a blocked key.
func TestLimiter_resetClearsKey(t *testing.T) {
	t.Parallel()

	l := NewLimiter(1, time.Minute)
	base := time.Unix(1_700_000_000, 0)

	if !l.Allow("k", base) {
		t.Fatal("first attempt should be allowed")
	}
	if l.Allow("k", base.Add(time.Second)) {
		t.Fatal("second attempt should be blocked before reset")
	}
	l.Reset("k")
	if !l.Allow("k", base.Add(2*time.Second)) {
		t.Error("attempt after reset should be allowed")
	}
}

// TestLimiter_keysAreIndependent verifies one key's attempts do not affect another.
func TestLimiter_keysAreIndependent(t *testing.T) {
	t.Parallel()

	l := NewLimiter(1, time.Minute)
	base := time.Unix(1_700_000_000, 0)

	if !l.Allow("a", base) {
		t.Fatal("key a first attempt should be allowed")
	}
	if !l.Allow("b", base) {
		t.Error("key b first attempt should be allowed independently of a")
	}
}

// TestLimiter_cleanupRemovesStaleKeys verifies Cleanup drops fully-aged keys.
func TestLimiter_cleanupRemovesStaleKeys(t *testing.T) {
	t.Parallel()

	l := NewLimiter(2, time.Minute)
	base := time.Unix(1_700_000_000, 0)
	l.Allow("k", base)

	l.Cleanup(base.Add(2 * time.Minute))

	l.mu.Lock()
	_, present := l.attempts["k"]
	l.mu.Unlock()
	if present {
		t.Error("Cleanup should have removed the stale key")
	}
}

// TestNewLimiter_clampsArguments verifies non-positive arguments are clamped to
// a usable limiter rather than producing a divide/never-allow configuration.
func TestNewLimiter_clampsArguments(t *testing.T) {
	t.Parallel()

	l := NewLimiter(0, -time.Second)
	if l.max != 1 {
		t.Errorf("max = %d, want clamped to 1", l.max)
	}
	if l.window <= 0 {
		t.Errorf("window = %s, want positive", l.window)
	}
}
