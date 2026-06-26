package auth

import (
	"testing"
	"time"
)

// TestSlideExpiry verifies the sliding-expiry calculation: extension within the
// cap, capping at max lifetime, and write coalescing below the renew interval.
func TestSlideExpiry(t *testing.T) {
	t.Parallel()

	created := time.Unix(1_700_000_000, 0)
	const ttl = 24 * time.Hour
	const maxLifetime = 72 * time.Hour

	tests := []struct {
		name       string
		current    time.Time
		now        time.Time
		wantExtend bool
		wantExpiry time.Time
	}{
		{
			name:       "extends by full ttl from now",
			current:    created.Add(ttl),
			now:        created.Add(2 * time.Hour),
			wantExtend: true,
			wantExpiry: created.Add(2*time.Hour + ttl),
		},
		{
			name:       "no write when gain below renew interval",
			current:    created.Add(ttl),
			now:        created.Add(30 * time.Second),
			wantExtend: false,
			wantExpiry: created.Add(ttl),
		},
		{
			name:       "capped at max lifetime",
			current:    created.Add(ttl),
			now:        created.Add(60 * time.Hour),
			wantExtend: true,
			wantExpiry: created.Add(maxLifetime),
		},
		{
			name:       "at cap no further extension",
			current:    created.Add(maxLifetime),
			now:        created.Add(71 * time.Hour),
			wantExtend: false,
			wantExpiry: created.Add(maxLifetime),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotExpiry, gotExtend := slideExpiry(created, tt.current, tt.now, ttl, maxLifetime)
			if gotExtend != tt.wantExtend {
				t.Errorf("extend = %v, want %v", gotExtend, tt.wantExtend)
			}
			if !gotExpiry.Equal(tt.wantExpiry) {
				t.Errorf("expiry = %s, want %s", gotExpiry, tt.wantExpiry)
			}
		})
	}
}
