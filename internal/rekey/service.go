package rekey

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/storage"
)

// ObjectStore is the part of the store a move uses. *storage.FS and *storage.R2
// satisfy it; a store that also implements storage.ETagStater has its copies
// verified by ETag instead of by reading them back.
type ObjectStore interface {
	// Copy duplicates the object at from to the key to.
	Copy(ctx context.Context, from, to string) error
	// Stat reports the object at relPath, wrapping os.ErrNotExist when absent.
	Stat(ctx context.Context, relPath string) (os.FileInfo, error)
	// Open opens the object at relPath for reading.
	Open(ctx context.Context, relPath string) (io.ReadCloser, error)
	// Delete removes the object at relPath, wrapping os.ErrNotExist when absent.
	Delete(ctx context.Context, relPath string) error
	// KeysWithPrefix enumerates the keys under prefix — how Finish learns which
	// sidecars already have their replacement under the new prefix.
	KeysWithPrefix(ctx context.Context, prefix string, yield func(key string) error) error
}

// compile-time assertions that the real backends can be moved.
var (
	_ ObjectStore = (*storage.FS)(nil)
	_ ObjectStore = (*storage.R2)(nil)
)

// JobEnqueuer schedules the rewrites Finish asks for. *jobs.Enqueuer satisfies
// it.
type JobEnqueuer interface {
	// EnqueueFamilyExport schedules a rewrite of the project's families.yaml.
	EnqueueFamilyExport(ctx context.Context, projectUID string) error
	// EnqueueSidecar schedules a rewrite of the photo's metadata sidecar.
	EnqueueSidecar(ctx context.Context, photoUID string) error
}

// Config bundles the dependencies of New. Pool, Store and Jobs are required.
type Config struct {
	// Pool is the shared database pool; the Service owns no connection.
	Pool *pgxpool.Pool
	// Store is the object store being re-keyed.
	Store ObjectStore
	// Jobs schedules the sidecar and family-export rewrites of Finish.
	Jobs JobEnqueuer
	// Clock reads the current time for error timestamps; nil is the wall clock.
	Clock func() time.Time
	// Logger receives the skipped objects. Defaults to slog.Default().
	Logger *slog.Logger
}

// Service runs storage moves.
type Service struct {
	pool  *pgxpool.Pool
	store ObjectStore
	jobs  JobEnqueuer
	clock func() time.Time
	log   *slog.Logger
	// hook, when set, is called at the named points of a photo's move; an error
	// it returns aborts Step as a crash would. Only tests set it.
	hook func(stage string) error
}

// Stages a test hook can crash at.
const (
	// stageCopied is after the copy is verified, before the row moves.
	stageCopied = "copied"
	// stageCommitted is after the row moved, before anything is deleted.
	stageCommitted = "committed"
	// stageFamiliesListed is in Finish, after the listing found no genealogy
	// export under the new prefix and before the root one is copied there.
	stageFamiliesListed = "families-listed"
)

