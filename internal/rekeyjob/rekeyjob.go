// Package rekeyjob is the queue handler that drives a project's storage move:
// the storage_rekey job, one per project, stepping the run internal/rekey
// records until it is done or failed.
//
// It is the scheduled half of internal/rekey — that package knows how to move a
// photo safely and keeps the run, this one knows when. The operator starts,
// pauses, resumes and retries the run (CLI, API or ctl); Start, Resume and Retry
// enqueue the job in their own transaction. The handler never decides to move
// anything on its own: it reads the run and does what the run's state says.
//
// # One second at a time
//
// A pass of the handler takes one batch — storage.rekey.per_second photos, about
// a second of work — and then requeues its own row (a deferral, which burns no
// attempt) to run again once that second is up. So the job never holds its
// worker slot for more than a few seconds, a restart loses at most the photo in
// hand (which the next step re-verifies and finishes), and the pace is the
// configured rate however fast the store is. A paused run is re-read once a
// minute; a resume pulls that re-read forward (jobs.EnqueueStorageRekey). A run
// that is done or failed completes the job, and nothing re-enqueues it until an
// operator retries or starts again.
package rekeyjob

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/rekey"
	"github.com/panbotka/kukatko/internal/worker"
)

// Pacing of the job.
const (
	// DefaultPerSecond is the batch, and the photos-per-second pace, when the
	// configuration names none: storage.rekey.per_second's default.
	DefaultPerSecond = 10
	// PausedRecheck is how long a job that found its run paused waits before it
	// reads the run again.
	PausedRecheck = time.Minute
)

// The deferral causes, so a deferred job's last word says why it waits.
var (
	// errMoreWork is the cause of the deferral between two batches.
	errMoreWork = errors.New("rekeyjob: more photos to move")
	// errPaused is the cause of the deferral of a paused run.
	errPaused = errors.New("rekeyjob: the storage move is paused")
	// errNoProject is the terminal failure of a job naming no project.
	errNoProject = errors.New("rekeyjob: the job names no project")
)

// Mover is the part of the storage move the job drives. It is satisfied by
// *rekey.Service.
type Mover interface {
	// Status returns the project's most recent run, or rekey.ErrNoRun.
	Status(ctx context.Context, projectUID string) (rekey.Run, error)
	// Step moves up to batchSize photos of run and reports how many it took
	// and whether the pass is over.
	Step(ctx context.Context, run rekey.Run, batchSize int) (int, bool, error)
	// Finish closes a run whose pass is over.
	Finish(ctx context.Context, run rekey.Run) (rekey.Run, error)
}

// compile-time assertion that the real service is what the job drives.
var _ Mover = (*rekey.Service)(nil)

// Config bundles the dependencies of New. Mover is required.
type Config struct {
	// Mover moves the photos and keeps the run.
	Mover Mover
	// PerSecond is the batch one pass takes and the photos-per-second pace;
	// DefaultPerSecond when not positive.
	PerSecond int
	// Clock reads the time a batch took; nil is the wall clock.
	Clock func() time.Time
	// Logger receives the run's end. Defaults to slog.Default().
	Logger *slog.Logger
}

// Service handles storage_rekey jobs.
type Service struct {
	mover     Mover
	perSecond int
	clock     func() time.Time
	log       *slog.Logger
}

