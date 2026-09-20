package taskdigestjob

import (
	"context"
	"log/slog"
	"time"
)

// DigestEnqueuer puts one `task_digest` job on the queue. It is satisfied by
// *jobs.Enqueuer.
type DigestEnqueuer interface {
	// EnqueueTaskDigest schedules one run of the digest.
	EnqueueTaskDigest(ctx context.Context) error
}

// SchedulerConfig bundles what the daily scheduler needs.
type SchedulerConfig struct {
	// Enabled mirrors tasks.digest.enabled AND mail.enabled: with either off
	// the scheduler is inert.
	Enabled bool
	// Enqueuer schedules the job; required when Enabled.
	Enqueuer DigestEnqueuer
	// Hour is the UTC hour of the day (0–23) the job is enqueued at.
	Hour int
	// Clock reads the current time; nil uses the wall clock.
	Clock func() time.Time
	// Logger records each enqueue; nil uses slog.Default().
	Logger *slog.Logger
}

// Scheduler enqueues the `task_digest` job once a day at the configured hour.
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
	hour     int
	clock    func() time.Time
	log      *slog.Logger
}

// NewScheduler builds a Scheduler from cfg, defaulting Clock to the wall
// clock and Logger to slog.Default(). It panics when enabled without an
// enqueuer — a scheduler that fires into nothing is a wiring bug.
func NewScheduler(cfg SchedulerConfig) *Scheduler {
	if cfg.Enabled && cfg.Enqueuer == nil {
		panic("taskdigestjob: NewScheduler requires an Enqueuer when enabled")
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{enabled: cfg.Enabled, enqueuer: cfg.Enqueuer, hour: cfg.Hour, clock: clock, log: log}
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

// tick enqueues one run and logs the outcome, swallowing the error (other than
// to log it) so a transient queue failure never stops the loop.
func (s *Scheduler) tick(ctx context.Context) {
	if err := s.enqueuer.EnqueueTaskDigest(ctx); err != nil {
		if ctx.Err() != nil {
			return
		}
		s.log.WarnContext(ctx, "task digest: enqueue failed", slog.String("error", err.Error()))
		return
	}
	s.log.InfoContext(ctx, "task digest: job enqueued")
}
