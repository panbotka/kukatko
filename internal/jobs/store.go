package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// uniqueViolation is the PostgreSQL SQLSTATE for a unique-constraint violation.
const uniqueViolation = "23505"

const (
	// DefaultMaxAttempts is the retry cap applied when EnqueueOptions.MaxAttempts
	// is not set; it mirrors the jobs.max_attempts column default.
	DefaultMaxAttempts = 5
	// backoffBaseSeconds is the first retry delay; each further attempt doubles it.
	backoffBaseSeconds = 30
	// backoffCapSeconds caps the exponential backoff so a long-failing job is still
	// retried roughly hourly rather than drifting arbitrarily far into the future.
	backoffCapSeconds = 3600
	// defaultDeadListLimit bounds ListDead when the caller passes a non-positive
	// limit.
	defaultDeadListLimit = 100
	// defaultListLimit is the page size List uses when the caller passes a
	// non-positive limit.
	defaultListLimit = 100
	// maxListLimit caps List's page size so an admin request cannot ask for an
	// unbounded result set.
	maxListLimit = 500
)

// jobColumns is the canonical, ordered column list for job reads (and for INSERT
// … RETURNING), matched position-for-position by scanJob.
const jobColumns = "id, type, state, priority, payload, attempts, max_attempts, " +
	"last_error, run_after, locked_by, locked_at, created_at, updated_at"

// Store is the database access layer for the persistent job queue. It owns no
// connection; it borrows the shared pgx pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation and, if so, the name of the violated constraint.
func isUniqueViolation(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return pgErr.ConstraintName, true
	}
	return "", false
}

// scanJob reads one job row in jobColumns order from a pgx.Row (a single-row
// QueryRow result or a row during iteration), returning a wrapped error on
// failure.
func scanJob(row pgx.Row) (Job, error) {
	var j Job
	var payload []byte
	if err := row.Scan(
		&j.ID, &j.Type, &j.State, &j.Priority, &payload, &j.Attempts, &j.MaxAttempts,
		&j.LastError, &j.RunAfter, &j.LockedBy, &j.LockedAt, &j.CreatedAt, &j.UpdatedAt,
	); err != nil {
		return Job{}, fmt.Errorf("jobs: scanning job: %w", err)
	}
	j.Payload = payload
	return j, nil
}

// payloadOrEmpty returns the canonical empty JSON object for an absent payload so
// the NOT NULL jsonb column always holds a valid document, and the payload itself
// otherwise.
func payloadOrEmpty(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte("{}")
	}
	return raw
}

// Execer is the subset of pgx one enqueue statement needs. Both *pgxpool.Pool
// and pgx.Tx satisfy it, so a job can be scheduled on its own connection or join
// a caller's transaction — the same convention audit.Write follows.
type Execer interface {
	// QueryRow runs sql with args and returns the single row it produced.
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Enqueue inserts a queued job of the given type with the supplied payload and
// options using exec, which may be a pool or an open transaction, and returns the
// created row.
//
// Passing the caller's pgx.Tx makes the job part of that transaction: a mutation
// that rolls back schedules no work, and one that commits schedules it exactly
// once. That is what lets a registration enqueue its confirmation mail inside the
// same transaction that creates the account, the way the audit trail is written.
//
// It is idempotent with respect to the dedup key: if an active (queued or
// running) job already exists for the same (type, payload->>'photo_uid') it
// returns ErrDuplicate without inserting. A payload without a photo_uid — a mail,
// a backup — never dedupes, because NULLs are distinct in a unique index.
func Enqueue(
	ctx context.Context, exec Execer, jobType string, payload json.RawMessage, opts EnqueueOptions,
) (Job, error) {
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = DefaultMaxAttempts
	}
	runAfter := time.Now()
	if opts.RunAfter != nil {
		runAfter = *opts.RunAfter
	}
	const q = `INSERT INTO jobs (type, state, priority, payload, max_attempts, run_after)
		VALUES ($1, 'queued', $2, $3, $4, $5)
		RETURNING ` + jobColumns
	job, err := scanJob(exec.QueryRow(ctx, q,
		jobType, opts.Priority, payloadOrEmpty(payload), maxAttempts, runAfter))
	if err != nil {
		if name, ok := isUniqueViolation(err); ok && name == "idx_jobs_dedup" {
			return Job{}, ErrDuplicate
		}
		return Job{}, err
	}
	return job, nil
}

