package photos

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// saveBlurhashSQL overwrites a photo's placeholder.
//
// updated_at is deliberately NOT bumped, for the same reason SaveOCR leaves it
// alone: the placeholder is machine-derived bookkeeping nobody asked for, and
// touching updated_at would reorder every "recently edited" listing in the
// library the first time the backfill drains.
const saveBlurhashSQL = `
UPDATE photos
SET blurhash = $2
WHERE uid = $1
RETURNING uid`

// SaveBlurhash stores the placeholder computed for the photo identified by uid,
// replacing whatever was there before. An empty hash clears the column back to
// NULL — "not computed yet" — so a caller never has to reach for a second method
// to undo a bad value, and so the photos_blurhash_not_empty CHECK cannot be
// tripped by a caller that has nothing to store.
//
// It returns ErrPhotoNotFound when no such photo exists.
func (s *Store) SaveBlurhash(ctx context.Context, uid, hash string) error {
	var got string
	if err := s.pool.QueryRow(ctx, saveBlurhashSQL, uid, blurhashOrNil(hash)).Scan(&got); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPhotoNotFound
		}
		return fmt.Errorf("photos: saving blurhash of %s: %w", uid, err)
	}
	return nil
}

// listMissingBlurhashSQL selects the uids of non-archived photos with no
// placeholder, newest first. The trailing %s receives an optional LIMIT clause.
// It is served by idx_photos_blurhash_pending, the partial index over exactly
// this predicate (migration 0068).
const listMissingBlurhashSQL = `
SELECT uid
FROM photos
WHERE blurhash IS NULL AND archived_at IS NULL
ORDER BY created_at DESC, uid DESC%s`

// ListPhotosMissingBlurhash returns the uids of non-archived photos that have no
// placeholder yet, newest first. A positive limit caps the result; a non-positive
// limit returns every pending photo. It backs the placeholder backfill, which
// enqueues a thumbnail job per returned uid — the job that computes the
// placeholder alongside the renditions it is derived from.
//
// Videos are included: their thumbnails are rendered from the poster frame the
// pipeline extracts, so a clip has a first frame to blur exactly as a still has
// a picture.
//
// Newest first is deliberate: a backfill over a large library drains for a while,
// and the photos somebody is most likely to be looking at while it does are the
// ones that arrived last.
func (s *Store) ListPhotosMissingBlurhash(ctx context.Context, limit int) ([]string, error) {
	query := fmt.Sprintf(listMissingBlurhashSQL, "")
	args := []any(nil)
	if limit > 0 {
		query = fmt.Sprintf(listMissingBlurhashSQL, "\nLIMIT $1")
		args = []any{limit}
	}
	return s.queryUIDs(ctx, "listing photos missing blurhash", query, args...)
}

// countMissingBlurhashSQL counts what ListPhotosMissingBlurhash would return.
const countMissingBlurhashSQL = `
SELECT count(*) FROM photos WHERE blurhash IS NULL AND archived_at IS NULL`

// CountPhotosMissingBlurhash returns how many photos ListPhotosMissingBlurhash
// would return, without materialising them. It backs the backfill's dry run, so
// an operator can see the size of a full-library run before starting it.
func (s *Store) CountPhotosMissingBlurhash(ctx context.Context) (int, error) {
	var count int
	if err := s.pool.QueryRow(ctx, countMissingBlurhashSQL).Scan(&count); err != nil {
		return 0, fmt.Errorf("photos: counting photos missing blurhash: %w", err)
	}
	return count, nil
}
