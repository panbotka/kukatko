package exif

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writePNG creates a small EXIF-free PNG of the given size at path, failing the
// test on any I/O error. It returns the path for convenience.
func writePNG(t *testing.T, path string, width, height int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png: %v", err)
	}
	defer func() { _ = file.Close() }()
	if err := png.Encode(file, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return path
}

// TestExtractWithFallback_sampleJPEG exercises the pure-Go parser against a real
// JPEG carrying GPS, camera and exposure EXIF, asserting the decoded values
// without requiring exiftool to be installed.
func TestExtractWithFallback_sampleJPEG(t *testing.T) {
	t.Parallel()

	meta := extractWithFallback(filepath.Join("testdata", "sample_gps.jpg"))

	if meta.Width != 500 || meta.Height != 375 {
		t.Errorf("dims = %dx%d, want 500x375", meta.Width, meta.Height)
	}
	if meta.Mime != "image/jpeg" {
		t.Errorf("Mime = %q, want image/jpeg", meta.Mime)
	}
	if meta.CameraMake != "NIKON CORPORATION" || meta.CameraModel != "NIKON D2H" {
		t.Errorf("camera = %q / %q", meta.CameraMake, meta.CameraModel)
	}
	floatEq(t, "Lat", meta.Lat, 39.91555555555556)
	floatEq(t, "Lng", meta.Lng, 116.39083333333333)
	floatEq(t, "Aperture", meta.Aperture, 4.5)
	floatEq(t, "FocalLength", meta.FocalLength, 23.33)
	if meta.Exposure != "1/125" {
		t.Errorf("Exposure = %q, want 1/125", meta.Exposure)
	}
	if meta.Orientation != 1 {
		t.Errorf("Orientation = %d, want 1", meta.Orientation)
	}
	want := time.Date(2003, 11, 23, 18, 7, 37, 0, time.UTC)
	if meta.TakenAt == nil || !meta.TakenAt.Equal(want) {
		t.Errorf("TakenAt = %v, want %v", meta.TakenAt, want)
	}
	if meta.Exif == nil {
		t.Error("Exif map should be populated for a file with EXIF")
	}
	if meta.ISO != nil || meta.Altitude != nil {
		t.Errorf("ISO/Altitude should be nil (absent in sample), got %v / %v", meta.ISO, meta.Altitude)
	}
}

// TestExtractWithFallback_noEXIF confirms an EXIF-free PNG yields geometry and
// MIME but zero values (and no error) for every EXIF-derived field.
func TestExtractWithFallback_noEXIF(t *testing.T) {
	t.Parallel()

	path := writePNG(t, filepath.Join(t.TempDir(), "plain.png"), 4, 3)
	meta := extractWithFallback(path)

	if meta.Mime != "image/png" {
		t.Errorf("Mime = %q, want image/png", meta.Mime)
	}
	if meta.Width != 4 || meta.Height != 3 {
		t.Errorf("dims = %dx%d, want 4x3", meta.Width, meta.Height)
	}
	if meta.TakenAt != nil || meta.Lat != nil || meta.Exif != nil {
		t.Errorf("EXIF-derived fields should be zero, got %+v", meta)
	}
	if meta.CameraMake != "" || meta.Orientation != 0 {
		t.Errorf("camera/orientation should be zero, got %q / %d", meta.CameraMake, meta.Orientation)
	}
}

// TestExposureFromTag_reduction is covered indirectly by the sample JPEG, but the
// gcd reduction helper is asserted directly here for the off-by-multiple case.
func TestGCDReduction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		a, b, want int64
	}{
		{a: 10, b: 1250, want: 10},
		{a: 6, b: 9, want: 3},
		{a: 7, b: 13, want: 1},
	}
	for _, tt := range tests {
		if got := gcd(tt.a, tt.b); got != tt.want {
			t.Errorf("gcd(%d,%d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
	if math.Abs(float64(absInt64(-5)-5)) != 0 {
		t.Errorf("absInt64(-5) = %d, want 5", absInt64(-5))
	}
}
