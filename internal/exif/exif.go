// Package exif extracts photo metadata — capture time, GPS location,
// camera/lens identity, pixel dimensions, orientation and the full EXIF
// document — from image files during ingest.
//
// It mirrors photo-sorter's strategy and keeps the binary CGO-free: the primary
// path shells out to the `exiftool` subprocess (which understands every camera
// quirk and container, including HEIC and RAW), and a pure-Go fallback built on
// rwcarlsen/goexif handles plain JPEG/TIFF when exiftool is not installed. Both
// paths converge on the same Metadata value so callers never care which ran.
//
// The package is deliberately tolerant: a file without any EXIF (a screenshot
// PNG, a freshly exported JPEG) is not an error — Extract returns zero values
// for the missing fields and resolves the capture time from the filename when
// it can, marking the source accordingly.
package exif

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

// Source identifies where a photo's capture time (TakenAt) came from, so the
// catalogue can show how trustworthy it is and importers can prefer stronger
// sources on conflict.
type Source string

const (
	// SourceExif means TakenAt came from EXIF DateTimeOriginal (most reliable).
	SourceExif Source = "exif"
	// SourceFilename means TakenAt was parsed from the file name (best effort).
	SourceFilename Source = "filename"
	// SourceUnknown means no capture time could be determined.
	SourceUnknown Source = "unknown"
)

// Metadata is the normalised result of extracting one file's metadata. Optional
// scalar values use pointers so a genuinely missing value (nil) is
// distinguishable from a zero reading; plain strings/ints default to their zero
// value when absent. The field set maps 1:1 onto the photos.Photo columns.
type Metadata struct {
	// TakenAt is the capture time, nil when unknown. TakenAtSource records how
	// it was resolved.
	TakenAt       *time.Time
	TakenAtSource Source

	// Lat, Lng and Altitude are decimal GPS coordinates (degrees, metres), nil
	// when the file carries no usable GPS fix.
	Lat      *float64
	Lng      *float64
	Altitude *float64

	// Camera and exposure identity.
	CameraMake  string
	CameraModel string
	LensModel   string
	ISO         *int
	Aperture    *float64 // f-number, e.g. 2.8
	Exposure    string   // shutter speed as displayed, e.g. "1/125"
	FocalLength *float64 // millimetres

	// Pixel geometry. Orientation is the raw EXIF value (1-8), 0 when absent.
	Width       int
	Height      int
	Orientation int

	// Mime is the detected media type, e.g. "image/jpeg".
	Mime string

	// Exif is the full, JSON-able EXIF document (every tag exiftool or the
	// fallback parser surfaced), nil when the file has no EXIF at all.
	Exif map[string]any
}

// Extract reads the metadata of the file at path. It prefers the exiftool
// subprocess and falls back to the pure-Go parser when exiftool is unavailable
// or fails. Capture time is resolved from EXIF first, then the filename, and is
// otherwise left unknown.
//
// Extract returns an error only when path is empty or the file cannot be
// stat'ed; a readable file with no EXIF yields a Metadata with zero values for
// the missing fields and a nil error.
func Extract(ctx context.Context, path string) (Metadata, error) {
	if path == "" {
		return Metadata{}, errors.New("exif: path must not be empty")
	}
	if _, err := os.Stat(path); err != nil {
		return Metadata{}, fmt.Errorf("exif: stat %s: %w", path, err)
	}

	var meta Metadata
	if exiftoolAvailable() {
		if extracted, err := extractWithExiftool(ctx, path); err == nil {
			meta = extracted
		}
	}
	if meta.Exif == nil {
		// Either exiftool is absent or it failed/returned nothing useful; the
		// pure-Go fallback is always attempted as a backstop.
		meta = extractWithFallback(path)
	}

	resolveTakenAt(&meta, path)
	return meta, nil
}

// resolveTakenAt fills in TakenAt/TakenAtSource for files whose EXIF carried no
// capture time, attempting a filename date parse before giving up. It leaves an
// EXIF-sourced time untouched.
func resolveTakenAt(meta *Metadata, path string) {
	if meta.TakenAt != nil {
		meta.TakenAtSource = SourceExif
		return
	}
	if when, ok := parseFilenameDate(path); ok {
		meta.TakenAt = &when
		meta.TakenAtSource = SourceFilename
		return
	}
	meta.TakenAtSource = SourceUnknown
}
