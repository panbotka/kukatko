package rekey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// runColumns is the column list every read of a run selects, in scanRun order.
const runColumns = `uid, project_uid, state, target_prefix, total_photos, done_photos, total_bytes,
	done_bytes, last_key, pending_photo_uid, pending_old_key, errors, error_count,
	started_at, updated_at, finished_at`

// rowScanner is the one method scanRun needs of a pgx.Row or pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanRun reads one run in runColumns order. It returns pgx.ErrNoRows unwrapped
// so callers can map it onto their own sentinel.
func scanRun(row rowScanner) (Run, error) {
	var (
		run    Run
		state  string
		errors []byte
	)
	err := row.Scan(&run.UID, &run.ProjectUID, &state, &run.TargetPrefix, &run.TotalPhotos,
		&run.DonePhotos, &run.TotalBytes, &run.DoneBytes, &run.LastKey, &run.PendingPhotoUID,
		&run.PendingOldKey, &errors, &run.ErrorCount, &run.StartedAt, &run.UpdatedAt, &run.FinishedAt)
	if err != nil {
		return Run{}, err //nolint:wrapcheck // callers map pgx.ErrNoRows and wrap the rest.
	}
	run.State = State(state)
	run.Errors = []RunError{}
	if err := json.Unmarshal(errors, &run.Errors); err != nil {
		return Run{}, fmt.Errorf("rekey: decoding the errors of run %s: %w", run.UID, err)
	}
	return run, nil
}

// queryRun runs a statement returning one run row and maps "no row" to
// notFound.
func queryRun(ctx context.Context, q pgxQuerier, notFound error, sql string, args ...any) (Run, error) {
	run, err := scanRun(q.QueryRow(ctx, sql, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, notFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("rekey: reading a run: %w", err)
	}
	return run, nil
}

// pgxQuerier is what queryRun needs: a pool or a transaction.
type pgxQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// compile-time assertions that both a pool and a transaction can read a run.
var (
	_ pgxQuerier = (*pgxpool.Pool)(nil)
	_ pgxQuerier = (pgx.Tx)(nil)
)

// getRunSQL reads one run by uid.
const getRunSQL = `SELECT ` + runColumns + ` FROM storage_rekey_runs WHERE uid = $1`

// latestRunSQL reads a project's most recent run.
const latestRunSQL = `SELECT ` + runColumns + ` FROM storage_rekey_runs
WHERE project_uid = $1 ORDER BY started_at DESC, uid DESC LIMIT 1`

// transitionSQL moves a project's most recent run from state $2 to $3; $4 resets
// the cursor (Retry starts a fresh pass over the photos the last one left).
const transitionSQL = `
UPDATE storage_rekey_runs SET state = $3, updated_at = now(),
	last_key = CASE WHEN $4 THEN '' ELSE last_key END
WHERE uid = (SELECT uid FROM storage_rekey_runs WHERE project_uid = $1
             ORDER BY started_at DESC, uid DESC LIMIT 1)
  AND state = $2
RETURNING ` + runColumns

// pendingBatchSQL reads the next photos of a project still in the old layout,
// in file_path order after the cursor.
const pendingBatchSQL = `
SELECT uid, file_path, file_hash, file_size FROM photos
WHERE project_id = $1 AND NOT starts_with(file_path, $2) AND file_path > $3
ORDER BY file_path LIMIT $4`

// photoByUIDSQL reads one photo of a project.
const photoByUIDSQL = `
SELECT uid, file_path, file_hash, file_size FROM photos WHERE uid = $1 AND project_id = $2`

// renditionsSQL reads the recorded streaming renditions of a set of photos.
const renditionsSQL = `
SELECT photo_uid, rendition, playlist, segment_count FROM photo_hls_renditions
WHERE photo_uid = ANY($1) ORDER BY photo_uid, rendition`

// remainingSQL counts a project's photos still in the old layout.
const remainingSQL = `SELECT count(*) FROM photos WHERE project_id = $1 AND NOT starts_with(file_path, $2)`

// movePhotoSQL points a photo at its new key, only if it still points at the
// old one.
const movePhotoSQL = `UPDATE photos SET file_path = $3, updated_at = now()
WHERE uid = $1 AND project_id = $4 AND file_path = $2`

// moveFileSQL points the photo's file row for the old key at the new key.
const moveFileSQL = `UPDATE photo_files SET file_path = $3 WHERE photo_uid = $1 AND file_path = $2`

// markPendingSQL records, in the transaction that moves the row, that the
// photo's old objects await deletion.
const markPendingSQL = `UPDATE storage_rekey_runs
SET pending_photo_uid = $2, pending_old_key = $3, updated_at = now() WHERE uid = $1`

// completePhotoSQL counts one moved photo, advances the cursor and clears the
// pending photo; it returns the run's state so the caller notices a pause.
const completePhotoSQL = `UPDATE storage_rekey_runs
SET done_photos = done_photos + 1, done_bytes = done_bytes + $2, last_key = $3,
	pending_photo_uid = '', pending_old_key = '', updated_at = now()
WHERE uid = $1 RETURNING state`

// clearPendingSQL forgets a pending photo that is no longer there to finish.
const clearPendingSQL = `UPDATE storage_rekey_runs
SET pending_photo_uid = '', pending_old_key = '', updated_at = now() WHERE uid = $1`

// recordErrorSQL appends one error to the run's ring (keeping the last $4),
// counts it, optionally advances the cursor past the failed photo, and fails a
// running run whose count passes $5. It returns the state afterwards.
const recordErrorSQL = `UPDATE storage_rekey_runs SET
	errors = (SELECT COALESCE(jsonb_agg(e ORDER BY ord), '[]'::jsonb) FROM (
		SELECT e, ord FROM jsonb_array_elements(errors || $2::jsonb) WITH ORDINALITY AS t(e, ord)
		ORDER BY ord DESC LIMIT $4) AS kept),
	error_count = error_count + 1,
	last_key = CASE WHEN $3 = '' THEN last_key ELSE $3 END,
	state = CASE WHEN state = 'running' AND error_count + 1 > $5 THEN 'failed' ELSE state END,
	updated_at = now()
WHERE uid = $1 RETURNING state`

// setStateSQL puts a run into a state, stamping finished_at when it is done.
const setStateSQL = `UPDATE storage_rekey_runs
SET state = $2, updated_at = now(), finished_at = CASE WHEN $2 = 'done' THEN now() ELSE finished_at END
WHERE uid = $1 RETURNING ` + runColumns

// photoUIDsSQL lists every photo of a project, for the sidecar rewrite.
const photoUIDsSQL = `SELECT uid FROM photos WHERE project_id = $1 ORDER BY uid`

// keyReferencedSQL reports whether any photo row of any project points at a
// key: an object a row names is never deleted, whichever project's it is.
const keyReferencedSQL = `SELECT EXISTS (SELECT 1 FROM photos WHERE file_path = $1)`

// rootLayoutProjectsSQL counts the projects still in the root layout.
const rootLayoutProjectsSQL = `SELECT count(*) FROM projects WHERE storage_prefix = ''`

// errorEntry encodes one RunError as the one-element JSON array recordErrorSQL
// appends.
func errorEntry(key string, cause error, at time.Time) ([]byte, error) {
	entry, err := json.Marshal([]RunError{{Key: key, Message: cause.Error(), At: at.UTC()}})
	if err != nil {
		return nil, fmt.Errorf("rekey: encoding an error entry: %w", err)
	}
	return entry, nil
}
