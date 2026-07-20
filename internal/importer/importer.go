// Package importer records the history of import and migration runs. Each run of
// the PhotoPrism import or the photo-sorter migration is tracked in the
// import_runs table together with a high-watermark — the largest source
// timestamp processed — so the next run can resume incrementally from where the
// last successful run left off (see ARCHITECTURE.md §5.2, §9, §10).
//
// A run progresses through a small lifecycle: Start opens a row in the running
// state, UpdateCounts records progress, and Complete or Fail closes it. Only a
// completed run advances the cursor returned by LatestWatermark, so a crashed or
// failed run leaves the watermark untouched and the work is simply retried.
package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Source identifies where an import run pulled its data from. The values mirror
// the source column's CHECK constraint in migration 0013_import_runs.sql.
type Source string

const (
	// SourcePhotoPrism is the read-only, repeatable import from a running
	// PhotoPrism instance.
	SourcePhotoPrism Source = "photoprism"
	// SourcePhotoSorter is the one-off (optionally repeatable) migration from the
	// photo-sorter database.
	SourcePhotoSorter Source = "photosorter"
	// SourcePhotoSorterFeeds is the read-only enrichment of PhotoPrism-imported
	// photos with photo-sorter's pre-computed embeddings and faces, copied 1:1 from
	// its HTTP migration feeds (internal/psfeedsimport). It is distinct from
	// SourcePhotoSorter (the direct-database photo migration) so their run history
	// and watermarks stay separate.
	SourcePhotoSorterFeeds Source = "photosorter_feeds"
	// SourceFolder is a `kukatko import dir` run: a directory of originals
	// ingested from disk. It has no source timestamp to resume from and so never
	// records a high-watermark; re-running is made safe by the SHA256 dedup.
	SourceFolder Source = "folder"
)

// Valid reports whether s is a recognised import source.
func (s Source) Valid() bool {
	return s == SourcePhotoPrism || s == SourcePhotoSorter ||
		s == SourcePhotoSorterFeeds || s == SourceFolder
}

// Status is the lifecycle state of an import run. The values mirror the status
// column's CHECK constraint.
type Status string

const (
	// StatusRunning marks a run that has started but not yet finished.
	StatusRunning Status = "running"
	// StatusDone marks a run that finished successfully; its watermark is
	// eligible to resume the next incremental run.
	StatusDone Status = "done"
	// StatusPartial marks a run that finished its scan but recorded at least one
	// unresolved per-photo or per-file failure (see import_failures). Like a failed
	// run its watermark is ignored (LatestWatermark reads only 'done' runs), so a
	// re-run retries the same window; unlike a failed run it did complete its pass,
	// so the aggregate counts are final and the individual failures are listable.
	StatusPartial Status = "partial"
	// StatusFailed marks a run that aborted with an error; its watermark is
	// ignored so the next run retries the same window.
	StatusFailed Status = "failed"
)

// Counts is the running tally of an import, serialised to the counts JSONB
// column. Each field counts photos handled in one way during the run.
type Counts struct {
	// Imported is the number of new photos created.
	Imported int `json:"imported"`
	// Updated is the number of existing photos whose metadata changed.
	Updated int `json:"updated"`
	// Skipped is the number of photos already up to date (no change needed).
	Skipped int `json:"skipped"`
	// Failed is the number of photos that errored without aborting the run.
	Failed int `json:"failed"`
}

// ProgressObserver receives an import run's latest checkpointed photo tally so
// it can be exported as metrics. It is satisfied by *metrics.Registry; the
// import services call it after every page checkpoint. Implementations must be
// safe for concurrent use. source is the import source ("photoprism" or
// "photosorter").
type ProgressObserver interface {
	// SetImportProgress publishes the latest tally for source.
	SetImportProgress(source string, imported, updated, skipped, failed int)
}

// NopProgressObserver is a ProgressObserver whose methods do nothing; the
// import services fall back to it when no observer is configured.
type NopProgressObserver struct{}

// SetImportProgress does nothing.
func (NopProgressObserver) SetImportProgress(string, int, int, int, int) {}

// Run is one row of import_runs: a single import or migration run with its
// lifecycle state, watermark, and tallies. FinishedAt and HighWatermark are nil
// while the run is in progress or when no watermark was produced.
type Run struct {
	ID            int64      `json:"id"`
	Source        Source     `json:"source"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at"`
	Status        Status     `json:"status"`
	HighWatermark *time.Time `json:"high_watermark"`
	Counts        Counts     `json:"counts"`
	LastError     string     `json:"last_error"`
}

