package notifypurgejob

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/project"
)

// fakePurger records the arguments of the last purge.
type fakePurger struct {
	project   string
	now       time.Time
	retention notification.Retention
	err       error
}

// Purge records its arguments and reports three deletions.
func (f *fakePurger) Purge(_ context.Context, projectUID string, now time.Time, r notification.Retention) (int64, error) {
	f.project, f.now, f.retention = projectUID, now, r
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

// TestHandle_purgesThePayloadsProject checks a run purges the project its
// payload names, and a legacy payload naming none the bootstrap project.
func TestHandle_purgesThePayloadsProject(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		payload string
		want    string
	}{
		{name: "named", payload: `{"project_uid":"pjb"}`, want: "pjb"},
		{name: "legacy empty payload", payload: `{}`, want: project.BootstrapUID},
		{name: "no payload", want: project.BootstrapUID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			purger := &fakePurger{}
			job := jobs.Job{Type: jobs.TypeNotificationPurge}
			if tt.payload != "" {
				job.Payload = []byte(tt.payload)
			}
			if err := New(Config{Notifications: purger}).Handle(t.Context(), job); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			if purger.project != tt.want {
				t.Fatalf("purged project %q, want %q", purger.project, tt.want)
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

// countingEnqueuer counts enqueues per project and fails when told to.
type countingEnqueuer struct {
	mu       sync.Mutex
	calls    atomic.Int32
	projects []string
	err      error
}

// EnqueueNotificationPurge counts one call and records its project.
func (c *countingEnqueuer) EnqueueNotificationPurge(_ context.Context, projectUID string) error {
	c.calls.Add(1)
	c.mu.Lock()
	c.projects = append(c.projects, projectUID)
	c.mu.Unlock()
	return c.err
}

// fakeProjects lists a fixed set of projects, or fails.
type fakeProjects struct {
	list []project.Project
	err  error
}

// List returns the fixed list.
func (f fakeProjects) List(context.Context) ([]project.Project, error) {
	return f.list, f.err
}

// oneProject is a lister with just the bootstrap project, active.
var oneProject = fakeProjects{list: []project.Project{{UID: project.BootstrapUID, Active: true}}}

// TestScheduler_firesAtStartAndOnEveryTick checks the scheduler enqueues at
// once, again on each interval, keeps going after a failed enqueue and stops
// with its context.
func TestScheduler_firesAtStartAndOnEveryTick(t *testing.T) {
	t.Parallel()

	enqueuer := &countingEnqueuer{err: errors.New("queue down")}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		NewScheduler(enqueuer, oneProject, 5*time.Millisecond, nil).Run(ctx)
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

// TestScheduler_TickEnqueuesEveryProject checks one round enqueues one purge
// per project — a deactivated one included, so its notifications do not
// outlive the retention — and reports a lister failure.
func TestScheduler_TickEnqueuesEveryProject(t *testing.T) {
	t.Parallel()

	enqueuer := &countingEnqueuer{}
	projects := fakeProjects{list: []project.Project{
		{UID: "pja", Active: true}, {UID: "pjoff"}, {UID: "pjb", Active: true},
	}}
	n, err := NewScheduler(enqueuer, projects, time.Hour, nil).Tick(t.Context())
	if err != nil || n != 3 {
		t.Fatalf("Tick = %d, %v; want 3, nil", n, err)
	}
	if !slices.Equal(enqueuer.projects, []string{"pja", "pjoff", "pjb"}) {
		t.Fatalf("enqueued %v, want [pja pjoff pjb]", enqueuer.projects)
	}

	boom := errors.New("db down")
	if _, err := NewScheduler(enqueuer, fakeProjects{err: boom}, time.Hour, nil).Tick(t.Context()); !errors.Is(err, boom) {
		t.Fatalf("Tick with a failing lister = %v, want the failure", err)
	}
}

// TestNewScheduler_defaults checks a non-positive interval falls back to a day
// and a missing enqueuer or lister panics.
func TestNewScheduler_defaults(t *testing.T) {
	t.Parallel()

	if s := NewScheduler(&countingEnqueuer{}, oneProject, 0, nil); s.interval != Interval || s.log == nil {
		t.Fatalf("defaults: interval %s, logger set %v", s.interval, s.log != nil)
	}
	for name, build := range map[string]func(){
		"no enqueuer": func() { NewScheduler(nil, oneProject, time.Hour, nil) },
		"no lister":   func() { NewScheduler(&countingEnqueuer{}, nil, time.Hour, nil) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Fatalf("NewScheduler with %s did not panic", name)
				}
			}()
			build()
		})
	}
}
