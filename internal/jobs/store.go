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

// Enqueue inserts a queued job of the given type with the supplied payload and
// options, returning the created row. It is idempotent with respect to the dedup
// key: if an active (queued or running) job already exists for the same
// (type, payload->>'photo_uid') it returns ErrDuplicate without inserting.
func (s *Store) Enqueue(
	ctx context.Context, jobType string, payload json.RawMessage, opts EnqueueOptions,
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
	job, err := scanJob(s.pool.QueryRow(ctx, q,
		jobType, opts.Priority, payloadOrEmpty(payload), maxAttempts, runAfter))
	if err != nil {
		if name, ok := isUniqueViolation(err); ok && name == "idx_jobs_dedup" {
			return Job{}, ErrDuplicate
		}
		return Job{}, err
	}
	return job, nil
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

// Complete marks the running job identified by id done and clears its lock. It
// returns ErrJobNotFound if no running job has that id.
func (s *Store) Complete(ctx context.Context, id int64) error {
	const q = `UPDATE jobs
		SET state = 'done', locked_by = NULL, locked_at = NULL, updated_at = now()
		WHERE id = $1 AND state = 'running'`
	tag, err := s.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("jobs: completing job %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrJobNotFound
	}
	return nil
}

// Fail records a failed attempt on the running job identified by id, storing
// cause as last_error and incrementing attempts. If attempts remain it requeues
// the job with an exponential-backoff run_after; otherwise it dead-letters the
// job (state='dead'). It returns the refreshed job, or ErrJobNotFound if no
// running job has that id.
func (s *Store) Fail(ctx context.Context, id int64, cause error) (Job, error) {
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
		WHERE id = $1 AND state = 'running'
		RETURNING ` + jobColumns
	job, err := scanJob(s.pool.QueryRow(ctx, q,
		id, msg, float64(backoffCapSeconds), float64(backoffBaseSeconds)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, ErrJobNotFound
		}
		return Job{}, err
	}
	return job, nil
}

// Defer requeues the running job identified by id to run after delay WITHOUT
// counting a failed attempt: it returns the job to 'queued', pushes run_after to
// now()+delay (a non-positive delay runs it again immediately), and clears the
// lock, leaving attempts untouched. It is for transient, no-fault conditions —
// chiefly the embeddings box being offline — so a job simply waits in the queue
// for the box to come back without ever exhausting its retry budget. It returns
// the refreshed job, or ErrJobNotFound if no running job has that id.
func (s *Store) Defer(ctx context.Context, id int64, delay time.Duration) (Job, error) {
	const q = `UPDATE jobs SET
			state = 'queued',
			run_after = now() + make_interval(secs => greatest($2::float8, 0)),
			locked_by = NULL,
			locked_at = NULL,
			updated_at = now()
		WHERE id = $1 AND state = 'running'
		RETURNING ` + jobColumns
	job, err := scanJob(s.pool.QueryRow(ctx, q, id, delay.Seconds()))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, ErrJobNotFound
		}
		return Job{}, err
	}
	return job, nil
}

// Heartbeat refreshes the lock timestamp of the running job identified by id and
// owned by workerID, keeping RecoverStaleLocks from reclaiming a job that is
// still being worked. It returns ErrJobNotFound if no such running job exists.
func (s *Store) Heartbeat(ctx context.Context, id int64, workerID string) error {
	const q = `UPDATE jobs SET locked_at = now(), updated_at = now()
		WHERE id = $1 AND state = 'running' AND locked_by = $2`
	tag, err := s.pool.Exec(ctx, q, id, workerID)
	if err != nil {
		return fmt.Errorf("jobs: heartbeating job %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrJobNotFound
	}
	return nil
}

// RecoverStaleLocks requeues running jobs whose lock is older than staleAfter,
// i.e. whose worker is presumed to have died. Each recovery counts as a failed
// attempt: a job with retries left returns to 'queued' runnable immediately,
// otherwise it is dead-lettered. It returns the number of jobs recovered.
func (s *Store) RecoverStaleLocks(ctx context.Context, staleAfter time.Duration) (int64, error) {
	const q = `UPDATE jobs SET
			attempts = attempts + 1,
			state = CASE WHEN attempts + 1 >= max_attempts THEN 'dead' ELSE 'queued' END,
			last_error = CASE
				WHEN attempts + 1 >= max_attempts THEN 'stale lock: worker presumed lost'
				ELSE last_error END,
			locked_by = NULL,
			locked_at = NULL,
			run_after = now(),
			updated_at = now()
		WHERE state = 'running' AND locked_at < now() - make_interval(secs => $1::float8)`
	tag, err := s.pool.Exec(ctx, q, staleAfter.Seconds())
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