// Sentinel errors returned by the store so callers (and tests) can branch on the
// cause with errors.Is.
var (
	// ErrRunNotFound is returned when no import run matches the given id.
	ErrRunNotFound = errors.New("importer: run not found")
	// ErrInvalidSource is returned when an unrecognised source is supplied.
	ErrInvalidSource = errors.New("importer: invalid source")
)

// Store persists import runs on a shared pgx pool.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// runColumns is the column list shared by every SELECT so the scan order stays
// in one place.
const runColumns = `id, source, started_at, finished_at, status, high_watermark, counts, last_error`

// Paging limits applied by List so a caller can never request an unbounded page.
const (
	// defaultListLimit is the page size used when the caller passes a
	// non-positive limit.
	defaultListLimit = 50
	// maxListLimit caps the page size so a single query can never read an
	// unbounded number of rows.
	maxListLimit = 200
)

// startSQL inserts a new running run and returns the full row.
const startSQL = `
INSERT INTO import_runs (source, status, counts)
VALUES ($1, 'running', '{}'::jsonb)
RETURNING ` + runColumns

// Start opens a new run for source in the running state and returns it with its
// assigned id and started_at. It returns ErrInvalidSource if source is not a
// recognised import source.
func (s *Store) Start(ctx context.Context, source Source) (Run, error) {
	if !source.Valid() {
		return Run{}, fmt.Errorf("%w: %q", ErrInvalidSource, source)
	}
	row := s.pool.QueryRow(ctx, startSQL, string(source))
	run, err := scanRun(row)
	if err != nil {
		return Run{}, fmt.Errorf("importer: starting run: %w", err)
	}
	return run, nil
}

// updateCountsSQL overwrites the counts of an in-progress run.
const updateCountsSQL = `UPDATE import_runs SET counts = $2 WHERE id = $1`

// UpdateCounts replaces the counts tally of the run identified by id. It returns
// ErrRunNotFound if no such run exists.
func (s *Store) UpdateCounts(ctx context.Context, id int64, counts Counts) error {
	encoded, err := json.Marshal(counts)
	if err != nil {
		return fmt.Errorf("importer: encoding counts: %w", err)
	}
	tag, err := s.pool.Exec(ctx, updateCountsSQL, id, encoded)
	if err != nil {
		return fmt.Errorf("importer: updating counts for run %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %d", ErrRunNotFound, id)
	}
	return nil
}

// finishSQL closes a run, stamping finished_at and the final status, watermark,
// counts, and error. It only matches a run still in the running state so a run is
// never finished twice.
const finishSQL = `
UPDATE import_runs
SET status = $2, finished_at = now(), high_watermark = $3, counts = $4, last_error = $5
WHERE id = $1 AND status = 'running'`

// finish closes the run identified by id with the given terminal status and
// fields. It is the shared body of Complete and Fail.
func (s *Store) finish(
	ctx context.Context, id int64, status Status,
	watermark *time.Time, counts Counts, lastErr string,
) error {
	encoded, err := json.Marshal(counts)
	if err != nil {
		return fmt.Errorf("importer: encoding counts: %w", err)
	}
	tag, err := s.pool.Exec(ctx, finishSQL, id, string(status), watermark, encoded, lastErr)
	if err != nil {
		return fmt.Errorf("importer: finishing run %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %d (not running)", ErrRunNotFound, id)
	}
	return nil
}

// Complete closes the run identified by id, recording its final counts and the
// high-watermark to resume the next incremental run from. A nil watermark stores
// SQL NULL (the run produced no new cursor). The terminal status is chosen from
// the run's persisted failures: 'partial' when the run recorded at least one
// unresolved import_failures row, otherwise 'done'. Persist failures with
// RecordFailures before calling Complete so they are counted. It returns
// ErrRunNotFound if the run does not exist or is no longer running.
func (s *Store) Complete(ctx context.Context, id int64, watermark *time.Time, counts Counts) error {
	unresolved, err := s.CountUnresolvedFailures(ctx, id)
	if err != nil {
		return err
	}
	status := StatusDone
	if unresolved > 0 {
		status = StatusPartial
	}
	return s.finish(ctx, id, status, watermark, counts, "")
}

// Fail marks the run identified by id as failed, recording lastErr and the final
// counts. No watermark is stored, so the next run retries the same window. It
// returns ErrRunNotFound if the run does not exist or is no longer running.
func (s *Store) Fail(ctx context.Context, id int64, lastErr string, counts Counts) error {
	return s.finish(ctx, id, StatusFailed, nil, counts, lastErr)
}

// getSQL reads a single run by id.
const getSQL = `SELECT ` + runColumns + ` FROM import_runs WHERE id = $1`