// Enqueue inserts a queued job of the given type on the store's own pool. It is
// the package-level Enqueue with the pool as the executor; a caller that wants
// the job to commit with its mutation calls that one with its transaction
// instead.
func (s *Store) Enqueue(
	ctx context.Context, jobType string, payload json.RawMessage, opts EnqueueOptions,
) (Job, error) {
	return Enqueue(ctx, s.pool, jobType, payload, opts)
}

// upgradeToForcedSQL rewrites the payload of the *queued plain* job covering one
// photo to a forced one, and reports what it found either way. It is one
// statement on purpose: the upgrade has to be conditional on the job still being
// queued at the moment it is written, so a worker claiming that job concurrently
// can neither lose the forced flag nor have it applied to a run already in
// flight. A read followed by a write could do both.
//
// The `active` CTE describes the collision as it stood when the statement began,
// the `upgraded` CTE rewrites it — repeating `state = 'queued'` in its own WHERE,
// which is what PostgreSQL re-checks against the row's committed version if a
// concurrent claim beat it there — and the final SELECT reports both halves. A
// running job wins the report, because it is the answer that cannot be upgraded.
const upgradeToForcedSQL = `WITH active AS (
		SELECT id, state, coalesce((payload ->> 'force')::boolean, false) AS forced
		FROM jobs
		WHERE type = $1 AND payload ->> 'photo_uid' = $2 AND state IN ('queued', 'running')
	), upgraded AS (
		UPDATE jobs SET payload = $3, updated_at = now()
		WHERE id IN (SELECT id FROM active WHERE state = 'queued' AND NOT forced)
		  AND state = 'queued'
		RETURNING id
	)
	SELECT active.state, active.forced, EXISTS (SELECT 1 FROM upgraded)
	FROM active ORDER BY (active.state = 'running') DESC LIMIT 1`

// UpgradeToForced makes the active job for (jobType, photoUID) carry payload —
// the forced payload — instead of the plain one it was enqueued with, and returns
// what the collision turned out to be: ForceUpgraded when a queued plain job was
// rewritten, ForceAbsorbed when the job was already forced (nothing to do), or
// ForceInFlight when it is running and therefore beyond reach. It returns
// ErrNoActiveJob when the job finished in the meantime and there is nothing to
// upgrade.
//
// It is what makes a forced enqueue that hits the dedup index mean something: the
// queue keeps at most one active job per photo per type, so without the upgrade
// the forced payload would simply be dropped and the plain job would take its
// idempotent skip. Nothing is inserted here — the row count for that photo and
// type is unchanged.
func (s *Store) UpgradeToForced(
	ctx context.Context, jobType, photoUID string, payload json.RawMessage,
) (ForceOutcome, error) {
	var state State
	var forced, upgraded bool
	err := s.pool.QueryRow(ctx, upgradeToForcedSQL, jobType, photoUID, payloadOrEmpty(payload)).
		Scan(&state, &forced, &upgraded)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNoActiveJob
		}
		return "", fmt.Errorf("jobs: upgrading the queued %s job for %s: %w", jobType, photoUID, err)
	}
	switch {
	case upgraded:
		return ForceUpgraded, nil
	case forced && state == StateQueued:
		return ForceAbsorbed, nil
	default:
		// Either the job was already running, or it was queued and plain when the
		// statement began and a worker claimed it before the UPDATE could take its
		// lock — in which case that run is using the old payload, which is exactly
		// what ForceInFlight says.
		return ForceInFlight, nil
	}
}

