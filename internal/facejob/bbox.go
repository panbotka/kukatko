package facejob

import "io"

// UprightImage is one photo's original made ready for the detector: pixels in
// display orientation and no EXIF orientation tag left to apply, together with the
// frame those pixels measure.
//
// The two fields belong together on purpose. Width and Height are measured on the
// very bytes Reader yields, so they are the frame the detector sees and therefore
// the only correct divisor for a pixel box it returns. The embeddings sidecar
// (InsightFace) does NOT rotate by EXIF — verified against the running service:
// the same iPhone original with orientation 6 yielded 2 misshapen boxes untouched
// and 6 face-shaped ones pre-rotated, and a second yielded 0 against 2 — so
// whoever sends the image owns the rotation, and this type is where that is done
// and stated.
type UprightImage struct {
	// Reader streams the upright image bytes. The caller must Close it.
	Reader io.ReadCloser
	// Width and Height are the pixel dimensions of exactly those bytes.
	Width  int
	Height int
}

// NormalizeBBox converts a face bounding box from sidecar pixel coordinates
// [x1, y1, x2, y2] to normalized coordinates [x, y, w, h] in 0..1 of the frame the
// box was detected in, mirroring photo-sorter's
// ConvertPixelBBoxToDisplayRelative. It is the single source of truth for this
// conversion.
//
// frameWidth and frameHeight must be the dimensions of the image the detector
// actually saw — UprightImage.Width/Height, not the photo's stored pair. That is
// the whole point of taking them as arguments: because the image sent is upright,
// its frame IS the display frame, so no orientation enters this conversion and
// there is no assumption left about who rotated what. Deriving the divisors from
// photos.file_width/file_height and an orientation tag instead is what put face
// boxes beside faces on every quarter-turned photo the live detector touched.
//
// If the inputs are degenerate (non-positive dimensions) the box is returned
// unchanged so a missing/zero dimension never produces NaN/Inf coordinates.
func NormalizeBBox(bbox [4]float64, frameWidth, frameHeight int) [4]float64 {
	if frameWidth <= 0 || frameHeight <= 0 {
		return bbox
	}

	x1 := bbox[0] / float64(frameWidth)
	y1 := bbox[1] / float64(frameHeight)
	x2 := bbox[2] / float64(frameWidth)
	y2 := bbox[3] / float64(frameHeight)

	return [4]float64{x1, y1, x2 - x1, y2 - y1}
}
