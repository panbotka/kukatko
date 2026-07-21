package imgconvert

import (
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// writePNG encodes a width×height RGBA image to a temp PNG file and returns its
// path, so the pixel-bound guard can be exercised on a real decodable header.
func writePNG(t *testing.T, width, height int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "img.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png: %v", err)
	}
	defer func() { _ = f.Close() }()
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return path
}

// TestEnforcePixelBound covers the cap decision on a real 100×80 (8000-pixel)
// source: within/at the cap passes, over it is rejected with ErrImageTooLarge,
// and a non-positive cap disables the check.
func TestEnforcePixelBound(t *testing.T) {
	t.Parallel()
	path := writePNG(t, 100, 80) // 8000 pixels

	tests := []struct {
		name      string
		maxPixels int64
		wantErr   bool
	}{
		{"within cap", 10000, false},
		{"exactly at cap", 8000, false},
		{"one under the source size rejected", 7999, true},
		{"tiny cap rejected", 1, true},
		{"zero disables cap", 0, false},
		{"negative disables cap", -1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := EnforcePixelBound(path, tt.maxPixels)
			if (err != nil) != tt.wantErr {
				t.Fatalf("EnforcePixelBound(cap=%d) err = %v, wantErr = %v", tt.maxPixels, err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, ErrImageTooLarge) {
				t.Errorf("EnforcePixelBound(cap=%d) err = %v, want ErrImageTooLarge", tt.maxPixels, err)
			}
		})
	}
}

// TestEnforcePixelBound_missingFile reports the open error rather than silently
// passing: a source that should exist but does not is a real failure, and it
// must not be mistaken for an oversize rejection.
func TestEnforcePixelBound_missingFile(t *testing.T) {
	t.Parallel()
	err := EnforcePixelBound(filepath.Join(t.TempDir(), "nope.png"), 100)
	if err == nil {
		t.Fatal("EnforcePixelBound on a missing file should error")
	}
	if errors.Is(err, ErrImageTooLarge) {
		t.Errorf("missing-file error = %v, should not be ErrImageTooLarge", err)
	}
}

// TestEnforcePixelBound_unparseableHeader passes a non-image file: an unreadable
// header is not an oversize rejection, so the guard returns nil and leaves the
// caller's own decode to surface the real error.
func TestEnforcePixelBound_unparseableHeader(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "not-an-image.txt")
	if err := os.WriteFile(path, []byte("this is definitely not an image header"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := EnforcePixelBound(path, 1); err != nil {
		t.Errorf("EnforcePixelBound on an unparseable header = %v, want nil", err)
	}
}
