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
			got := resizeFit(image.NewRGBA(image.Rect(0, 0, tc.srcW, tc.srcH)), tc.srcW, tc.srcH, tc.max)
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
		got := resizeCropSquare(image.NewRGBA(image.Rect(0, 0, tc.srcW, tc.srcH)), tc.srcW, tc.srcH, tc.side)
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

// TestCentredSquare verifies the crop of a tile: the exact centred square when
// the source is the picture itself, and the same square rounded to the pixels
// of a smaller copy (here half size, offset bounds) when it is not.
func TestCentredSquare(t *testing.T) {
	t.Parallel()
	if got, want := centredSquare(image.Rect(0, 0, 1000, 600), 1000, 600), image.Rect(200, 0, 800, 600); got != want {
		t.Errorf("full-size square = %v, want %v", got, want)
	}
	if got, want := centredSquare(image.Rect(10, 10, 510, 310), 1000, 600), image.Rect(110, 10, 410, 310); got != want {
		t.Errorf("half-size square = %v, want %v", got, want)
	}
}

// TestRenderSpec_geometryFromFullSize verifies a size's dimensions come from the
// picture's full size, not from the copy it is scaled from: a fit rendered from
// a slightly smaller rendition has the pixel size a direct resize would have.
func TestRenderSpec_geometryFromFullSize(t *testing.T) {
	t.Parallel()
	// 2600×1700 → fit_1920 is 1920×1255; from a 2560×1673 copy it would be 1254.
	got, err := renderSpec(image.NewRGBA(image.Rect(0, 0, 2560, 1673)), sizes["fit_1920"], 2600, 1700)
	if err != nil {
		t.Fatal(err)
	}
	if b := got.Bounds(); b.Dx() != 1920 || b.Dy() != 1255 {
		t.Errorf("renderSpec = %dx%d, want 1920x1255", b.Dx(), b.Dy())
	}
}
