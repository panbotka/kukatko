package auth

import (
	"strconv"
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

// TestLimiter_boundsKeysUnderDistinctKeyFlood verifies the key set stays capped
// at maxKeys when far more distinct keys arrive than the cap allows, without any
// Cleanup tick in between — the RAM-exhaustion guard for the public login
// endpoint.
func TestLimiter_boundsKeysUnderDistinctKeyFlood(t *testing.T) {
	t.Parallel()

	l := NewLimiter(3, time.Hour)
	base := time.Unix(1_700_000_000, 0)

	// Every attempt lands within the window, so none of the keys expires: the
	// cap can only hold if insertion evicts.
	for i := range maxKeys * 3 {
		l.Allow(strconv.Itoa(i), base.Add(time.Duration(i)*time.Millisecond))
	}

	l.mu.Lock()
	size := len(l.attempts)
	l.mu.Unlock()
	if size > maxKeys {
		t.Errorf("limiter holds %d keys, want at most %d", size, maxKeys)
	}
}

// TestLimiter_evictsLeastRecentlyActive verifies a key that keeps being used
// survives an eviction sweep while single-touch keys are dropped. The tracked
// key is blocked for most of the run, so this also pins down that a flood of
// fresh keys cannot evict — and thereby clear — an active block.
func TestLimiter_evictsLeastRecentlyActive(t *testing.T) {
	t.Parallel()

	l := NewLimiter(3, time.Hour)
	base := time.Unix(1_700_000_000, 0)

	// Fill far past the cap, touching the tracked key after every insertion.
	for i := range maxKeys * 2 {
		at := base.Add(time.Duration(i) * time.Millisecond)
		l.Allow(strconv.Itoa(i), at)
		l.Allow("active", at.Add(time.Microsecond))
	}

	l.mu.Lock()
	_, present := l.attempts["active"]
	size := len(l.attempts)
	l.mu.Unlock()
	if !present {
		t.Error("the continuously refreshed key should survive eviction")
	}
	if size > maxKeys {
		t.Errorf("limiter holds %d keys, want at most %d", size, maxKeys)
	}
	// Its block must have survived the flood too, not just its map entry.
	if l.Allow("active", base.Add(time.Duration(maxKeys*2)*time.Millisecond)) {
		t.Error("the blocked key should still be blocked after the flood")
	}
}

// TestLimiter_expiredKeysFreeRoomBeforeEviction verifies that when the map is
// full of aged-out keys a new insertion reclaims them rather than evicting live
// entries, so ordinary traffic never loses its throttling state.
func TestLimiter_expiredKeysFreeRoomBeforeEviction(t *testing.T) {
	t.Parallel()

	l := NewLimiter(1, time.Minute)
	base := time.Unix(1_700_000_000, 0)

	for i := range maxKeys {
		l.Allow(strconv.Itoa(i), base)
	}
	// An hour later every recorded attempt has aged out of the one-minute window.
	l.Allow("fresh", base.Add(time.Hour))

	l.mu.Lock()
	size := len(l.attempts)
	l.mu.Unlock()
	if size != 1 {
		t.Errorf("limiter holds %d keys, want only the fresh one", size)
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