// New returns a Service from cfg. It panics when a required dependency is
// missing, which is a wiring bug and should fail at startup.
func New(cfg Config) *Service {
	if cfg.Pool == nil || cfg.Store == nil || cfg.Jobs == nil {
		panic("rekey: Pool, Store and Jobs are required")
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{pool: cfg.Pool, store: cfg.Store, jobs: cfg.Jobs, clock: clock, log: log}
}

// at runs the test hook for stage, if any.
func (s *Service) at(stage string) error {
	if s.hook == nil {
		return nil
	}
	return s.hook(stage)
}

// insertRunSQL records a new running run with its totals.
const insertRunSQL = `
INSERT INTO storage_rekey_runs (uid, project_uid, state, target_prefix, total_photos, total_bytes)
SELECT $1, $2, 'running', $3, count(*), COALESCE(sum(file_size), 0)::bigint
FROM photos WHERE project_id = $2 AND NOT starts_with(file_path, $3)
RETURNING ` + runColumns

// Start begins moving the project projectUID under projects/<uid>/ and returns
// the new run. In one transaction it switches the project's storage prefix — so
// every upload from this moment lands in the new layout — records the switch
// and the start in the audit log with meta's actor, inserts a running run whose
// totals are the count and summed size of the photos still in the old layout,
// and schedules the storage_rekey job that steps it (jobs.EnqueueStorageRekey,
// a no-op when the project's job is already scheduled).
//
// It returns ErrProjectNotFound for an unknown project, ErrRunActive when a run
// of the project is running or paused, and ErrAlreadyPrefixed when the project
// already has a prefix of its own and was never moved (a created project). A
// project whose earlier run is done or failed may be started again: the new run
// counts whatever is still in the old layout.
func (s *Service) Start(ctx context.Context, meta audit.Meta, projectUID string) (Run, error) {
	uid, err := newRunUID()
	if err != nil {
		return Run{}, err
	}
	target := project.StoragePrefixFor(projectUID)
	var run Run
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		current, err := s.checkStart(ctx, tx, projectUID, target)
		if err != nil {
			return err
		}
		if current != target {
			if err := s.switchPrefix(ctx, tx, meta, projectUID, current, target, uid); err != nil {
				return err
			}
		}
		run, err = queryRun(ctx, tx, ErrProjectNotFound, insertRunSQL, uid, projectUID, target)
		if err != nil {
			return err
		}
		return s.recordAndSchedule(ctx, tx, meta, audit.ActionProjectRekeyStart, run, true)
	})
	if err != nil {
		return Run{}, fmt.Errorf("rekey: starting the move of %s: %w", projectUID, err)
	}
	return run, nil
}

// checkStart locks the project row and returns its current storage prefix,
// refusing a start Start's contract forbids.
func (s *Service) checkStart(ctx context.Context, tx pgx.Tx, projectUID, target string) (string, error) {
	var (
		prefix       string
		active, ever bool
	)
	err := tx.QueryRow(ctx, `SELECT storage_prefix FROM projects WHERE uid = $1 FOR UPDATE`, projectUID).Scan(&prefix)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrProjectNotFound
	}
	if err != nil {
		return "", fmt.Errorf("locking the project: %w", err)
	}
	err = tx.QueryRow(ctx, `SELECT
		EXISTS (SELECT 1 FROM storage_rekey_runs WHERE project_uid = $1 AND state IN ('running', 'paused')),
		EXISTS (SELECT 1 FROM storage_rekey_runs WHERE project_uid = $1)`, projectUID).Scan(&active, &ever)
	if err != nil {
		return "", fmt.Errorf("reading the project's runs: %w", err)
	}
	switch {
	case active:
		return "", ErrRunActive
	case prefix != "" && (!ever || prefix != target):
		return "", ErrAlreadyPrefixed
	}
	return prefix, nil
}

// switchPrefix sets the project's storage prefix to target and records it in
// the audit log in the same transaction.
func (s *Service) switchPrefix(
	ctx context.Context, tx pgx.Tx, meta audit.Meta, projectUID, current, target, runUID string,
) error {
	if _, err := tx.Exec(ctx, `UPDATE projects SET storage_prefix = $2, updated_at = now() WHERE uid = $1`,
		projectUID, target); err != nil {
		return fmt.Errorf("switching the storage prefix: %w", err)
	}
	entry := meta.Entry(audit.ActionProjectStoragePrefix, project.TargetType, projectUID,
		map[string]any{"old": current, "new": target, "rekey_run": runUID})
	entry.ProjectUID = projectUID
	if err := audit.Write(ctx, tx, entry); err != nil {
		return fmt.Errorf("auditing the storage prefix: %w", err)
	}
	return nil
}

// Pause stops the project's running run; Step refuses it until Resume. The
// pause is audited with meta's actor in the same transaction. It returns
// ErrInvalidState when the latest run is not running and ErrNoRun when there is
// none.
func (s *Service) Pause(ctx context.Context, meta audit.Meta, projectUID string) (Run, error) {
	return s.transition(ctx, meta, projectUID, transitionPause)
}

