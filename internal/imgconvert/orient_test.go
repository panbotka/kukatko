package imgconvert

import (
	"image"
	"image/color"
	"testing"
)

// TestOrient_dimensions confirms which orientations swap width and height and
// which are no-ops (including out-of-range values).
func TestOrient_dimensions(t *testing.T) {
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
		got := Orient(src, tc.orientation)
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

// TestOrient_pixelMapping places a marker at the top-left and checks
// where each orientation transform relocates it.
func TestOrient_pixelMapping(t *testing.T) {
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
		got := Orient(src, tc.orientation)
		r, g, b, a := got.At(tc.wantX, tc.wantY).RGBA()
		if r>>8 != 255 || g>>8 != 0 || b>>8 != 0 || a>>8 != 255 {
			t.Errorf("orientation %d: marker not at (%d,%d); got RGBA (%d,%d,%d,%d)",
				tc.orientation, tc.wantX, tc.wantY, r>>8, g>>8, b>>8, a>>8)
		}
	}
}
