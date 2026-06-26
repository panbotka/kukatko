package thumb

import (
	"errors"
	"image"
	"image/color"
	"testing"
)

// TestApplyOrientation_dimensions confirms which orientations swap width and
// height and which are no-ops (including out-of-range values).
func TestApplyOrientation_dimensions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		orientation int
		wantSwap    bool
	}{
		{0, false}, {1, false}, {2, false}, {3, false}, {4, false},
		{5, true}, {6, true}, {7, true}, {8, true}, {9, false},
	}
	src := image.NewRGBA(image.Rect(0, 0, 10, 4))
	for _, tc := range tests {
		got := applyOrientation(src, tc.orientation)
		b := got.Bounds()
		wantW, wantH := 10, 4
		if tc.wantSwap {
			wantW, wantH = 4, 10
		}
		if b.Dx() != wantW || b.Dy() != wantH {
			t.Errorf("orientation %d: got %dx%d, want %dx%d", tc.orientation, b.Dx(), b.Dy(), wantW, wantH)
		}
	}
}

// TestApplyOrientation_pixelMapping places a marker at the top-left and checks
// where each orientation transform relocates it.
func TestApplyOrientation_pixelMapping(t *testing.T) {
	t.Parallel()
	const w, h = 4, 3
	marker := color.RGBA{R: 255, G: 0, B: 0, A: 255}
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			src.Set(x, y, color.RGBA{R: 10, G: 10, B: 10, A: 255})
		}
	}
	src.Set(0, 0, marker)

	tests := []struct {
		orientation  int
		wantX, wantY int
	}{
		{2, w - 1, 0},     // mirror horizontal
		{3, w - 1, h - 1}, // rotate 180
		{4, 0, h - 1},     // mirror vertical
		{5, 0, 0},         // transpose
		{6, h - 1, 0},     // rotate 90 CW
		{7, h - 1, w - 1}, // transverse
		{8, 0, w - 1},     // rotate 90 CCW
	}
	for _, tc := range tests {
		got := applyOrientation(src, tc.orientation)
		r, g, b, a := got.At(tc.wantX, tc.wantY).RGBA()
		if r>>8 != 255 || g>>8 != 0 || b>>8 != 0 || a>>8 != 255 {
			t.Errorf("orientation %d: marker not at (%d,%d); got RGBA (%d,%d,%d,%d)",
				tc.orientation, tc.wantX, tc.wantY, r>>8, g>>8, b>>8, a>>8)
		}
	}
}

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
