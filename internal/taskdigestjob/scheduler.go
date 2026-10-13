package taskdigestjob

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/panbotka/kukatko/internal/phototask"
)

// DigestEnqueuer puts one `task_digest` job on the queue. It is satisfied by
// *jobs.Enqueuer.
type DigestEnqueuer interface {
	// EnqueueTaskDigest schedules the digest of one person in one project.
	EnqueueTaskDigest(ctx context.Context, projectUID, userUID string) error
}

// TargetSource names the (project, person) pairs that have a digest due. It is
// satisfied by *phototask.Store.
type TargetSource interface {
	// DigestTargets returns every pair with something new waiting on it.
	DigestTargets(ctx context.Context) ([]phototask.DigestTarget, error)
}

// SchedulerConfig bundles what the daily scheduler needs.
type SchedulerConfig struct {
	// Enabled mirrors tasks.digest.enabled AND mail.enabled: with either off
	// the scheduler is inert.
	Enabled bool
	// Enqueuer schedules the jobs; required when Enabled.
	Enqueuer DigestEnqueuer
	// Targets names the pairs to enqueue a job for; required when Enabled.
	Targets TargetSource
	// Hour is the UTC hour of the day (0–23) the job is enqueued at.
	Hour int
	// Clock reads the current time; nil uses the wall clock.
	Clock func() time.Time
	// Logger records each enqueue; nil uses slog.Default().
	Logger *slog.Logger
}

// Scheduler enqueues the `task_digest` jobs once a day at the configured hour:
// one per (project, person) pair the store says has something due, so each
// project's digest is sent, retried and stamped on its own.
// It follows the trash retention purge's shape — a goroutine for the lifetime
// of the server, inert when the feature is off — but enqueues a job rather
// than doing the work, so the run is retried, observable and dead-lettered like
// every other one. Missed hours are not caught up: a server that was down at
// the hour sends nothing until the next one, which for a daily digest is the
// right call — yesterday's news arriving at noon is what the mail's own stamp
// exists to prevent.
type Scheduler struct {
	enabled  bool
	enqueuer DigestEnqueuer
	targets  TargetSource
	hour     int
	clock    func() time.Time
	log      *slog.Logger
}

// NewScheduler builds a Scheduler from cfg, defaulting Clock to the wall
// clock and Logger to slog.Default(). It panics when enabled without an
// enqueuer or a target source — a scheduler that fires into nothing is a
// wiring bug.
func NewScheduler(cfg SchedulerConfig) *Scheduler {
	if cfg.Enabled && (cfg.Enqueuer == nil || cfg.Targets == nil) {
		panic("taskdigestjob: NewScheduler requires an Enqueuer and Targets when enabled")
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{
		enabled: cfg.Enabled, enqueuer: cfg.Enqueuer, targets: cfg.Targets, hour: cfg.Hour, clock: clock, log: log,
	}
}

// NextRun returns the first instant strictly after now at which the clock reads
// hour:00:00 UTC — today if that hour is still ahead, otherwise tomorrow. now
// is read in UTC whatever zone it carries, because the hour is configured as
// one.
func NextRun(now time.Time, hour int) time.Time {
	utc := now.UTC()
	next := time.Date(utc.Year(), utc.Month(), utc.Day(), hour, 0, 0, 0, time.UTC)
	if !next.After(utc) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// Run waits for each daily fire time and enqueues the job, until ctx is
// cancelled. It returns at once when the scheduler is disabled. Intended to be
// launched in a goroutine for the lifetime of the server process.
func (s *Scheduler) Run(ctx context.Context) {
	if !s.enabled {
		s.log.InfoContext(ctx, "task digest: scheduled digest disabled")
		return
	}
	s.log.InfoContext(ctx, "task digest: scheduled daily", slog.Int("utc_hour", s.hour))
	for {
		next := NextRun(s.clock(), s.hour)
		timer := time.NewTimer(next.Sub(s.clock()))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.tick(ctx)
		}
	}
}

// tick enqueues one job per due (project, person) pair and logs the outcome,
// swallowing errors (other than to log them) so a transient failure never
// stops the loop. A failed enqueue of one pair does not keep the others from
// theirs.
func (s *Scheduler) tick(ctx context.Context) {
	enqueued, err := s.Tick(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		s.log.WarnContext(ctx, "task digest: enqueue failed", slog.Int("enqueued", enqueued),
			slog.String("error", err.Error()))
		return
	}
	s.log.InfoContext(ctx, "task digest: jobs enqueued", slog.Int("enqueued", enqueued))
}

// Tick enqueues one `task_digest` job for every (project, person) pair with a
// digest due and returns how many it enqueued (a pair already queued counts:
// the enqueue is a no-op for it). It goes on past a failed pair and returns
// the failures joined.
func (s *Scheduler) Tick(ctx context.Context) (int, error) {
	targets, err := s.targets.DigestTargets(ctx)
	if err != nil {
		return 0, fmt.Errorf("taskdigestjob: reading who is due: %w", err)
	}
	var (
		enqueued int
		errs     []error
	)
	for _, target := range targets {
		if err := s.enqueuer.EnqueueTaskDigest(ctx, target.ProjectUID, target.UserUID); err != nil {
			errs = append(errs, fmt.Errorf("enqueuing the digest of %s in %s: %w", target.UserUID, target.ProjectUID, err))
			continue
		}
		enqueued++
	}
	return enqueued, errors.Join(errs...)
}
