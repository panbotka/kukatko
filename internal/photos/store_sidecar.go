package photos

import (
	"context"
	"fmt"
)

// listSidecarPendingSQL selects the uids of non-archived photos whose metadata
// sidecar is missing or stale, newest first. The trailing %s receives an optional
// LIMIT clause.
//
// The NULL half is served by idx_photos_sidecar_pending, the partial index over
// exactly that predicate, and is the first backfill's whole workload. The
// staleness half (written before the photo's own last edit) falls back to a scan,
// which is the right trade for an admin-only backfill that is expected to find
// nothing: it exists to recover an enqueue lost to a crash, not to carry the
// normal path.
const listSidecarPendingSQL = `
SELECT uid
FROM photos
WHERE archived_at IS NULL
  AND (sidecar_written_at IS NULL OR sidecar_written_at < updated_at)
ORDER BY created_at DESC, uid DESC%s`

// ListPhotosMissingSidecar returns the uids of non-archived photos whose metadata
// sidecar has never been written (sidecar_written_at IS NULL) or was written
// before the photo's last edit, newest first. A positive limit caps the result; a
// non-positive limit returns every pending photo. It backs the sidecar backfill,
// which enqueues a `sidecar` job per returned uid.
//
// The predicate is the write marker rather than "the file is absent", so the
// backfill converges without touching storage: it needs no per-photo Head against
// a bucket to decide what is pending, and a re-run over a drained library returns
// nothing.
//
// It deliberately does not catch curation that lives in another table — an album
// membership or a label attached without its enqueue landing leaves updated_at
// untouched and the photo looks current here. Recovering that is what the forced
// full run (ListActiveUIDs) is for.
func (s *Store) ListPhotosMissingSidecar(ctx context.Context, limit int) ([]string, error) {
	query := fmt.Sprintf(listSidecarPendingSQL, "")
	args := []any(nil)
	if limit > 0 {
		query = fmt.Sprintf(listSidecarPendingSQL, "\nLIMIT $1")
		args = []any{limit}
	}
	return s.queryUIDs(ctx, "listing photos missing a sidecar", query, args...)
}

// markSidecarWrittenSQL stamps the sidecar export marker. It deliberately leaves
// updated_at alone: this records the export, not an edit of the photo, and
// bumping updated_at would make every write mark its own photo stale again so the
// backfill's staleness predicate could never drain.
const markSidecarWrittenSQL = `
UPDATE photos
SET sidecar_written_at = now()
WHERE uid = $1`

// MarkSidecarWritten stamps sidecar_written_at on the photo identified by uid,
// recording that its sidecar is current as of now. It returns ErrPhotoNotFound
// when no such photo exists.
//
// The caller stamps only after the file has actually landed in storage, so a
// failed write leaves the photo pending and the backfill picks it up again.
func (s *Store) MarkSidecarWritten(ctx context.Context, uid string) error {
	tag, err := s.pool.Exec(ctx, markSidecarWrittenSQL, uid)
	if err != nil {
		return fmt.Errorf("photos: marking sidecar written for %s: %w", uid, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPhotoNotFound
	}
	return nil
}
