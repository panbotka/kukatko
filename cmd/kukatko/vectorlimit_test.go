package main

import (
	"testing"

	"github.com/panbotka/kukatko/internal/config"
)

// TestGeocodeProjectRate verifies the ratelimit.geocode_project rule maps onto
// the maps API's per-project share: a positive rate passes through, while the
// rule's "non-positive disables" becomes a negative rate — the maps API reads a
// zero as "use the default", which would silently re-enable the share.
func TestGeocodeProjectRate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		rule      config.RateLimitRule
		wantRate  float64
		wantBurst int
	}{
		{"configured", config.RateLimitRule{RatePerSec: 3, Burst: 10}, 3, 10},
		{"zero disables", config.RateLimitRule{RatePerSec: 0, Burst: 10}, -1, 10},
		{"negative disables", config.RateLimitRule{RatePerSec: -2, Burst: 4}, -1, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rate, burst := geocodeProjectRate(tt.rule)
			if rate != tt.wantRate || burst != tt.wantBurst {
				t.Errorf("geocodeProjectRate(%+v) = %v, %d; want %v, %d",
					tt.rule, rate, burst, tt.wantRate, tt.wantBurst)
			}
		})
	}
}
