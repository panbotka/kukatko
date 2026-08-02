//go:build integration

package auth

import (
	"os"
	"strconv"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestBcryptCost_productionUnchanged asserts the production work factor from
// inside the integration build, where hashes are minted cheaply. Lowering the
// suite's cost must never drift into lowering what a server mints, and this is
// the assertion that notices if someone "fixes" the slow suite by editing
// bcryptCost itself.
func TestBcryptCost_productionUnchanged(t *testing.T) {
	t.Parallel()

	if bcryptCost != productionBcryptCost {
		t.Errorf("bcryptCost = %d, want %d — production password hashing has been weakened",
			bcryptCost, productionBcryptCost)
	}
}

// TestHashCost_defaultIsCheap verifies that a plain `make test-integration`
// really does mint below the production cost; otherwise the suite is paying for
// hashes it does not need and the knob is silently doing nothing. A run that
// sets EnvTestBcryptCost on purpose (e.g. to reproduce production timings) is
// skipped rather than failed.
func TestHashCost_defaultIsCheap(t *testing.T) {
	t.Parallel()

	if raw := os.Getenv(EnvTestBcryptCost); raw != "" {
		t.Skipf("%s=%s overrides the default cost", EnvTestBcryptCost, raw)
	}
	if hashCost >= productionBcryptCost {
		t.Errorf("integration build mints at cost %d, want below %d", hashCost, productionBcryptCost)
	}
}

// TestTestHashCost verifies how the EnvTestBcryptCost value is interpreted:
// unset means the cheapest cost bcrypt accepts, a valid number is honoured, and
// anything else panics rather than falling back to a cost nobody asked for.
func TestTestHashCost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		raw       string
		want      int
		wantPanic bool
	}{
		{name: "unset falls back to the cheapest cost", raw: "", want: bcrypt.MinCost},
		{name: "minimum is honoured", raw: strconv.Itoa(bcrypt.MinCost), want: bcrypt.MinCost},
		{name: "production cost is honoured", raw: strconv.Itoa(productionBcryptCost), want: productionBcryptCost},
		{name: "maximum is honoured", raw: strconv.Itoa(bcrypt.MaxCost), want: bcrypt.MaxCost},
		{name: "below the minimum panics", raw: strconv.Itoa(bcrypt.MinCost - 1), wantPanic: true},
		{name: "above the maximum panics", raw: strconv.Itoa(bcrypt.MaxCost + 1), wantPanic: true},
		{name: "non-numeric panics", raw: "cheap", wantPanic: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				got := recover()
				if tt.wantPanic && got == nil {
					t.Errorf("testHashCost(%q) did not panic, want a panic", tt.raw)
				}
				if !tt.wantPanic && got != nil {
					t.Errorf("testHashCost(%q) panicked: %v", tt.raw, got)
				}
			}()

			if got := testHashCost(tt.raw); !tt.wantPanic && got != tt.want {
				t.Errorf("testHashCost(%q) = %d, want %d", tt.raw, got, tt.want)
			}
		})
	}
}