// claimSQL builds the atomic claim statement. When filterTypes is true the
// candidate subquery is restricted to the types passed as $2 (a text array).
func claimSQL(filterTypes bool) string {
	typeFilter := ""
	if filterTypes {
		typeFilter = "AND type = ANY($2) "
	}
	return `UPDATE jobs
		SET state = 'running', locked_by = $1, locked_at = now(), updated_at = now()
		WHERE id = (
			SELECT id FROM jobs
			WHERE state = 'queued' AND run_after <= now() ` + typeFilter + `
			ORDER BY priority DESC, run_after ASC, id ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING ` + jobColumns
}

// Claim atomically picks the next runnable job — the highest-priority, earliest
// due, oldest queued row whose run_after has passed — marks it running under
// workerID, and returns it. Concurrent claimers never receive the same row
// (SELECT … FOR UPDATE SKIP LOCKED). If types are given, only those job types are
// considered. It returns ErrNoJobs when nothing is runnable.
func (s *Store) Claim(ctx context.Context, workerID string, types ...string) (Job, error) {
	query := claimSQL(len(types) > 0)
	var row pgx.Row
	if len(types) > 0 {
		row = s.pool.QueryRow(ctx, query, workerID, types)
	} else {
		row = s.pool.QueryRow(ctx, query, workerID)
	}
	job, err := scanJob(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, ErrNoJobs
		}
		return Job{}, err
	}
	return job, nil
}

