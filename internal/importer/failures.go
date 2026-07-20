package importer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Stage names the import step a failure happened in, stored in
// import_failures.stage. StagePhoto is a whole photo that could not be imported;
// the others are the best-effort satellites that used to be logged and silently
// lost (see the import_failures migration and ARCHITECTURE.md §5.2).
type Stage string

const (
	// StagePhoto is a whole photo that failed to import (download, dedup,
	// catalogue write): the photo is missing from Kukátko entirely.
	StagePhoto Stage = "photo"
	// StageFile is an original file/sibling of an imported photo that was dropped
	// (e.g. a RAW sibling or a motion-photo video clip).
	StageFile Stage = "file"
	// StageMarker is a face marker that failed to transfer onto an imported photo.
	StageMarker Stage = "marker"
	// StageAlbumMember is an album membership that failed to attach.
	StageAlbumMember Stage = "album_member"
	// StageLabel is a label (or label membership) that failed to attach.
	StageLabel Stage = "label"
	// StageThumbnail is a thumbnail that failed to generate for an imported photo.
	StageThumbnail Stage = "thumbnail"
	// StageEmbedding is an image embedding that failed to record from the feeds.
	StageEmbedding Stage = "embedding"
	// StageFaces is a photo's face vectors that failed to record from the feeds.
	StageFaces Stage = "faces"
	// StagePhash is a perceptual hash that failed to transfer.
	StagePhash Stage = "phash"
	// StageEdit is a non-destructive edit that failed to transfer.
	StageEdit Stage = "edit"
	// StageMetadata is photo metadata/details that failed to apply.
	StageMetadata Stage = "metadata"
)

// Failure is one row of import_failures: a single per-photo or per-file failure
// recorded during a run, so it can be listed and retried instead of being lost to
// slog.Warn. RunID and Source identify the run; the remaining fields describe what
// failed. ResolvedAt is nil while the failure is outstanding.
type Failure struct {
	ID         int64      `json:"id"`
	RunID      int64      `json:"run_id"`
	Source     Source     `json:"source"`
	Stage      Stage      `json:"stage"`
	PhotoUID   string     `json:"photo_uid"`
	SourceRef  string     `json:"source_ref"`
	Detail     string     `json:"detail"`
	Error      string     `json:"error"`
	CreatedAt  time.Time  `json:"created_at"`
	ResolvedAt *time.Time `json:"resolved_at"`
}

// NewFailure builds a Failure for run runID from source, filling only the
// descriptive fields. It is a convenience so import services can accumulate
// failures in one line: NewFailure(runID, src, StagePhoto, "", ppUID, name, err).
// A nil err records an empty message.
func NewFailure(
	runID int64, source Source, stage Stage, photoUID, sourceRef, detail string, err error,
) Failure {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return Failure{
		RunID:     runID,
		Source:    source,
		Stage:     stage,
		PhotoUID:  photoUID,
		SourceRef: sourceRef,
		Detail:    detail,
		Error:     msg,
	}
}

// failureColumns is the column list shared by every import_failures SELECT so the
// scan order stays in one place.
const failureColumns = `id, run_id, source, stage, photo_uid, source_ref, detail, error, created_at, resolved_at`

// insertFailureSQL appends one failure row for a run.
const insertFailureSQL = `
INSERT INTO import_failures (run_id, source, stage, photo_uid, source_ref, detail, error)
VALUES ($1, $2, $3, $4, $5, $6, $7)`

// RecordFailures persists failures, one import_failures row each, in a single
// batch. Failures with an empty RunID or an unset Source are rejected as a caller
// mistake. Recording no failures is a no-op. Persist failures before Complete so
// the run's terminal status reflects them.
func (s *Store) RecordFailures(ctx context.Context, failures []Failure) error {
	if len(failures) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for i := range failures {
		f := failures[i]
		if f.RunID == 0 {
			return errors.New("importer: recording failure: missing run id")
		}
		if !f.Source.Valid() {
			return fmt.Errorf("%w: %q", ErrInvalidSource, f.Source)
		}
		batch.Queue(insertFailureSQL,
			f.RunID, string(f.Source), string(f.Stage), f.PhotoUID, f.SourceRef, f.Detail, f.Error)
	}
	results := s.pool.SendBatch(ctx, batch)
	defer func() { _ = results.Close() }()
	for range failures {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("importer: recording %d import failures: %w", len(failures), err)
		}
	}
	return nil
}

