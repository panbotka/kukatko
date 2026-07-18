package review

import (
	"errors"
	"testing"
	"time"
)

// TestParseWindow covers the accepted window values, the empty default, and the
// rejection of an unknown value.
func TestParseWindow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		raw     string
		want    LeaderboardWindow
		wantErr error
	}{
		{"empty defaults to all-time", "", WindowAllTime, nil},
		{"explicit all", "all", WindowAllTime, nil},
		{"week", "7d", WindowWeek, nil},
		{"today", "today", WindowToday, nil},
		{"unknown is rejected", "month", "", ErrInvalidWindow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseWindow(tt.raw)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ParseWindow(%q) err = %v, want %v", tt.raw, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseWindow(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// TestWindowCutoff verifies each window's lower bound relative to a fixed now:
// all-time is unbounded, week is now−7d, today is local midnight.
func TestWindowCutoff(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 18, 15, 30, 45, 0, time.UTC)
	tests := []struct {
		name   string
		window LeaderboardWindow
		want   *time.Time
	}{
		{"all-time has no bound", WindowAllTime, nil},
		{"week is seven days back", WindowWeek, new(now.AddDate(0, 0, -7))},
		{"today is local midnight", WindowToday, new(time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := windowCutoff(tt.window, now)
			switch {
			case tt.want == nil && got != nil:
				t.Fatalf("windowCutoff = %v, want nil", got)
			case tt.want == nil:
				// both nil, nothing more to check
			case got == nil:
				t.Fatalf("windowCutoff = nil, want %v", *tt.want)
			case !got.Equal(*tt.want):
				t.Errorf("windowCutoff = %v, want %v", *got, *tt.want)
			}
		})
	}
}
