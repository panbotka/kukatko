package taskdigestjob

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/phototask"
)

// fakeEnqueuer counts the digest jobs the scheduler asked for, remembers their
// pairs and can stop the loop by cancelling the run's context on the first one.
type fakeEnqueuer struct {
	calls  int
	pairs  []string
	err    error
	failOn string
	cancel context.CancelFunc
}

// EnqueueTaskDigest records the call, cancels the run when asked to, and
// returns the scripted error (for every pair, or only for failOn).
func (f *fakeEnqueuer) EnqueueTaskDigest(_ context.Context, projectUID, userUID string) error {
	f.calls++
	if f.cancel != nil {
		f.cancel()
	}
	if f.failOn != "" && f.failOn == projectUID {
		return errors.New("queue refused")
	}
	f.pairs = append(f.pairs, projectUID+"/"+userUID)
	return f.err
}

// oneTarget is the target source of the loop tests: one pair is due.
var oneTarget = fakeTargets{{ProjectUID: "v", UserUID: "us-a"}}

// fakeTargets is a scripted TargetSource.
type fakeTargets []phototask.DigestTarget

// DigestTargets returns the scripted pairs.
func (f fakeTargets) DigestTargets(context.Context) ([]phototask.DigestTarget, error) {
	return f, nil
}

// TestNextRun verifies the next fire time is the configured UTC hour today
// when it is still ahead, tomorrow when it has passed or is exactly now, and
// that a clock in another zone is read as UTC.
func TestNextRun(t *testing.T) {
	t.Parallel()

	prague := time.FixedZone("CEST", 2*60*60)
	tests := []struct {
		name string
		now  time.Time
		hour int
		want time.Time
	}{
		{
			name: "hour still ahead today",
			now:  time.Date(2026, time.September, 20, 5, 30, 0, 0, time.UTC), hour: 7,
			want: time.Date(2026, time.September, 20, 7, 0, 0, 0, time.UTC),
		},
		{
			name: "hour already passed",
			now:  time.Date(2026, time.September, 20, 7, 0, 1, 0, time.UTC), hour: 7,
			want: time.Date(2026, time.September, 21, 7, 0, 0, 0, time.UTC),
		},
		{
			name: "exactly the hour is not strictly after",
			now:  time.Date(2026, time.September, 20, 7, 0, 0, 0, time.UTC), hour: 7,
			want: time.Date(2026, time.September, 21, 7, 0, 0, 0, time.UTC),
		},
		{
			name: "midnight rolls the month",
			now:  time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC), hour: 0,
			want: time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "a zoned clock is read as UTC",
			now:  time.Date(2026, time.September, 20, 8, 30, 0, 0, prague), hour: 7, // 06:30 UTC
			want: time.Date(2026, time.September, 20, 7, 0, 0, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := NextRun(tt.now, tt.hour); !got.Equal(tt.want) {
				t.Errorf("NextRun(%v, %d) = %v, want %v", tt.now, tt.hour, got, tt.want)
			}
		})
	}
}

// TestScheduler_disabledIsInert verifies a disabled scheduler returns at once
// without enqueuing anything — the "none when disabled" of the digest.
func TestScheduler_disabledIsInert(t *testing.T) {
	t.Parallel()

	enq := &fakeEnqueuer{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		NewScheduler(SchedulerConfig{Enabled: false, Enqueuer: enq, Hour: 7}).Run(context.Background())
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a disabled scheduler did not return")
	}
	if enq.calls != 0 {
		t.Errorf("enqueued %d times, want none", enq.calls)
	}
}

// TestScheduler_enqueuesAtTheHour verifies the loop waits for the hour and
// enqueues exactly one job per fire time: the clock is pinned a moment before
// the hour, so the first wait is short and the enqueuer cancels the run.
func TestScheduler_enqueuesAtTheHour(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	enq := &fakeEnqueuer{cancel: cancel}
	beforeHour := time.Date(2026, time.September, 20, 6, 59, 59, 999_000_000, time.UTC)
	s := NewScheduler(SchedulerConfig{
		Enabled: true, Enqueuer: enq, Targets: oneTarget, Hour: 7, Clock: func() time.Time { return beforeHour },
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(ctx)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the scheduler did not stop after its context was cancelled")
	}
	if enq.calls != 1 {
		t.Errorf("enqueued %d times, want exactly 1 before the cancel", enq.calls)
	}
}

// TestScheduler_tickSurvivesAFailure verifies a failing enqueue is logged and
// swallowed rather than ending the loop.
func TestScheduler_tickSurvivesAFailure(t *testing.T) {
	t.Parallel()

	enq := &fakeEnqueuer{err: errors.New("queue is down")}
	s := NewScheduler(SchedulerConfig{Enabled: true, Enqueuer: enq, Targets: oneTarget, Hour: 7})
	s.tick(context.Background())
	s.tick(context.Background())
	if enq.calls != 2 {
		t.Errorf("enqueued %d times, want the loop to keep ticking", enq.calls)
	}
}

// TestScheduler_stopsWhenCancelled verifies a scheduler waiting for a far-off
// hour returns promptly when its context is cancelled.
func TestScheduler_stopsWhenCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	enq := &fakeEnqueuer{}
	s := NewScheduler(SchedulerConfig{Enabled: true, Enqueuer: enq, Targets: oneTarget, Hour: 7})
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(ctx)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the scheduler did not stop after its context was cancelled")
	}
	if enq.calls != 0 {
		t.Errorf("enqueued %d times, want none", enq.calls)
	}
}

// TestScheduler_tickEnqueuesOneJobPerPair verifies a tick enqueues one job per
// due (project, person) pair — a person in two projects gets two — and that a
// pair the queue refuses neither stops the others nor goes unreported.
func TestScheduler_tickEnqueuesOneJobPerPair(t *testing.T) {
	t.Parallel()

	targets := fakeTargets{
		{ProjectUID: "pa", UserUID: "us-a"}, {ProjectUID: "pb", UserUID: "us-a"}, {ProjectUID: "pc", UserUID: "us-b"},
	}
	enq := &fakeEnqueuer{failOn: "pb"}
	s := NewScheduler(SchedulerConfig{Enabled: true, Enqueuer: enq, Targets: targets, Hour: 7})
	enqueued, err := s.Tick(context.Background())
	if err == nil {
		t.Error("Tick = nil, want the refused pair reported")
	}
	if enqueued != 2 || fmt.Sprint(enq.pairs) != "[pa/us-a pc/us-b]" {
		t.Errorf("enqueued %d: %v, want the two pairs the queue accepted", enqueued, enq.pairs)
	}
}

// TestNewScheduler_requiresItsCollaboratorsWhenEnabled verifies the wiring bug
// surfaces at startup, and that a disabled scheduler needs neither an enqueuer
// nor a target source.
func TestNewScheduler_requiresItsCollaboratorsWhenEnabled(t *testing.T) {
	t.Parallel()

	NewScheduler(SchedulerConfig{Enabled: false})
	for name, cfg := range map[string]SchedulerConfig{
		"no enqueuer": {Enabled: true, Targets: oneTarget},
		"no targets":  {Enabled: true, Enqueuer: &fakeEnqueuer{}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Errorf("NewScheduler(%s) did not panic", name)
				}
			}()
			NewScheduler(cfg)
		})
	}
}
