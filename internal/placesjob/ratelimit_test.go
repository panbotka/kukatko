package placesjob

import (
	"testing"
)

// TestTokenBucket_burstThenThrottles verifies the bucket allows up to the burst
// immediately and then denies once the tokens are spent.
func TestTokenBucket_burstThenThrottles(t *testing.T) {
	t.Parallel()

	// A very low rate so no meaningful refill happens during the test.
	tb := NewTokenBucket(0.0001, 3)
	for i := range 3 {
		if !tb.Allow() {
			t.Fatalf("Allow() #%d = false, want true (within burst)", i+1)
		}
	}
	if tb.Allow() {
		t.Error("Allow() after burst = true, want false")
	}
}

// TestTokenBucket_disabledAlwaysAllows verifies a non-positive rate disables the
// limiter so every call is allowed.
func TestTokenBucket_disabledAlwaysAllows(t *testing.T) {
	t.Parallel()

	tb := NewTokenBucket(0, 0)
	for i := range 100 {
		if !tb.Allow() {
			t.Fatalf("Allow() #%d = false on disabled limiter, want true", i+1)
		}
	}
}

// TestAllowAll verifies the default limiter always allows.
func TestAllowAll(t *testing.T) {
	t.Parallel()

	if !(allowAll{}).Allow() {
		t.Error("allowAll.Allow() = false, want true")
	}
}
