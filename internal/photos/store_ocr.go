package photos

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// OCR is what the text recogniser read from a photo: the text itself and the tag
// of the model that produced it. It is written by the `ocr` job and read by
// full-text search (through the generated fts column) and by the `text:` filter.
//
// An empty Text is a legitimate value — most photographs have no text in them —
// and is stored as such, because the alternative (leave the row untouched) makes
// "OCR found nothing" indistinguishable from "OCR never ran" and re-schedules the
// whole library on every backfill. The bookkeeping timestamp is what tells the
// two apart; see migration 0058.
type OCR struct {
	// Text is the recognised text, blocks joined by newlines in reading order.
	Text string
	// Model is the recogniser's model tag, e.g. "PP-OCRv5_mobile".
	Model string
}

// saveOCRSQL overwrites a photo's OCR columns and stamps ocr_at.
//
// updated_at is deliberately NOT bumped. OCR is machine-derived bookkeeping the
// user never asked for; touching updated_at would reorder "recently edited"
// listings for the entire library the first time the backfill drains, which is
// exactly the kind of change nobody wants a background job to make.
const saveOCRSQL = `
UPDATE photos
SET ocr_text = $2, ocr_model = $3, ocr_at = now()
WHERE uid = $1
RETURNING uid`

// SaveOCR stores the text a recogniser read from the photo identified by uid and
// marks the photo as OCR'd. It overwrites whatever was there before: unlike the
// file-metadata fill this is not gap-filling but a fresh reading of the same
// image, so a forced re-run with a better model must be able to replace an older,
// worse one.
//
// It returns ErrPhotoNotFound when no such photo exists.
func (s *Store) SaveOCR(ctx context.Context, uid string, result OCR) error {
	var got string
	if err := s.pool.QueryRow(ctx, saveOCRSQL, uid, result.Text, result.Model).Scan(&got); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPhotoNotFound
		}
		return fmt.Errorf("photos: saving OCR of %s: %w", uid, err)
	}
	return nil
}

// getOCRSQL reads one photo's stored OCR result.
const getOCRSQL = `SELECT ocr_text, ocr_model FROM photos WHERE uid = $1 AND ocr_at IS NOT NULL`

// GetOCR returns the OCR result stored for the photo identified by uid. It
// returns ErrPhotoNotFound both when the photo does not exist and when it has
// never been OCR'd — from a reader's point of view there is no result either way,
// and the caller that needs to tell them apart is the backfill, which asks by
// listing rather than by uid.
func (s *Store) GetOCR(ctx context.Context, uid string) (OCR, error) {
	var out OCR
	if err := s.pool.QueryRow(ctx, getOCRSQL, uid).Scan(&out.Text, &out.Model); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OCR{}, ErrPhotoNotFound
		}
		return OCR{}, fmt.Errorf("photos: loading OCR of %s: %w", uid, err)
	}
	return out, nil
}

// listMissingOCRSQL selects the uids of non-archived, non-video photos that have
// never been through the text recogniser, newest first. The trailing %s receives
// an optional LIMIT clause. It is served by idx_photos_ocr_pending, the partial
// index over exactly this predicate (migration 0058).
const listMissingOCRSQL = `
SELECT uid
FROM photos
WHERE ocr_at IS NULL AND archived_at IS NULL AND media_type <> 'video'
ORDER BY created_at DESC, uid DESC%s`

// ListPhotosMissingOCR returns the uids of non-archived photos that have never
// been read by the text recogniser, newest first. A positive limit caps the
// result; a non-positive limit returns every pending photo. It backs the OCR
// backfill, which enqueues an `ocr` job per returned uid.
//
// Videos are excluded: OCR runs on stills only, and there is deliberately no
// poster-frame recognition. Live photos are included — they are stills that
// happen to carry a motion clip.
//
// The predicate is the recognition marker rather than "the text is empty", so the
// backfill converges: a photograph with no writing in it is finished once it has
// been looked at, and is not re-scheduled on every run.
func (s *Store) ListPhotosMissingOCR(ctx context.Context, limit int) ([]string, error) {
	query := fmt.Sprintf(listMissingOCRSQL, "")
	args := []any(nil)
	if limit > 0 {
		query = fmt.Sprintf(listMissingOCRSQL, "\nLIMIT $1")
		args = []any{limit}
	}
	return s.queryUIDs(ctx, "listing photos missing OCR", query, args...)
}

// listActiveImageUIDsSQL selects the uids of every non-archived, non-video photo,
// newest first.
const listActiveImageUIDsSQL = `
SELECT uid
FROM photos
WHERE archived_at IS NULL AND media_type <> 'video'
ORDER BY created_at DESC, uid DESC`

// ListActiveImageUIDs returns the uids of every non-archived photo that is not a
// video, newest first. It backs the forced full OCR backfill (`?all=true`), which
// re-reads photos the recogniser has already seen — how a library picks up a
// better recognition model.
func (s *Store) ListActiveImageUIDs(ctx context.Context) ([]string, error) {
	return s.queryUIDs(ctx, "listing active image photos", listActiveImageUIDsSQL)
}
