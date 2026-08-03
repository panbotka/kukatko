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

// TestExtractNamed_readsTheGivenNameNotThePath is the regression test for the
// staged-upload bug: the bytes live under a generated temp name while the real
// name travels separately, and reading the date off the wrong one invented
// capture times out of random digits. The temp name here is verbatim the one
// that put a photo in the year 2879 in CI.
func TestExtractNamed_readsTheGivenNameNotThePath(t *testing.T) {
	t.Parallel()

	staged := writePNG(t, filepath.Join(t.TempDir(), "kukatko-ingest-2879101112"), 2, 2)

	tests := []struct {
		name       string
		upload     string
		wantSource Source
		wantTaken  *time.Time
	}{
		{
			name:       "undated upload name gets no date at all",
			upload:     "lake.jpg",
			wantSource: SourceUnknown,
		},
		{
			name:       "dated upload name is recovered through the staged file",
			upload:     "IMG_20230115_143052.jpg",
			wantSource: SourceFilename,
			wantTaken:  new(time.Date(2023, 1, 15, 14, 30, 52, 0, time.UTC)),
		},
		{
			name:       "no name at all is not an invitation to fall back to the path",
			upload:     "",
			wantSource: SourceUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			meta, err := ExtractNamed(context.Background(), staged, tc.upload)
			if err != nil {
				t.Fatalf("ExtractNamed() error = %v", err)
			}
			if meta.TakenAtSource != tc.wantSource {
				t.Errorf("TakenAtSource = %q, want %q", meta.TakenAtSource, tc.wantSource)
			}
			switch {
			case tc.wantTaken == nil && meta.TakenAt != nil:
				t.Errorf("TakenAt = %v, want nil (the staged name must not be read)", meta.TakenAt)
			case tc.wantTaken != nil && (meta.TakenAt == nil || !meta.TakenAt.Equal(*tc.wantTaken)):
				t.Errorf("TakenAt = %v, want %v", meta.TakenAt, *tc.wantTaken)
			}
		})
	}
}
