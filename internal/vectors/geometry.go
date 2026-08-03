package vectors

import (
	"context"
	"fmt"

	"github.com/panbotka/kukatko/internal/exif"
)

// RenormalizeTransposedBBox repairs a face bbox that was normalised against the
// TRANSPOSED display frame, which is what happens when the frame it was divided
// by came from a source that had already applied the EXIF orientation to its
// dimensions (PhotoPrism, and photo-sorter after it) and the divider swapped them
// a second time.
//
// The detector's pixel box is in display space either way — the embeddings
// sidecar auto-rotates before detecting — so only the divisors were wrong: x and
// w were divided by the raw width where the display width applies, y and h by the
// raw height where the display height applies. For a quarter turn the display
// frame is the raw frame transposed, so undoing it is a per-axis rescale by
// rawWidth/rawHeight and its reciprocal. rawWidth/rawHeight are the photo's
// stored (pre-rotation) dimensions.
//
// Anything but a quarter turn leaves the box alone: without a swap the two frames
// coincide and the normalisation was already right. A degenerate frame is
// likewise returned unchanged rather than producing NaN/Inf coordinates.
func RenormalizeTransposedBBox(bbox [4]float64, rawWidth, rawHeight, orientation int) [4]float64 {
	if !exif.QuarterTurn(orientation) || rawWidth <= 0 || rawHeight <= 0 {
		return bbox
	}
	scale := float64(rawWidth) / float64(rawHeight)
	return [4]float64{bbox[0] * scale, bbox[1] / scale, bbox[2] * scale, bbox[3] / scale}
}

// repairFaceDimensionsSQL rewrites the cached frame and the normalised bbox of
// every face of one photo that was recorded against the transposed frame. The
// WHERE clause is the fingerprint of exactly that mistake — a quarter-turned
// photo whose face row cached the display frame (the raw pair swapped) — so a row
// already correct is left alone and a second run matches nothing.
const repairFaceDimensionsSQL = `
UPDATE faces
SET photo_width  = $2,
    photo_height = $3,
    bbox         = ARRAY[bbox[1] * $4, bbox[2] / $4, bbox[3] * $4, bbox[4] / $4]::double precision[]
WHERE photo_uid = $1
  AND orientation BETWEEN 5 AND 8
  AND photo_width = $3
  AND photo_height = $2`

// RepairFaceDimensions rewrites the faces of one photo whose cached frame and
// normalised bbox were recorded against the transposed display frame, and returns
// how many rows it changed. rawWidth/rawHeight are the photo's stored
// (pre-rotation) dimensions, i.e. what photos.file_width/file_height mean.
//
// It is the faces half of the orientation repair: the photos half fixes the
// catalogue row the viewer sizes its frame from, this one fixes the per-face
// render hints (the subject tiles and cluster crops read them) and the boxes
// themselves. It is idempotent — the fingerprint in the WHERE clause stops
// matching once a row is fixed — and it is a no-op for a square frame, where the
// swap and the rescale are both identities.
func (s *Store) RepairFaceDimensions(ctx context.Context, photoUID string, rawWidth, rawHeight int) (int64, error) {
	if rawWidth <= 0 || rawHeight <= 0 {
		return 0, nil
	}
	scale := float64(rawWidth) / float64(rawHeight)
	tag, err := s.pool.Exec(ctx, repairFaceDimensionsSQL, photoUID, rawWidth, rawHeight, scale)
	if err != nil {
		return 0, fmt.Errorf("vectors: repairing face dimensions for %s: %w", photoUID, err)
	}
	return tag.RowsAffected(), nil
}
