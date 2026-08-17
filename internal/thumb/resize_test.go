package thumb

import (
	"errors"
	"image"
	"testing"
)

// TestResizeFit covers downscaling (aspect preserved) and the no-upscale rule.
func TestResizeFit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		srcW, srcH, max int
		wantW, wantH    int
	}{
		{"landscape downscale", 1200, 900, 600, 600, 450},
		{"portrait downscale", 900, 1200, 600, 450, 600},
		{"square downscale", 1000, 1000, 250, 250, 250},
		{"no upscale", 400, 300, 720, 400, 300},
		{"already at bound", 720, 480, 720, 720, 480},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resizeFit(image.NewRGBA(image.Rect(0, 0, tc.srcW, tc.srcH)), tc.max)
			b := got.Bounds()
			if b.Dx() != tc.wantW || b.Dy() != tc.wantH {
				t.Errorf("resizeFit(%dx%d, %d) = %dx%d, want %dx%d",
					tc.srcW, tc.srcH, tc.max, b.Dx(), b.Dy(), tc.wantW, tc.wantH)
			}
		})
	}
}

// TestResizeCropSquare confirms output is always side × side regardless of the
// source aspect ratio.
func TestResizeCropSquare(t *testing.T) {
	t.Parallel()
	tests := []struct {
		srcW, srcH, side int
	}{
		{1000, 600, 224},
		{600, 1000, 100},
		{500, 500, 500},
	}
	for _, tc := range tests {
		got := resizeCropSquare(image.NewRGBA(image.Rect(0, 0, tc.srcW, tc.srcH)), tc.side)
		b := got.Bounds()
		if b.Dx() != tc.side || b.Dy() != tc.side {
			t.Errorf("resizeCropSquare(%dx%d, %d) = %dx%d, want square %d",
				tc.srcW, tc.srcH, tc.side, b.Dx(), b.Dy(), tc.side)
		}
	}
}

// TestResizeForSpec_invalidMode confirms an unknown mode is rejected.
func TestResizeForSpec_invalidMode(t *testing.T) {
	t.Parallel()
	_, err := resizeForSpec(image.NewRGBA(image.Rect(0, 0, 4, 4)), sizeSpec{Max: 10, Mode: "bogus"})
	if err == nil {
		t.Error("resizeForSpec should reject an unknown mode")
	}
}

// TestValidateHash covers hashes that are too short, non-hex, or valid.
func TestValidateHash(t *testing.T) {
	t.Parallel()
	tests := []struct {
		hash    string
		wantErr bool
	}{
		{testHash, false},
		{"abcdef", false},
		{"abcde", true},
		{"", true},
		{"ABCDEF", true}, // uppercase is rejected (storage hashes are lowercase)
		{"abcdeg", true},
	}
	for _, tc := range tests {
		err := validateHash(tc.hash)
		if (err != nil) != tc.wantErr {
			t.Errorf("validateHash(%q) err = %v, wantErr = %v", tc.hash, err, tc.wantErr)
		}
		if err != nil && !errors.Is(err, ErrInvalidHash) {
			t.Errorf("validateHash(%q) err = %v, want ErrInvalidHash", tc.hash, err)
		}
	}
}