// Get returns the run identified by id, or ErrRunNotFound if none exists.
func (s *Store) Get(ctx context.Context, id int64) (Run, error) {
	row := s.pool.QueryRow(ctx, getSQL, id)
	run, err := scanRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, fmt.Errorf("%w: %d", ErrRunNotFound, id)
	}
	if err != nil {
		return Run{}, fmt.Errorf("importer: getting run %d: %w", id, err)
	}
	return run, nil
}

// listSQL reads a page of runs across all sources, most recently started first.
// The id tiebreaker keeps the order stable when two runs share a started_at.
const listSQL = `
SELECT ` + runColumns + `
FROM import_runs
ORDER BY started_at DESC, id DESC
LIMIT $1 OFFSET $2`

// List returns a page of import runs across every source, ordered most recently
// started first, for the admin history view. limit is clamped to
// [1, maxListLimit] (a non-positive limit defaults to defaultListLimit) and a
// negative offset is treated as zero, so the result set is always bounded. An
// empty history yields a non-nil, empty slice.
func (s *Store) List(ctx context.Context, limit, offset int) ([]Run, error) {
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.pool.Query(ctx, listSQL, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("importer: listing runs: %w", err)
	}
	defer rows.Close()

	runs := make([]Run, 0, limit)
	for rows.Next() {
		run, scanErr := scanRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("importer: iterating runs: %w", err)
	}
	return runs, nil
}

// latestWatermarkSQL reads the watermark of the most recent successful run for a
// source. Only done runs that produced a watermark are considered, so running and
// failed runs never advance the cursor.
const latestWatermarkSQL = `
SELECT high_watermark
FROM import_runs
WHERE source = $1 AND status = 'done' AND high_watermark IS NOT NULL
ORDER BY finished_at DESC
LIMIT 1`

// LatestWatermark returns the high-watermark of the most recent successful run
// for source, which the next incremental run should resume from. The boolean is
// false when no successful run with a watermark exists yet (a first, full run).
// It returns ErrInvalidSource if source is not recognised.
func (s *Store) LatestWatermark(ctx context.Context, source Source) (time.Time, bool, error) {
	if !source.Valid() {
		return time.Time{}, false, fmt.Errorf("%w: %q", ErrInvalidSource, source)
	}
	var watermark time.Time
	err := s.pool.QueryRow(ctx, latestWatermarkSQL, string(source)).Scan(&watermark)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("importer: latest watermark for %s: %w", source, err)
	}
	return watermark, true, nil
}

// latestRunSQL reads the most recently started run for a source regardless of
// its status, so the admin dashboard can show the last run (running, done or
// failed) of each source. The id tiebreaker keeps the order stable when two runs
// share a started_at.
const latestRunSQL = `
SELECT ` + runColumns + `
FROM import_runs
WHERE source = $1
ORDER BY started_at DESC, id DESC
LIMIT 1`

// LatestRun returns the most recently started run for source, whatever its
// status. The boolean is false when the source has never run. It returns
// ErrInvalidSource if source is not recognised. Unlike LatestWatermark it does
// not filter on status, so a running or failed run is reported too.
func (s *Store) LatestRun(ctx context.Context, source Source) (Run, bool, error) {
	if !source.Valid() {
		return Run{}, false, fmt.Errorf("%w: %q", ErrInvalidSource, source)
	}
	row := s.pool.QueryRow(ctx, latestRunSQL, string(source))
	run, err := scanRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, false, nil
	}
	if err != nil {
		return Run{}, false, fmt.Errorf("importer: latest run for %s: %w", source, err)
	}
	return run, true, nil
}

// rowScanner is the subset of pgx.Row that scanRun needs, satisfied by both
// pool.QueryRow results and rows from a multi-row query.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanRun reads one import_runs row into a Run, decoding the counts JSONB. The
// scan error is wrapped with %w so callers can still map a pgx.ErrNoRows from a
// QueryRow to ErrRunNotFound via errors.Is.
func scanRun(row rowScanner) (Run, error) {
	var (
		run    Run
		counts []byte
	)
	if err := row.Scan(
		&run.ID, &run.Source, &run.StartedAt, &run.FinishedAt,
		&run.Status, &run.HighWatermark, &counts, &run.LastError,
	); err != nil {
		return Run{}, fmt.Errorf("importer: scanning run: %w", err)
	}
	if err := json.Unmarshal(counts, &run.Counts); err != nil {
		return Run{}, fmt.Errorf("importer: decoding counts for run %d: %w", run.ID, err)
	}
	return run, nil
}
