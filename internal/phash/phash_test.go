package phash

import (
	"image"
	"image/color"
	"math/bits"
	"testing"
)

// solidImage returns a width×height image filled with a single gray level.
func solidImage(width, height int, level uint8) image.Image {
	img := image.NewGray(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetGray(x, y, color.Gray{Y: level})
		}
	}
	return img
}

// splitImage returns a width×height image split in half along the given axis:
// "vertical" makes the left half dark and the right half light, "horizontal"
// makes the top half dark and the bottom half light. A step edge has strong,
// stable low-frequency DCT energy, so its pHash survives rescaling — unlike a
// smooth gradient, whose coefficients hover near zero and flip on rounding.
func splitImage(width, height int, axis string) image.Image {
	img := image.NewGray(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			light := x >= width/2
			if axis == "horizontal" {
				light = y >= height/2
			}
			level := uint8(20)
			if light {
				level = 235
			}
			img.SetGray(x, y, color.Gray{Y: level})
		}
	}
	return img
}

// vSplit is a vertically split (left dark, right light) image of the given size.
func vSplit(width, height int) image.Image {
	return splitImage(width, height, "vertical")
}

// TestDistance_knownBits checks the Hamming distance over a few hand-picked bit
// patterns including the signed-integer boundary.
func TestDistance_knownBits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a, b int64
		want int
	}{
		{"identical", 0, 0, 0},
		{"single bit", 0, 1, 1},
		{"three bits", 0b1011, 0, 3},
		{"sign bit only", -9223372036854775808, 0, 1},
		{"all bits", 0, -1, 64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Distance(tt.a, tt.b); got != tt.want {
				t.Errorf("Distance(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestDistance_symmetric verifies the distance is symmetric for arbitrary
// inputs.
func TestDistance_symmetric(t *testing.T) {
	t.Parallel()
	const a, b int64 = 0x0123456789abcdef, -0x7766554433221101
	if Distance(a, b) != Distance(b, a) {
		t.Errorf("Distance not symmetric: %d vs %d", Distance(a, b), Distance(b, a))
	}
}

// TestCompute_identicalImagesMatch verifies the same image hashes to the same
// value (zero distance) for both hashes.
func TestCompute_identicalImagesMatch(t *testing.T) {
	t.Parallel()
	img := vSplit(64, 48)
	a := Compute(img)
	b := Compute(img)
	if a != b {
		t.Fatalf("Compute not deterministic: %+v vs %+v", a, b)
	}
	if Distance(a.Phash, b.Phash) != 0 || Distance(a.Dhash, b.Dhash) != 0 {
		t.Errorf("identical images differ: phash=%d dhash=%d",
			Distance(a.Phash, b.Phash), Distance(a.Dhash, b.Dhash))
	}
}

// TestCompute_similarImagesAreClose verifies a slightly down-scaled copy of an
// image stays within a small Hamming distance, while a structurally different
// image is far away.
func TestCompute_similarImagesAreClose(t *testing.T) {
	t.Parallel()
	original := Compute(vSplit(128, 96))
	scaled := Compute(vSplit(64, 48)) // same vertical split, half resolution
	rotated := Compute(splitImage(128, 96, "horizontal"))

	if d := Distance(original.Phash, scaled.Phash); d > 6 {
		t.Errorf("scaled copy too far in pHash: distance %d", d)
	}
	if d := Distance(original.Phash, rotated.Phash); d < 8 {
		t.Errorf("horizontal split unexpectedly close to vertical in pHash: distance %d", d)
	}
}

// TestCompute_solidImageIsStable verifies a flat image produces a defined,
// repeatable hash (no panic on the degenerate all-equal DCT block).
func TestCompute_solidImageIsStable(t *testing.T) {
	t.Parallel()
	a := Compute(solidImage(16, 16, 128))
	b := Compute(solidImage(40, 30, 128))
	// Different sizes of the same flat color must yield identical hashes.
	if a != b {
		t.Errorf("flat images of different sizes differ: %+v vs %+v", a, b)
	}
}

// TestCompute_tinyImage verifies a 1x1 source does not panic and yields a
// 64-bit-wide result.
func TestCompute_tinyImage(t *testing.T) {
	t.Parallel()
	h := Compute(solidImage(1, 1, 50))
	// Sanity: the popcounts are well defined (0..64).
	if bits.OnesCount64(uint64(h.Phash)) > 64 || bits.OnesCount64(uint64(h.Dhash)) > 64 {
		t.Fatalf("unexpected hash population: %+v", h)
	}
}