// countUnresolvedSQL counts the outstanding failures of a run.
const countUnresolvedSQL = `SELECT count(*) FROM import_failures WHERE run_id = $1 AND resolved_at IS NULL`

// CountUnresolvedFailures returns how many outstanding (resolved_at IS NULL)
// failures the run identified by id has recorded. Complete uses it to decide
// between the 'done' and 'partial' terminal status.
func (s *Store) CountUnresolvedFailures(ctx context.Context, id int64) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx, countUnresolvedSQL, id).Scan(&n); err != nil {
		return 0, fmt.Errorf("importer: counting failures for run %d: %w", id, err)
	}
	return n, nil
}

// FailureFilter narrows a ListFailures query. A zero RunID matches every run; an
// empty Source matches every source; UnresolvedOnly restricts to outstanding
// failures. Limit is clamped to [1, maxListLimit] (a non-positive Limit defaults
// to defaultListLimit) and a negative Offset is treated as zero.
type FailureFilter struct {
	RunID          int64
	Source         Source
	UnresolvedOnly bool
	Limit          int
	Offset         int
}

// ListFailures returns a page of recorded import failures matching filter, most
// recently recorded first (id tiebreaker keeps the order stable). It returns a
// non-nil, empty slice when nothing matches. It returns ErrInvalidSource if
// filter.Source is set but unrecognised.
func (s *Store) ListFailures(ctx context.Context, filter FailureFilter) ([]Failure, error) {
	if filter.Source != "" && !filter.Source.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidSource, filter.Source)
	}
	limit := filter.Limit
	switch {
	case limit <= 0:
		limit = defaultListLimit
	case limit > maxListLimit:
		limit = maxListLimit
	}
	offset := max(filter.Offset, 0)
	sql, args := buildListFailuresQuery(filter, limit, offset)
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("importer: listing failures: %w", err)
	}
	defer rows.Close()

	failures := make([]Failure, 0, limit)
	for rows.Next() {
		f, scanErr := scanFailure(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		failures = append(failures, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("importer: iterating failures: %w", err)
	}
	return failures, nil
}

// buildListFailuresQuery assembles the ListFailures SELECT and its ordered
// arguments from filter, appending only the WHERE clauses the filter enables so
// every predicate stays parameterised.
func buildListFailuresQuery(filter FailureFilter, limit, offset int) (string, []any) {
	var (
		args   []any
		wheres []string
	)
	if filter.RunID != 0 {
		args = append(args, filter.RunID)
		wheres = append(wheres, fmt.Sprintf("run_id = $%d", len(args)))
	}
	if filter.Source != "" {
		args = append(args, string(filter.Source))
		wheres = append(wheres, fmt.Sprintf("source = $%d", len(args)))
	}
	if filter.UnresolvedOnly {
		wheres = append(wheres, "resolved_at IS NULL")
	}
	var b strings.Builder
	b.WriteString(`SELECT ` + failureColumns + ` FROM import_failures`)
	if len(wheres) > 0 {
		b.WriteString(" WHERE ")
		b.WriteString(strings.Join(wheres, " AND "))
	}
	args = append(args, limit, offset)
	fmt.Fprintf(&b, " ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	return b.String(), args
}

// scanFailure reads one import_failures row into a Failure.
func scanFailure(row rowScanner) (Failure, error) {
	var f Failure
	if err := row.Scan(
		&f.ID, &f.RunID, &f.Source, &f.Stage, &f.PhotoUID,
		&f.SourceRef, &f.Detail, &f.Error, &f.CreatedAt, &f.ResolvedAt,
	); err != nil {
		return Failure{}, fmt.Errorf("importer: scanning failure: %w", err)
	}
	return f, nil
}