// New returns a Service from cfg. It panics when Mover is missing, which is a
// wiring bug and should fail at startup rather than on the first job.
func New(cfg Config) *Service {
	if cfg.Mover == nil {
		panic("rekeyjob: Mover is required")
	}
	perSecond := cfg.PerSecond
	if perSecond <= 0 {
		perSecond = DefaultPerSecond
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{mover: cfg.Mover, perSecond: perSecond, clock: clock, log: log}
}

// Handle runs one pass of a storage_rekey job over the run of the project its
// payload names:
//
//   - running: it steps one batch and returns a deferral that brings it back
//     once the batch's share of the pace has passed — or, when the pass is over,
//     finishes the run and completes;
//   - paused: it returns a deferral of PausedRecheck;
//   - done or failed, or no run at all: it completes, and nothing re-enqueues it.
//
// A run that the step itself failed (too many errors) or that Finish failed
// (photos, or their thumbnails and segments, left behind) completes the job too: the run says failed, and Retry
// schedules a new job. A failure that is not about the run — the database or
// the store unreachable — is returned as it is, so the queue retries it with
// backoff; the run stays exactly resumable. A payload naming no project is a
// terminal failure.
func (s *Service) Handle(ctx context.Context, job jobs.Job) error {
	projectUID := jobs.ProjectUIDOf(job)
	if projectUID == "" {
		// The worker's own marker types must reach it as they are, not wrapped.
		return worker.Terminal(fmt.Errorf("%w (job %d)", errNoProject, job.ID)) //nolint:wrapcheck // see above
	}
	run, err := s.mover.Status(ctx, projectUID)
	if errors.Is(err, rekey.ErrNoRun) {
		s.log.InfoContext(ctx, "storage move: no run to step", slog.String("project_uid", projectUID))
		return nil
	}
	if err != nil {
		return fmt.Errorf("rekeyjob: reading the run of %s: %w", projectUID, err)
	}
	switch run.State {
	case rekey.StateRunning:
		return s.step(ctx, run)
	case rekey.StatePaused:
		return worker.RetryAfter(PausedRecheck, errPaused) //nolint:wrapcheck // the worker's marker, as it is
	case rekey.StateDone, rekey.StateFailed:
		return nil
	}
	return fmt.Errorf("rekeyjob: run %s is in an unknown state %q", run.UID, run.State)
}

// step moves one batch of run and says when the job runs next.
func (s *Service) step(ctx context.Context, run rekey.Run) error {
	started := s.clock()
	processed, done, err := s.mover.Step(ctx, run, s.perSecond)
	switch {
	case errors.Is(err, rekey.ErrRunFailed):
		s.logEnd(ctx, run, "storage move failed: too many errors")
		return nil
	case errors.Is(err, rekey.ErrNotRunning):
		// The run changed state since it was read; the next pass reads it again.
		return worker.RetryAfter(0, err) //nolint:wrapcheck // the worker's marker, as it is
	case err != nil:
		return fmt.Errorf("rekeyjob: stepping run %s: %w", run.UID, err)
	case done:
		return s.finish(ctx, run)
	}
	return worker.RetryAfter( //nolint:wrapcheck // the worker's marker, as it is
		s.pace(processed, s.clock().Sub(started)), errMoreWork)
}

// finish closes run once its pass is over.
func (s *Service) finish(ctx context.Context, run rekey.Run) error {
	_, err := s.mover.Finish(ctx, run)
	switch {
	case err == nil:
		s.logEnd(ctx, run, "storage move done")
		return nil
	case errors.Is(err, rekey.ErrPhotosRemain):
		s.logEnd(ctx, run, "storage move failed: photos left in the old layout")
		return nil
	case errors.Is(err, rekey.ErrDerivedRemain):
		s.logEnd(ctx, run, "storage move failed: derived objects left in the old layout")
		return nil
	case errors.Is(err, rekey.ErrPendingCleanup), errors.Is(err, rekey.ErrNotRunning):
		// Something is still to do, or the run changed state: read it again.
		return worker.RetryAfter(0, err) //nolint:wrapcheck // the worker's marker, as it is
	}
	return fmt.Errorf("rekeyjob: finishing run %s: %w", run.UID, err)
}

// pace returns how long to wait after a batch of processed photos that took
// elapsed, so the move never runs faster than perSecond photos a second.
func (s *Service) pace(processed int, elapsed time.Duration) time.Duration {
	budget := time.Duration(processed) * time.Second / time.Duration(s.perSecond)
	return max(budget-elapsed, 0)
}

// logEnd logs the end of a run with what it reached.
func (s *Service) logEnd(ctx context.Context, run rekey.Run, msg string) {
	s.log.InfoContext(ctx, msg,
		slog.String("project_uid", run.ProjectUID),
		slog.String("run", run.UID))
}
