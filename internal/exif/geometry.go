package exif

// QuarterTurn reports whether an EXIF orientation tag turns the image a quarter
// turn — the only case that exchanges its width and height. Values 5-8
// (transpose, 90° CW, transverse, 270° CW) do; 1-4 (upright, the mirrors and the
// 180° flip), an absent tag (0) and any out-of-range value do not.
func QuarterTurn(orientation int) bool {
	return orientation >= 5 && orientation <= 8
}

// RawDimensions converts a pair of ALREADY ORIENTED ("display") pixel dimensions
// back to the file's stored, pre-rotation ones by undoing a quarter turn.
//
// It exists because Kukátko's whole geometry stack — the thumbnailer, which
// decodes the untouched original and applies the orientation itself; the
// frontend's `displayFrame`; facejob.NormalizeBBox — assumes photos.file_width /
// file_height describe the bytes on disk and file_orientation is the raw tag that
// still has to be applied to them. A source that has already applied the tag to
// its dimensions (PhotoPrism does: its MediaFile.Width()/Height() swap the two
// for orientations 5-8, and photo-sorter copied those values) must therefore be
// converted on the way in, or the pair contradicts itself and every consumer
// rotates a second time.
//
// The transform is its own inverse, so it also converts stored dimensions to
// display ones — but callers wanting the display frame should say so at the point
// of use rather than reusing this name.
func RawDimensions(width, height, orientation int) (int, int) {
	if QuarterTurn(orientation) {
		return height, width
	}
	return width, height
}