// Complete marks the running job identified by id and owned by workerID done and
// clears its lock. It returns ErrLockLost if the job was meanwhile reclaimed by
// another worker (so this late result must be dropped), or ErrJobNotFound if no
// job has that id.
func (s *Store) Complete(ctx context.Context, id int64, workerID string) error {
	const q = `UPDATE jobs
		SET state = 'done', locked_by = NULL, locked_at = NULL, updated_at = now()
		WHERE id = $1 AND state = 'running' AND locked_by = $2`
	tag, err := s.pool.Exec(ctx, q, id, workerID)
	if err != nil {
		return fmt.Errorf("jobs: completing job %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return s.ownershipMissReason(ctx, id, workerID)
	}
	return nil
}

// ownershipMissReason explains why a lifecycle update guarded by locked_by
// matched no row: the job is gone (ErrJobNotFound) or it exists but is no longer
// running under workerID because it was reclaimed (ErrLockLost).
func (s *Store) ownershipMissReason(ctx context.Context, id int64, workerID string) error {
	job, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if job.State == StateRunning && job.LockedBy != nil && *job.LockedBy == workerID {
		// Should not happen: the guard matched but the update did not. Report it
		// as a miss rather than claiming a successful write.
		return ErrJobNotFound
	}
	return ErrLockLost
}

// Fail records a failed attempt on the running job identified by id and owned by
// workerID, storing cause as last_error and incrementing attempts. If attempts
// remain it requeues the job with an exponential-backoff run_after; otherwise it
// dead-letters the job (state='dead'). It returns the refreshed job, ErrLockLost
// if the job was meanwhile reclaimed by another worker, or ErrJobNotFound if no
// job has that id.
func (s *Store) Fail(ctx context.Context, id int64, workerID string, cause error) (Job, error) {
	msg := "unknown error"
	if cause != nil {
		msg = cause.Error()
	}
	const q = `UPDATE jobs SET
			attempts = attempts + 1,
			last_error = $2,
			state = CASE WHEN attempts + 1 >= max_attempts THEN 'dead' ELSE 'queued' END,
			run_after = CASE
				WHEN attempts + 1 >= max_attempts THEN run_after
				ELSE now() + make_interval(
					secs => least($3::float8, $4::float8 * power(2, attempts)::float8))
			END,
			locked_by = NULL,
			locked_at = NULL,
			updated_at = now()
		WHERE id = $1 AND state = 'running' AND locked_by = $5
		RETURNING ` + jobColumns
	job, err := scanJob(s.pool.QueryRow(ctx, q,
		id, msg, float64(backoffCapSeconds), float64(backoffBaseSeconds), workerID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, s.ownershipMissReason(ctx, id, workerID)
		}
		return Job{}, err
	}
	return job, nil
}

// FailTerminal records a permanent failure on the running job identified by id
// and owned by workerID: it stores cause as last_error, counts the attempt and
// parks the job in StateFailed, where nothing will claim it again. Unlike Fail it
// never requeues, whatever the remaining attempt budget — it is for work that
// cannot succeed on a later try, such as a mail whose recipient the server
// rejected as permanently undeliverable. The job stays requeueable by hand
// (Requeue accepts StateFailed), which is how an operator retries one after
// fixing whatever made it impossible.
//
// It returns the refreshed job, ErrLockLost if the job was meanwhile reclaimed by
// another worker, or ErrJobNotFound if no job has that id.
func (s *Store) FailTerminal(ctx context.Context, id int64, workerID string, cause error) (Job, error) {
	msg := "unknown error"
	if cause != nil {
		msg = cause.Error()
	}
	const q = `UPDATE jobs SET
			attempts = attempts + 1,
			last_error = $2,
			state = 'failed',
			locked_by = NULL,
			locked_at = NULL,
			updated_at = now()
		WHERE id = $1 AND state = 'running' AND locked_by = $3
		RETURNING ` + jobColumns
	job, err := scanJob(s.pool.QueryRow(ctx, q, id, msg, workerID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, s.ownershipMissReason(ctx, id, workerID)
		}
		return Job{}, err
	}
	return job, nil
}

// Defer requeues the running job identified by id and owned by workerID to run
// after delay WITHOUT counting a failed attempt: it returns the job to 'queued',
// pushes run_after to now()+delay (a non-positive delay runs it again
// immediately), and clears the lock, leaving attempts untouched. It is for
// transient, no-fault conditions — chiefly the embeddings box being offline — so
// a job simply waits in the queue for the box to come back without ever
// exhausting its retry budget. It returns the refreshed job, ErrLockLost if the
// job was meanwhile reclaimed by another worker, or ErrJobNotFound if no job has
// that id.
func (s *Store) Defer(ctx context.Context, id int64, workerID string, delay time.Duration) (Job, error) {
	const q = `UPDATE jobs SET
			state = 'queued',
			run_after = now() + make_interval(secs => greatest($2::float8, 0)),
			locked_by = NULL,
			locked_at = NULL,
			updated_at = now()
		WHERE id = $1 AND state = 'running' AND locked_by = $3
		RETURNING ` + jobColumns
	job, err := scanJob(s.pool.QueryRow(ctx, q, id, delay.Seconds(), workerID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, s.ownershipMissReason(ctx, id, workerID)
		}
		return Job{}, err
	}
	return job, nil
}

// Heartbeat refreshes the lock timestamp of the running job identified by id and
// owned by workerID, keeping RecoverStaleLocks from reclaiming a job that is
// still being worked. The worker calls it on a ticker for as long as a handler
// runs, so a job that legitimately takes longer than the stale window (a full
// import pass, say) is not recovered and run twice. It returns ErrLockLost if
// the job is no longer running under workerID, or ErrJobNotFound if no job has
// that id.
func (s *Store) Heartbeat(ctx context.Context, id int64, workerID string) error {
	const q = `UPDATE jobs SET locked_at = now(), updated_at = now()
		WHERE id = $1 AND state = 'running' AND locked_by = $2`
	tag, err := s.pool.Exec(ctx, q, id, workerID)
	if err != nil {
		return fmt.Errorf("jobs: heartbeating job %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return s.ownershipMissReason(ctx, id, workerID)
	}
	return nil
}

// RecoverStaleLocks requeues running jobs whose lock is older than staleAfter,
// i.e. whose worker is presumed to have died. Each recovery counts as a failed
// attempt: a job with retries left returns to 'queued' with the same
// exponential-backoff delay Fail applies, otherwise it is dead-lettered. The
// backoff matters because a job that kills its process (an OOM on a huge
// original, say) would otherwise be re-claimed instantly and crash again in a
// tight loop. It returns the number of jobs recovered.
func (s *Store) RecoverStaleLocks(ctx context.Context, staleAfter time.Duration) (int64, error) {
	const q = `UPDATE jobs SET
			attempts = attempts + 1,
			state = CASE WHEN attempts + 1 >= max_attempts THEN 'dead' ELSE 'queued' END,
			last_error = CASE
				WHEN attempts + 1 >= max_attempts THEN 'stale lock: worker presumed lost'
				ELSE last_error END,
			locked_by = NULL,
			locked_at = NULL,
			run_after = CASE
				WHEN attempts + 1 >= max_attempts THEN run_after
				ELSE now() + make_interval(
					secs => least($2::float8, $3::float8 * power(2, attempts)::float8))
			END,
			updated_at = now()
		WHERE state = 'running' AND locked_at < now() - make_interval(secs => $1::float8)`
	tag, err := s.pool.Exec(ctx, q, staleAfter.Seconds(),
		float64(backoffCapSeconds), float64(backoffBaseSeconds))
	if err != nil {
		return 0, fmt.Errorf("jobs: recovering stale locks: %w", err)
	}
	return tag.RowsAffected(), nil
}

// Get returns the job with the given id, or ErrJobNotFound.
func (s *Store) Get(ctx context.Context, id int64) (Job, error) {
	const q = "SELECT " + jobColumns + " FROM jobs WHERE id = $1"
	job, err := scanJob(s.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, ErrJobNotFound
		}
		return Job{}, err
	}
	return job, nil
}

// groupCount returns the per-value row counts grouped by the given trusted column
// name (an internal constant, never user input).
func (s *Store) groupCount(ctx context.Context, column string) (map[string]int, error) {
	q := "SELECT " + column + ", count(*) FROM jobs GROUP BY " + column
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("jobs: counting by %s: %w", column, err)
	}
	defer rows.Close()
	counts := make(map[string]int)
	for rows.Next() {
		var key string
		var n int
		if err := rows.Scan(&key, &n); err != nil {
			return nil, fmt.Errorf("jobs: scanning %s count: %w", column, err)
		}
		counts[key] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("jobs: iterating %s counts: %w", column, err)
	}
	return counts, nil
}

