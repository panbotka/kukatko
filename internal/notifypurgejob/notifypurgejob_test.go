package notifypurgejob

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/notification"
)

// fakePurger records the arguments of the last purge.
type fakePurger struct {
	now       time.Time
	retention notification.Retention
	err       error
}

// Purge records its arguments and reports three deletions.
func (f *fakePurger) Purge(_ context.Context, now time.Time, r notification.Retention) (int64, error) {
	f.now, f.retention = now, r
	return 3, f.err
}

// TestHandle_purgesWithTheRetention checks a run purges as of the clock with
// the default retention when none is configured, and with the configured one
// otherwise.
func TestHandle_purgesWithTheRetention(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 11, 3, 0, 0, 0, time.UTC)
	custom := notification.Retention{Read: time.Hour, Unread: 2 * time.Hour}
	for _, tt := range []struct {
		name string
		in   notification.Retention
		want notification.Retention
	}{
		{name: "default", want: notification.DefaultRetention()},
		{name: "configured", in: custom, want: custom},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			purger := &fakePurger{}
			svc := New(Config{Notifications: purger, Retention: tt.in, Clock: func() time.Time { return at }})
			if err := svc.Handle(t.Context(), jobs.Job{Type: jobs.TypeNotificationPurge}); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			if !purger.now.Equal(at) || purger.retention != tt.want {
				t.Fatalf("purged as of %v with %+v, want %v with %+v", purger.now, purger.retention, at, tt.want)
			}
		})
	}
}

// TestHandle_returnsTheFailure checks a failed purge goes back to the queue.
func TestHandle_returnsTheFailure(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	svc := New(Config{Notifications: &fakePurger{err: boom}})
	if err := svc.Handle(t.Context(), jobs.Job{}); !errors.Is(err, boom) {
		t.Fatalf("Handle = %v, want the wrapped failure", err)
	}
}

// TestNew_requiresThePurger checks wiring without a store panics.
func TestNew_requiresThePurger(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("New without Notifications did not panic")
		}
	}()
	New(Config{})
}

// countingEnqueuer counts enqueues and fails when told to.
type countingEnqueuer struct {
	calls atomic.Int32
	err   error
}

// EnqueueNotificationPurge counts one call.
func (c *countingEnqueuer) EnqueueNotificationPurge(context.Context) error {
	c.calls.Add(1)
	return c.err
}

// TestScheduler_firesAtStartAndOnEveryTick checks the scheduler enqueues at
// once, again on each interval, keeps going after a failed enqueue and stops
// with its context.
func TestScheduler_firesAtStartAndOnEveryTick(t *testing.T) {
	t.Parallel()

	enqueuer := &countingEnqueuer{err: errors.New("queue down")}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		NewScheduler(enqueuer, 5*time.Millisecond, nil).Run(ctx)
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for enqueuer.calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if got := enqueuer.calls.Load(); got < 3 {
		t.Fatalf("enqueues = %d, want at least 3 (startup + ticks, despite failures)", got)
	}
}

// TestNewScheduler_defaults checks a non-positive interval falls back to a day
// and a missing enqueuer panics.
func TestNewScheduler_defaults(t *testing.T) {
	t.Parallel()

	if s := NewScheduler(&countingEnqueuer{}, 0, nil); s.interval != Interval || s.log == nil {
		t.Fatalf("defaults: interval %s, logger set %v", s.interval, s.log != nil)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("NewScheduler without an enqueuer did not panic")
		}
	}()
	NewScheduler(nil, time.Hour, nil)
}
