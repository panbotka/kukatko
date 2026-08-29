package exif

import (
	"os"

	"github.com/rwcarlsen/goexif/exif"
)

// FileOrientation reads only the orientation tag of the file at path, with pure-Go
// parsers and without any shell-out. It returns 1-8 as found in the file and 0 when
// there is no tag to read — no EXIF and no XMP, an unreadable one, an unopenable
// file — so a caller can tell "the file says nothing" from "the file says upright".
//
// EXIF is read first and keeps precedence; only when it holds no orientation is the
// XMP packet consulted (see xmpOrientation). Both are read because a file may carry
// the tag in either: the batch that exposed this were JPEGs with no EXIF block at
// all whose orientation lived only in XMP, and reading EXIF alone made this weaker
// than the exiftool-based reader the ingest path uses — the two disagreed, and the
// picture went to the face detector lying on its side.
//
// It exists for the one question a caller has about a file it is about to hand to
// something that does not read orientation itself: does this image still need its
// orientation applied? That has to be answered from the bytes being handed over,
// not from the catalogue: an intermediate copy (imgconvert's HEIC/RAW/video JPEG)
// may already carry the rotation in its pixels, and applying the original's tag on
// top of it would turn the picture a second time. Extract is the full reader; this
// is the cheap, single-tag one.
func FileOrientation(path string) int {
	if orientation, ok := exifFileOrientation(path); ok {
		return orientation
	}
	return xmpOrientation(path)
}

// exifFileOrientation reads the EXIF orientation tag of the file at path and
// reports whether it found a usable one (1-8). An unopenable file, a file with no
// EXIF block and an out-of-range value are all "not found", so the caller can move
// on to the other place an orientation can live.
func exifFileOrientation(path string) (int, bool) {
	// G304: path is the storage/imgconvert file the caller is about to read.
	file, err := os.Open(path) //nolint:gosec
	if err != nil {
		return 0, false
	}
	defer func() { _ = file.Close() }()

	decoded, err := exif.Decode(file)
	if err != nil {
		return 0, false
	}
	orientation, ok := tagInt(decoded, exif.Orientation)
	if !ok || orientation < 1 || orientation > 8 {
		return 0, false
	}
	return orientation, true
}