// Resume puts the project's paused run back to running and schedules the job
// that steps it, audited with meta's actor, all in one transaction. It returns
// ErrInvalidState when the latest run is not paused and ErrNoRun when there is
// none.
func (s *Service) Resume(ctx context.Context, meta audit.Meta, projectUID string) (Run, error) {
	return s.transition(ctx, meta, projectUID, transitionResume)
}

// Retry puts the project's failed run back to running with its cursor reset, so
// the next pass takes again every photo still in the old layout — the ones the
// failed pass recorded errors for — and schedules the job that steps it,
// audited with meta's actor, all in one transaction. Its error count and ring
// are kept. It returns ErrInvalidState when the latest run is not failed and
// ErrNoRun when there is none.
func (s *Service) Retry(ctx context.Context, meta audit.Meta, projectUID string) (Run, error) {
	return s.transition(ctx, meta, projectUID, transitionRetry)
}

// stateChange is one operator transition of a run: the state it starts from,
// the state it ends in, whether it resets the cursor and how it is audited.
type stateChange struct {
	from, to State
	reset    bool
	action   string
}

// The three operator transitions.
var (
	transitionPause  = stateChange{from: StateRunning, to: StatePaused, action: audit.ActionProjectRekeyPause}
	transitionResume = stateChange{from: StatePaused, to: StateRunning, action: audit.ActionProjectRekeyResume}
	transitionRetry  = stateChange{
		from: StateFailed, to: StateRunning, reset: true, action: audit.ActionProjectRekeyRetry,
	}
)

// transition moves the project's latest run along change in one transaction
// that also audits it and, for a run that ends up running, schedules its job.
func (s *Service) transition(ctx context.Context, meta audit.Meta, projectUID string, change stateChange) (Run, error) {
	var run Run
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		run, err = queryRun(ctx, tx, ErrInvalidState, transitionSQL,
			projectUID, string(change.from), string(change.to), change.reset)
		if err != nil {
			return err
		}
		return s.recordAndSchedule(ctx, tx, meta, change.action, run, change.to == StateRunning)
	})
	if err == nil {
		return run, nil
	}
	if !errors.Is(err, ErrInvalidState) {
		return Run{}, fmt.Errorf("rekey: %s of %s: %w", change.action, projectUID, err)
	}
	if _, statusErr := s.Status(ctx, projectUID); statusErr != nil {
		return Run{}, statusErr
	}
	return Run{}, fmt.Errorf("%w: it is not %s", ErrInvalidState, change.from)
}

// recordAndSchedule writes the audit entry of an operator action on run inside
// tx and, with schedule set, enqueues the job that steps the run in the same
// transaction.
func (s *Service) recordAndSchedule(
	ctx context.Context, tx pgx.Tx, meta audit.Meta, action string, run Run, schedule bool,
) error {
	entry := meta.Entry(action, project.TargetType, run.ProjectUID,
		map[string]any{"rekey_run": run.UID, "target_prefix": run.TargetPrefix})
	entry.ProjectUID = run.ProjectUID
	if err := audit.Write(ctx, tx, entry); err != nil {
		return fmt.Errorf("auditing %s: %w", action, err)
	}
	if !schedule {
		return nil
	}
	if err := jobs.EnqueueStorageRekey(ctx, tx, run.ProjectUID); err != nil {
		return fmt.Errorf("scheduling the move: %w", err)
	}
	return nil
}

// Status returns the project's most recent run, or ErrNoRun.
func (s *Service) Status(ctx context.Context, projectUID string) (Run, error) {
	return queryRun(ctx, s.pool, ErrNoRun, latestRunSQL, projectUID)
}

// reload re-reads a run, so a step works from its current state rather than the
// copy the caller holds.
func (s *Service) reload(ctx context.Context, runUID string) (Run, error) {
	return queryRun(ctx, s.pool, ErrNoRun, getRunSQL, runUID)
}
