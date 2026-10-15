// Package notifypurgejob keeps the notification records from growing forever:
// the `notification_purge` job deletes, in one project, every read notification
// older than the read retention and every unread one older than the unread
// retention (notification.Store.Purge), and a scheduler enqueues one such job
// per project once a day.
//
// A deactivated project is purged like any other. Its notifications are
// already out of reach — a member of a deactivated project is answered 404 at
// /p/<slug>/ and so never sees its inbox, and every push or e-mail still queued
// for it is dropped when it runs (internal/notifystanding) — and the normal
// retention then deletes them, so a library switched off does not keep its
// titles, album names and frozen photo sets forever. Nothing is purged early at
// deactivation: a project switched back on finds its recent inbox as it was.
//
// It follows the task digest's shape (internal/taskdigestjob): the scheduler
// only enqueues, the work runs in the queue, so a run is retried, observable
// and dead-lettered like every other one. Unlike the digest it is always on —
// an inbox is part of every account — and it fires once at startup too, so an
// instance restarted daily still purges.
//
// notification_purge is a library-wide job (see internal/jobs): its payload
// names the project as project_uid, read back with jobs.ProjectUIDOf, and the
// queue keeps at most one queued purge per project.
package notifypurgejob

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/project"
)

// Interval is how often the scheduler enqueues a purge. Retention is counted
// in weeks, so a day's slack is invisible.
const Interval = 24 * time.Hour

// Purger deletes the notifications past their retention. It is satisfied by
// *notification.Store.
type Purger interface {
	// Purge deletes, as of now, the project's read notifications older than
	// r.Read and its unread ones older than r.Unread, and returns how many it
	// deleted.
	Purge(ctx context.Context, projectUID string, now time.Time, r notification.Retention) (int64, error)
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

// Handle runs one purge of the project the job's payload names
// (jobs.ProjectUIDOf). A job queued before the purge was per project names
// none; it purges the bootstrap project, the only one there was then, and the
// fallback is logged (jobs.ProjectUIDOrLegacy). A failed
// purge is returned for the queue to retry; an invalid retention fails every
// attempt the same way, which the dead letter then shows.
func (s *Service) Handle(ctx context.Context, job jobs.Job) error {
	projectUID := jobs.ProjectUIDOrLegacy(ctx, s.log, job, project.BootstrapUID)
	deleted, err := s.notes.Purge(ctx, projectUID, s.clock(), s.retention)
	if err != nil {
		return fmt.Errorf("notifypurgejob: purging project %s: %w", projectUID, err)
	}
	s.log.InfoContext(ctx, "notification retention purge done",
		slog.String("project_uid", projectUID), slog.Int64("deleted", deleted))
	return nil
}

// PurgeEnqueuer puts one `notification_purge` job on the queue. It is
// satisfied by *jobs.Enqueuer.
type PurgeEnqueuer interface {
	// EnqueueNotificationPurge schedules one purge of the project projectUID.
	EnqueueNotificationPurge(ctx context.Context, projectUID string) error
}

// Projects lists the instance's projects, so the scheduler can enqueue one
// purge per project; *project.Store satisfies it.
type Projects interface {
	// List returns every project, active or not.
	List(ctx context.Context) ([]project.Project, error)
}

// Scheduler enqueues the purge of every project — deactivated ones included —
// at startup and then once per interval, until its context ends.
type Scheduler struct {
	enqueuer PurgeEnqueuer
	projects Projects
	interval time.Duration
	log      *slog.Logger
}

// NewScheduler returns a Scheduler enqueuing through enqueuer, for every
// project projects lists, every interval (a non-positive interval uses
// Interval). A nil logger uses slog.Default(). It panics without an enqueuer or
// a project lister.
func NewScheduler(enqueuer PurgeEnqueuer, projects Projects, interval time.Duration, log *slog.Logger) *Scheduler {
	if enqueuer == nil {
		panic("notifypurgejob: NewScheduler requires an enqueuer")
	}
	if projects == nil {
		panic("notifypurgejob: NewScheduler requires a project lister")
	}
	if interval <= 0 {
		interval = Interval
	}
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{enqueuer: enqueuer, projects: projects, interval: interval, log: log}
}

// Run enqueues one round of purges now and one every interval after, until ctx
// is cancelled. Intended to be launched in a goroutine for the lifetime of the
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

// tick enqueues one round, logging (and otherwise swallowing) a failure so a
// transient queue error never stops the loop.
func (s *Scheduler) tick(ctx context.Context) {
	if _, err := s.Tick(ctx); err != nil {
		if ctx.Err() != nil {
			return
		}
		s.log.WarnContext(ctx, "notification purge: enqueue failed", slog.String("error", err.Error()))
	}
}

// Tick enqueues one purge for every project and returns how many it enqueued
// (a project whose purge is already queued counts: the enqueue is a no-op for
// it). A deactivated project is purged too — retention applies to it as to any
// other (see the package documentation). It goes on past a failed project and
// returns the failures joined.
func (s *Scheduler) Tick(ctx context.Context) (int, error) {
	all, err := s.projects.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("notifypurgejob: listing projects: %w", err)
	}
	var (
		enqueued int
		errs     []error
	)
	for _, p := range all {
		if err := s.enqueuer.EnqueueNotificationPurge(ctx, p.UID); err != nil {
			errs = append(errs, fmt.Errorf("enqueuing the purge of %s: %w", p.UID, err))
			continue
		}
		enqueued++
	}
	return enqueued, errors.Join(errs...)
}
