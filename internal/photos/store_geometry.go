package photos

import (
	"context"
	"fmt"
	"strings"
)

// DimensionMismatch is one photo whose stored pixel dimensions contradict the
// dimensions its own EXIF document records: the photo is quarter-turned by its
// EXIF orientation and file_width/file_height are the file's dimensions with the
// two exchanged, i.e. the DISPLAYED frame rather than the stored one.
//
// That is what a PhotoPrism-derived import produced: PhotoPrism reports its file
// dimensions with the orientation already applied, while file_orientation is the
// raw tag, so the pair told every consumer to rotate a second time. See
// exif.RawDimensions.
type DimensionMismatch struct {
	// UID is the affected photo.
	UID string `json:"uid"`
	// StoredWidth and StoredHeight are the contradictory values currently in the
	// catalogue (the displayed frame).
	StoredWidth  int `json:"stored_width"`
	StoredHeight int `json:"stored_height"`
	// Orientation is the photo's raw EXIF orientation tag (5-8 here, the quarter
	// turns).
	Orientation int `json:"orientation"`
	// RawWidth and RawHeight are the file's own dimensions, read out of the stored
	// EXIF document — the values the columns should hold.
	RawWidth  int `json:"raw_width"`
	RawHeight int `json:"raw_height"`
}

// exifPixelSizeSQL builds the SQL expression that reads a pixel dimension out of
// the stored EXIF document, trying the tag names exiftool and the pure-Go
// fallback use, in order of reliability. Every candidate is type-checked with
// jsonb_typeof before the cast: the column is a free-form document and a
// non-numeric value there must degrade to "unknown" rather than abort the whole
// query with a cast error.
func exifPixelSizeSQL(keys ...string) string {
	parts := make([]string, len(keys))
	for i, key := range keys {
		parts[i] = fmt.Sprintf(
			"NULLIF(CASE WHEN jsonb_typeof(p.exif->'%s') = 'number' "+
				"THEN (p.exif->>'%s')::int END, 0)", key, key)
	}
	return "COALESCE(" + strings.Join(parts, ", ") + ")"
}

// listDimensionMismatchesSQL selects the quarter-turned photos whose stored
// dimensions are their file's own dimensions transposed.
//
// The comparison IS the evidence: a row is only reported when the EXIF document
// states a size and the columns hold exactly that size with the sides exchanged.
// A photo whose file says nothing, or whose columns already agree with it, is
// left out — no provenance guessing, no heuristic, and nothing to undo on a
// square frame (raw_w <> raw_h excludes it, where the swap is a no-op anyway).
var listDimensionMismatchesSQL = `
SELECT uid, file_width, file_height, file_orientation, raw_w, raw_h
FROM (
    SELECT p.uid, p.file_width, p.file_height, p.file_orientation,
           ` + exifPixelSizeSQL("ImageWidth", "ExifImageWidth", "PixelXDimension") + ` AS raw_w,
           ` + exifPixelSizeSQL("ImageHeight", "ImageLength", "ExifImageHeight", "PixelYDimension") + ` AS raw_h
    FROM photos p
    WHERE p.file_orientation BETWEEN 5 AND 8
      AND p.exif IS NOT NULL
) g
WHERE raw_w IS NOT NULL AND raw_h IS NOT NULL
  AND raw_w <> raw_h
  AND file_width = raw_h
  AND file_height = raw_w
ORDER BY uid`

// ListDimensionMismatches returns every photo whose stored pixel dimensions are
// its own file's dimensions transposed — the PhotoPrism-derived import defect
// that made the viewer letterbox a rotated photo and drift its face boxes off the
// faces. It is read-only, which makes it the dry run of the repair: the returned
// list is exactly what RepairDimensions would rewrite, with both the current and
// the corrected pair, so the change can be inspected before it is made.
//
// The slice is empty (not nil) when the catalogue is consistent.
func (s *Store) ListDimensionMismatches(ctx context.Context) ([]DimensionMismatch, error) {
	rows, err := s.pool.Query(ctx, listDimensionMismatchesSQL)
	if err != nil {
		return nil, fmt.Errorf("photos: listing dimension mismatches: %w", err)
	}
	defer rows.Close()

	out := make([]DimensionMismatch, 0)
	for rows.Next() {
		var m DimensionMismatch
		if err := rows.Scan(&m.UID, &m.StoredWidth, &m.StoredHeight,
			&m.Orientation, &m.RawWidth, &m.RawHeight); err != nil {
			return nil, fmt.Errorf("photos: scanning dimension mismatch: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photos: iterating dimension mismatches: %w", err)
	}
	return out, nil
}

// repairDimensionsSQL writes the file's own dimensions onto the photo, guarded by
// the transposed pair it is replacing so the update is a no-op once applied (or
// if the row changed underneath) rather than swapping a corrected row back.
const repairDimensionsSQL = `
UPDATE photos
SET file_width = $2, file_height = $3, updated_at = now()
WHERE uid = $1 AND file_width = $3 AND file_height = $2`

// RepairDimensions writes the file's own (stored, pre-rotation) dimensions onto
// the photo, reporting whether the row changed. It is idempotent and reversible:
// the guard makes a repeat run a no-op, and because the correction is derived
// from the file's own EXIF rather than from a rule about where the row came from,
// re-reading the file always reproduces it — an accidental run can be undone by
// swapping the pair back, which is the same transform (exif.RawDimensions is its
// own inverse).
func (s *Store) RepairDimensions(ctx context.Context, m DimensionMismatch) (bool, error) {
	if m.RawWidth <= 0 || m.RawHeight <= 0 {
		return false, nil
	}
	tag, err := s.pool.Exec(ctx, repairDimensionsSQL, m.UID, m.RawWidth, m.RawHeight)
	if err != nil {
		return false, fmt.Errorf("photos: repairing dimensions of %s: %w", m.UID, err)
	}
	return tag.RowsAffected() > 0, nil
}