// CountsByState returns the number of jobs in each lifecycle state. States with
// no jobs are absent from the map.
func (s *Store) CountsByState(ctx context.Context) (map[State]int, error) {
	raw, err := s.groupCount(ctx, "state")
	if err != nil {
		return nil, err
	}
	counts := make(map[State]int, len(raw))
	for key, n := range raw {
		counts[State(key)] = n
	}
	return counts, nil
}

// CountsByType returns the number of jobs of each type. Types with no jobs are
// absent from the map.
func (s *Store) CountsByType(ctx context.Context) (map[string]int, error) {
	return s.groupCount(ctx, "type")
}

// TypeState is one cell of the queue breakdown: a job type paired with a
// lifecycle state. It is a comparable struct so CountsByTypeState can key a map
// by it without stringly-typed concatenation.
type TypeState struct {
	// Type is the job type ("image_embed", "thumbnail", ...).
	Type string
	// State is the lifecycle state ("queued", "running", ...).
	State State
}

// CountsByTypeState returns the number of jobs per (type, state) pair. Pairs
// with no jobs are absent from the map, so a caller wanting a dense matrix fills
// the gaps itself.
//
// It is the single query behind the /metrics queue gauges: the per-state and
// per-type totals are sums over this breakdown, so one scan of the jobs table
// answers all three rather than the two separate GROUP BYs a scrape used to run.
func (s *Store) CountsByTypeState(ctx context.Context) (map[TypeState]int, error) {
	const q = "SELECT type, state, count(*) FROM jobs GROUP BY type, state"
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("jobs: counting by type and state: %w", err)
	}
	defer rows.Close()
	counts := make(map[TypeState]int)
	for rows.Next() {
		var key TypeState
		var n int
		if err := rows.Scan(&key.Type, &key.State, &n); err != nil {
			return nil, fmt.Errorf("jobs: scanning type/state count: %w", err)
		}
		counts[key] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("jobs: iterating type/state counts: %w", err)
	}
	return counts, nil
}

