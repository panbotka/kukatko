package exif

import (
	"os"

	"github.com/rwcarlsen/goexif/exif"
)

// FileOrientation reads only the EXIF orientation tag of the file at path, with
// the pure-Go parser and without any shell-out. It returns 1-8 as found in the
// file and 0 when there is no tag to read — no EXIF block, an unreadable one, an
// unopenable file — so a caller can tell "the file says nothing" from "the file
// says upright".
//
// It exists for the one question a caller has about a file it is about to hand to
// something that does not read EXIF itself: does this image still need its
// orientation applied? That has to be answered from the bytes being handed over,
// not from the catalogue: an intermediate copy (imgconvert's HEIC/RAW/video JPEG)
// may already carry the rotation in its pixels, and applying the original's tag on
// top of it would turn the picture a second time. Extract is the full reader; this
// is the cheap, single-tag one.
func FileOrientation(path string) int {
	// G304: path is the storage/imgconvert file the caller is about to read.
	file, err := os.Open(path) //nolint:gosec
	if err != nil {
		return 0
	}
	defer func() { _ = file.Close() }()

	decoded, err := exif.Decode(file)
	if err != nil {
		return 0
	}
	orientation, ok := tagInt(decoded, exif.Orientation)
	if !ok || orientation < 1 || orientation > 8 {
		return 0
	}
	return orientation
}
