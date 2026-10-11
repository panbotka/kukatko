// Package notifypurgejob keeps the notification records from growing forever:
// the `notification_purge` job deletes every read notification older than the
// read retention and every unread one older than the unread retention
// (notification.Store.Purge), and a scheduler enqueues that job once a day.
//
// It follows the task digest's shape (internal/taskdigestjob): the scheduler
// only enqueues, the work runs in the queue, so a run is retried, observable
// and dead-lettered like every other one. Unlike the digest it is always on —
// an inbox is part of every account — and it fires once at startup too, so an
// instance restarted daily still purges.
package notifypurgejob

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/notification"
)

// Interval is how often the scheduler enqueues a purge. Retention is counted
// in weeks, so a day's slack is invisible.
const Interval = 24 * time.Hour

// Purger deletes the notifications past their retention. It is satisfied by
// *notification.Store.
type Purger interface {
	// Purge deletes, as of now, the read notifications older than r.Read and the
	// unread ones older than r.Unread, and returns how many it deleted.
	Purge(ctx context.Context, now time.Time, r notification.Retention) (int64, error)
}

// Config bundles the dependencies of New.
type Config struct {
	// Notifications runs the purge. Required.
	Notifications Purger
	// Retention is the two thresholds; the zero value uses
	// notification.DefaultRetention().
	Retention notification.Retention
	// Clock reads the current time; nil uses the wall clock.
	Clock func() time.Time
	// Logger records each run; nil uses slog.Default().
	Logger *slog.Logger
}

// Service is the `notification_purge` job handler.
type Service struct {
	notes     Purger
	retention notification.Retention
	clock     func() time.Time
	log       *slog.Logger
}

// New returns a Service from cfg. It panics without Notifications, which is a
// wiring bug and should fail at startup rather than on the first run.
func New(cfg Config) *Service {
	if cfg.Notifications == nil {
		panic("notifypurgejob: Notifications is required")
	}
	retention := cfg.Retention
	if retention == (notification.Retention{}) {
		retention = notification.DefaultRetention()
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{notes: cfg.Notifications, retention: retention, clock: clock, log: log}
}

// Handle runs one purge. The payload is ignored — there is nothing to name. A
// failed purge is returned for the queue to retry; an invalid retention fails
// every attempt the same way, which the dead letter then shows.
func (s *Service) Handle(ctx context.Context, _ jobs.Job) error {
	deleted, err := s.notes.Purge(ctx, s.clock(), s.retention)
	if err != nil {
		return fmt.Errorf("notifypurgejob: purging: %w", err)
	}
	s.log.InfoContext(ctx, "notification retention purge done", slog.Int64("deleted", deleted))
	return nil
}

// PurgeEnqueuer puts one `notification_purge` job on the queue. It is
// satisfied by *jobs.Enqueuer.
type PurgeEnqueuer interface {
	// EnqueueNotificationPurge schedules one purge.
	EnqueueNotificationPurge(ctx context.Context) error
}

// Scheduler enqueues the purge at startup and then once per interval, until its
// context ends.
type Scheduler struct {
	enqueuer PurgeEnqueuer
	interval time.Duration
	log      *slog.Logger
}

// NewScheduler returns a Scheduler enqueuing through enqueuer every interval (a
// non-positive interval uses Interval). A nil logger uses slog.Default(). It
// panics without an enqueuer.
func NewScheduler(enqueuer PurgeEnqueuer, interval time.Duration, log *slog.Logger) *Scheduler {
	if enqueuer == nil {
		panic("notifypurgejob: NewScheduler requires an enqueuer")
	}
	if interval <= 0 {
		interval = Interval
	}
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{enqueuer: enqueuer, interval: interval, log: log}
}

// Run enqueues one purge now and one every interval after, until ctx is
// cancelled. Intended to be launched in a goroutine for the lifetime of the
// server process.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// tick enqueues one purge, logging (and otherwise swallowing) a failure so a
// transient queue error never stops the loop.
func (s *Scheduler) tick(ctx context.Context) {
	if err := s.enqueuer.EnqueueNotificationPurge(ctx); err != nil {
		if ctx.Err() != nil {
			return
		}
		s.log.WarnContext(ctx, "notification purge: enqueue failed", slog.String("error", err.Error()))
	}
}
