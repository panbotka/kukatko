package exif

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestExtract_errors verifies Extract reports an error only for an empty path or
// a file that cannot be stat'ed.
func TestExtract_errors(t *testing.T) {
	t.Parallel()

	if _, err := Extract(context.Background(), ""); err == nil {
		t.Error("empty path should error")
	}
	if _, err := Extract(context.Background(), filepath.Join(t.TempDir(), "missing.jpg")); err == nil {
		t.Error("missing file should error")
	}
}

// TestExtract_exifSource confirms a file carrying EXIF resolves TakenAt from EXIF
// and reports SourceExif, independent of whether exiftool is installed (both the
// primary and fallback paths converge on the same result).
func TestExtract_exifSource(t *testing.T) {
	t.Parallel()

	meta, err := Extract(context.Background(), filepath.Join("testdata", "sample_gps.jpg"))
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if meta.TakenAtSource != SourceExif {
		t.Errorf("TakenAtSource = %q, want exif", meta.TakenAtSource)
	}
	if meta.TakenAt == nil {
		t.Fatal("TakenAt should be set from EXIF")
	}
	floatEq(t, "Lat", meta.Lat, 39.91555555555556)
}

// TestExtract_filenameSource confirms that an EXIF-free file with a date-encoded
// name resolves TakenAt from the filename and reports SourceFilename.
func TestExtract_filenameSource(t *testing.T) {
	t.Parallel()

	path := writePNG(t, filepath.Join(t.TempDir(), "VID_20220809_181500.png"), 2, 2)
	meta, err := Extract(context.Background(), path)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if meta.TakenAtSource != SourceFilename {
		t.Errorf("TakenAtSource = %q, want filename", meta.TakenAtSource)
	}
	want := time.Date(2022, 8, 9, 18, 15, 0, 0, time.UTC)
	if meta.TakenAt == nil || !meta.TakenAt.Equal(want) {
		t.Errorf("TakenAt = %v, want %v", meta.TakenAt, want)
	}
}

// TestExtract_unknownSource confirms a file with neither EXIF nor a date-encoded
// name reports SourceUnknown and a nil TakenAt.
func TestExtract_unknownSource(t *testing.T) {
	t.Parallel()

	path := writePNG(t, filepath.Join(t.TempDir(), "plain-image.png"), 2, 2)
	meta, err := Extract(context.Background(), path)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if meta.TakenAtSource != SourceUnknown {
		t.Errorf("TakenAtSource = %q, want unknown", meta.TakenAtSource)
	}
	if meta.TakenAt != nil {
		t.Errorf("TakenAt = %v, want nil", meta.TakenAt)
	}
}
