package facejob

// NormalizeBBox converts a face bounding box from sidecar pixel coordinates
// [x1, y1, x2, y2] to normalized display-space coordinates [x, y, w, h] in 0..1,
// mirroring photo-sorter's ConvertPixelBBoxToDisplayRelative. It is the single
// source of truth for this conversion, shared with the photo-sorter feeds
// importer (internal/psfeedsimport), whose faces feed carries the same raw pixel
// [x1, y1, x2, y2] boxes.
//
// The embeddings sidecar (InsightFace) auto-rotates the image by its EXIF
// orientation before detecting faces, so the returned pixel box is already in
// display space — no coordinate rotation is needed here. We only have to divide
// by the display dimensions. The stored file dimensions are the raw (pre-EXIF)
// dimensions, which for orientations 5–8 (the 90°/270° rotations) are swapped
// relative to how the image is displayed, so display width/height are the file
// dimensions with width and height exchanged.
//
// fileWidth, fileHeight are the photo's stored (raw) pixel dimensions and
// orientation is its EXIF orientation (1–8). If the inputs are degenerate (not a
// 4-element box, or non-positive dimensions) the box is returned unchanged so a
// missing/zero dimension never produces NaN/Inf coordinates.
func NormalizeBBox(bbox [4]float64, fileWidth, fileHeight, orientation int) [4]float64 {
	if fileWidth <= 0 || fileHeight <= 0 {
		return bbox
	}

	displayWidth, displayHeight := fileWidth, fileHeight
	if orientation >= 5 && orientation <= 8 {
		displayWidth, displayHeight = fileHeight, fileWidth
	}

	x1 := bbox[0] / float64(displayWidth)
	y1 := bbox[1] / float64(displayHeight)
	x2 := bbox[2] / float64(displayWidth)
	y2 := bbox[3] / float64(displayHeight)

	return [4]float64{x1, y1, x2 - x1, y2 - y1}
}