// CountPending returns how many jobs of the given types are still pending, that
// is queued or running (not yet in a terminal state). With no types it returns
// 0. It backs the optional Wake-on-LAN auto-wake, which only sends a magic
// packet when embedding work is actually waiting on the box.
func (s *Store) CountPending(ctx context.Context, types ...string) (int, error) {
	if len(types) == 0 {
		return 0, nil
	}
	const q = "SELECT count(*) FROM jobs WHERE state IN ('queued', 'running') AND type = ANY($1)"
	var n int
	if err := s.pool.QueryRow(ctx, q, types).Scan(&n); err != nil {
		return 0, fmt.Errorf("jobs: counting pending jobs: %w", err)
	}
	return n, nil
}

// unfinishedForPhotoSQL selects, per job type, the newest job for one photo that
// has not completed — the row that says whether the work is waiting, running or
// broken. Done jobs are excluded because completed work is read from its own
// evidence, not from the queue; and only the newest row per type is kept, so a
// job re-enqueued after an earlier one was dead-lettered speaks for its type
// rather than the corpse behind it.
const unfinishedForPhotoSQL = "SELECT DISTINCT ON (type) " + jobColumns + " FROM jobs " +
	"WHERE payload ->> 'photo_uid' = $1 AND state <> 'done' ORDER BY type, id DESC"

// UnfinishedForPhoto returns the newest unfinished job per type for the photo
// identified by photoUID — at most one row per job type, in type order. It backs
// the per-photo processing report, which needs the queue's side of the story in
// a single round trip rather than one query per step.
func (s *Store) UnfinishedForPhoto(ctx context.Context, photoUID string) ([]Job, error) {
	rows, err := s.pool.Query(ctx, unfinishedForPhotoSQL, photoUID)
	if err != nil {
		return nil, fmt.Errorf("jobs: listing unfinished jobs for %s: %w", photoUID, err)
	}
	defer rows.Close()
	list := make([]Job, 0, len(photoStepTypes))
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("jobs: iterating unfinished jobs for %s: %w", photoUID, err)
	}
	return list, nil
}

// photoStepTypes is only a capacity hint for UnfinishedForPhoto: the per-photo
// job types, so the usual result fits without a reallocation.
var photoStepTypes = []string{
	TypeMetadata, TypeThumbnail, TypeImageEmbed, TypeFaceDetect,
	TypeOCR, TypePlaces, TypeSidecar, TypeStoryboard,
}

// ListDead returns dead-lettered jobs, most recently updated first, for the admin
// dead-letter view. A non-positive limit defaults to defaultDeadListLimit.
func (s *Store) ListDead(ctx context.Context, limit, offset int) ([]Job, error) {
	if limit <= 0 {
		limit = defaultDeadListLimit
	}
	const q = "SELECT " + jobColumns + " FROM jobs WHERE state = 'dead' " +
		"ORDER BY updated_at DESC, id DESC LIMIT $1 OFFSET $2"
	rows, err := s.pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("jobs: listing dead jobs: %w", err)
	}
	defer rows.Close()
	dead := make([]Job, 0, limit)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		dead = append(dead, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("jobs: iterating dead jobs: %w", err)
	}
	return dead, nil
}

// RequeueDead resets a dead-lettered job back to 'queued' with a fresh attempt
// budget, runnable immediately, and returns the refreshed job. It returns
// ErrJobNotFound if no job has that id, or ErrNotDead if the job is not dead.
func (s *Store) RequeueDead(ctx context.Context, id int64) (Job, error) {
	return s.requeueInStates(ctx, id, []string{string(StateDead)})
}

