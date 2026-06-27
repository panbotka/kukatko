package backup

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestParseSchedule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		spec    string
		wantErr error
	}{
		{name: "standard cron", spec: "0 3 * * *", wantErr: nil},
		{name: "daily descriptor", spec: "@daily", wantErr: nil},
		{name: "every duration", spec: "@every 6h", wantErr: nil},
		{name: "whitespace trimmed", spec: "  @hourly  ", wantErr: nil},
		{name: "empty disabled", spec: "", wantErr: ErrNoSchedule},
		{name: "blank disabled", spec: "   ", wantErr: ErrNoSchedule},
		{name: "garbage invalid", spec: "not-a-cron", wantErr: ErrInvalidSchedule},
		{name: "too few fields invalid", spec: "0 3 *", wantErr: ErrInvalidSchedule},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			schedule, err := ParseSchedule(tt.spec)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ParseSchedule(%q) error = %v, want %v", tt.spec, err, tt.wantErr)
			}
			if tt.wantErr == nil {
				if schedule == nil {
					t.Fatal("ParseSchedule returned a nil schedule on success")
				}
				next := schedule.Next(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
				if !next.After(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
					t.Errorf("schedule.Next did not advance: %v", next)
				}
			}
		})
	}
}

func TestRunSchedule_disabledReturnsImmediately(t *testing.T) {
	t.Parallel()
	svc := New(Config{Objects: newFakeStore(nil), Originals: &fakeOriginals{}, Dumper: &fakeDumper{}})
	done := make(chan struct{})
	go func() {
		// An empty schedule disables scheduled backups and must return at once.
		svc.RunSchedule(context.Background(), "")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunSchedule with an empty schedule did not return")
	}
}

func TestRunSchedule_invalidReturnsImmediately(t *testing.T) {
	t.Parallel()
	svc := New(Config{Objects: newFakeStore(nil), Originals: &fakeOriginals{}, Dumper: &fakeDumper{}})
	done := make(chan struct{})
	go func() {
		svc.RunSchedule(context.Background(), "totally invalid")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunSchedule with an invalid schedule did not return")
	}
}

func TestRunSchedule_stopsOnContextCancel(t *testing.T) {
	t.Parallel()
	svc := New(Config{Objects: newFakeStore(nil), Originals: &fakeOriginals{}, Dumper: &fakeDumper{}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		// A far-future schedule means the loop is parked waiting; cancelling ctx
		// must unblock it.
		svc.RunSchedule(ctx, "0 0 1 1 *")
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunSchedule did not stop on context cancellation")
	}
}
