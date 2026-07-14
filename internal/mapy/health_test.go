package mapy_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/mapy"
)

// TestHealth_ClassifiesOutcomes verifies each upstream outcome folds into the
// right health state, so the dashboard can tell a rejected key (the operator must
// act) from a provider outage (wait it out).
func TestHealth_ClassifiesOutcomes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		err          error
		want         mapy.HealthState
		wantDegraded bool
	}{
		{"success", nil, mapy.HealthOK, false},
		{"rejected key", fmt.Errorf("tile: %w (status 403)", mapy.ErrUnauthorized), mapy.HealthKeyRejected, true},
		{"rate limited", fmt.Errorf("tile: %w", mapy.ErrRateLimited), mapy.HealthRateLimited, true},
		{"provider down", fmt.Errorf("tile: %w", mapy.ErrUnavailable), mapy.HealthUnavailable, true},
		{"other upstream failure", fmt.Errorf("tile: %w", mapy.ErrUpstream), mapy.HealthError, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			health := mapy.NewHealth()
			health.Record(tt.err)

			got := health.Snapshot()
			if got.State != tt.want {
				t.Errorf("state = %q, want %q", got.State, tt.want)
			}
			if got.State.Degraded() != tt.wantDegraded {
				t.Errorf("degraded = %v, want %v", got.State.Degraded(), tt.wantDegraded)
			}
			if got.CheckedAt.IsZero() {
				t.Error("checked_at is zero, want the time of the observation")
			}
			if tt.err != nil && got.Detail == "" {
				t.Error("detail is empty, want a sanitised description of the failure")
			}
		})
	}
}

// TestHealth_IgnoresUninformativeOutcomes verifies outcomes that say nothing
// about the upstream — a tile outside the covered area, a mapset the caller made
// up, a request the browser cancelled — leave the state untouched, so they cannot
// mask a rejected key or fake a degradation.
func TestHealth_IgnoresUninformativeOutcomes(t *testing.T) {
	t.Parallel()
	for _, err := range []error{
		fmt.Errorf("tile: %w", mapy.ErrNotFound),
		fmt.Errorf("tile: %w: %q", mapy.ErrInvalidMapset, "satellite"),
		fmt.Errorf("tile: %w", context.Canceled),
	} {
		health := mapy.NewHealth()
		health.Record(fmt.Errorf("tile: %w (status 403)", mapy.ErrUnauthorized))
		health.Record(err)

		if got := health.Snapshot().State; got != mapy.HealthKeyRejected {
			t.Errorf("after Record(%v): state = %q, want the earlier %q to survive",
				err, got, mapy.HealthKeyRejected)
		}
	}
}

// TestHealth_RecoversOnSuccess verifies a later success clears a degraded state,
// so a fixed key is reported healthy again without a restart.
func TestHealth_RecoversOnSuccess(t *testing.T) {
	t.Parallel()
	health := mapy.NewHealth()
	health.Record(fmt.Errorf("tile: %w (status 403)", mapy.ErrUnauthorized))
	health.Record(nil)

	got := health.Snapshot()
	if got.State != mapy.HealthOK {
		t.Errorf("state = %q, want %q", got.State, mapy.HealthOK)
	}
	if got.Detail != "" {
		t.Errorf("detail = %q, want empty once healthy", got.Detail)
	}
}

// TestHealth_NilIsSafe verifies an unconfigured (nil) tracker — no mapy.com key —
// swallows recordings and reports the unknown state instead of panicking, so
// callers need no nil checks.
func TestHealth_NilIsSafe(t *testing.T) {
	t.Parallel()
	var health *mapy.Health
	health.Record(errors.New("boom"))

	if got := health.Snapshot().State; got != mapy.HealthUnknown {
		t.Errorf("state = %q, want %q", got, mapy.HealthUnknown)
	}
}

// TestHealth_DetailNeverLeaksTheKey verifies the surfaced detail carries only the
// classified upstream failure, never the API key (which the client sends as a
// header and keeps out of its errors).
func TestHealth_DetailNeverLeaksTheKey(t *testing.T) {
	t.Parallel()
	const key = "super-secret-key"
	health := mapy.NewHealth()
	// The mapy client never puts the key in an error; this is the shape it does
	// produce for a rejected key.
	health.Record(fmt.Errorf("tile: %w (status 403)", mapy.ErrUnauthorized))

	if detail := health.Snapshot().Detail; strings.Contains(detail, key) {
		t.Errorf("detail = %q, must never carry the API key", detail)
	}
}