// Requeue resets a dead-lettered or terminally failed job back to 'queued' with
// a fresh attempt budget, runnable immediately, and returns the refreshed job.
// It backs the admin requeue endpoint, which may target either a dead-letter or
// a failed job. It returns ErrJobNotFound if no job has that id, or ErrNotDead
// if the job is in neither a dead nor a failed state.
func (s *Store) Requeue(ctx context.Context, id int64) (Job, error) {
	return s.requeueInStates(ctx, id, []string{string(StateDead), string(StateFailed)})
}

// RequeueAllDead resets every dead-lettered job back to 'queued' with a fresh
// attempt budget, runnable immediately, and returns how many were requeued. With
// no types it requeues the whole dead letter; with types it requeues only those
// job types, which is what lets an operator retry the one thing that broke (the
// OCR jobs that died while the box was down) without also retrying everything
// else that has ever been given up on.
//
// It is one UPDATE rather than a listing followed by a requeue per row: a
// dead letter of thousands is exactly the case this exists for, and it must not
// turn into thousands of round trips.
func (s *Store) RequeueAllDead(ctx context.Context, types ...string) (int, error) {
	const q = `UPDATE jobs SET
			state = 'queued', attempts = 0, last_error = '', run_after = now(),
			locked_by = NULL, locked_at = NULL, updated_at = now()
		WHERE state = 'dead' AND (cardinality($1::text[]) = 0 OR type = ANY($1))`
	if types == nil {
		types = []string{}
	}
	tag, err := s.pool.Exec(ctx, q, types)
	if err != nil {
		return 0, fmt.Errorf("jobs: requeuing dead jobs: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// requeueInStates resets the job identified by id to a fresh 'queued' state when
// its current state is one of states, returning the refreshed job. It returns
// ErrJobNotFound if no job has that id, or ErrNotDead if the job is in some
// other state.
func (s *Store) requeueInStates(ctx context.Context, id int64, states []string) (Job, error) {
	const q = `UPDATE jobs SET
			state = 'queued', attempts = 0, last_error = '', run_after = now(),
			locked_by = NULL, locked_at = NULL, updated_at = now()
		WHERE id = $1 AND state = ANY($2)
		RETURNING ` + jobColumns
	job, err := scanJob(s.pool.QueryRow(ctx, q, id, states))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, s.requeueMissReason(ctx, id)
		}
		return Job{}, err
	}
	return job, nil
}

// requeueMissReason explains why a requeue update matched no row: the job is
// missing (ErrJobNotFound) or exists but is not in a requeueable state
// (ErrNotDead).
func (s *Store) requeueMissReason(ctx context.Context, id int64) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	return ErrNotDead
}

// ListOptions filters and paginates Store.List. The zero value lists the most
// recently updated jobs across all states up to defaultListLimit.
type ListOptions struct {
	// State, when non-nil, restricts the result to jobs in that lifecycle state.
	State *State
	// Limit caps the page size; a non-positive value uses defaultListLimit and
	// any value above maxListLimit is clamped to it.
	Limit int
	// Offset skips the given number of leading rows for pagination.
	Offset int
}

// List returns a page of jobs ordered most-recently-updated first (id breaks
// ties), optionally restricted to a single state. It backs the admin job
// browser and dead-letter view.
func (s *Store) List(ctx context.Context, opts ListOptions) ([]Job, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	args := []any{limit, opts.Offset}
	where := ""
	if opts.State != nil {
		where = "WHERE state = $3 "
		args = append(args, string(*opts.State))
	}
	q := "SELECT " + jobColumns + " FROM jobs " + where +
		"ORDER BY updated_at DESC, id DESC LIMIT $1 OFFSET $2"
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("jobs: listing jobs: %w", err)
	}
	defer rows.Close()
	list := make([]Job, 0, limit)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("jobs: iterating jobs: %w", err)
	}
	return list, nil
}
