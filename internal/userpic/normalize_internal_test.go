package userpic

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

// TestPeekUpload verifies the header read: a real PNG's dimensions, and ok=false
// for bytes that are not an image.
func TestPeekUpload(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 9, 4))); err != nil {
		t.Fatal(err)
	}
	if cfg, ok := peekUpload(buf.Bytes()); !ok || cfg.Width != 9 || cfg.Height != 4 {
		t.Errorf("peekUpload(9x4 png) = (%+v, %v)", cfg, ok)
	}
	if _, ok := peekUpload([]byte("nope")); ok {
		t.Error("peekUpload(garbage) ok = true")
	}
}

// TestNormalizeCost verifies the working memory charged beyond the bitmap: a
// full-size RGBA copy (the orientation) plus the resampler's MaxSide × square ×
// 32-byte scratch.
func TestNormalizeCost(t *testing.T) {
	t.Parallel()
	got := normalizeCost(image.Config{Width: 1000, Height: 600})
	if want := int64(1000*600*4 + MaxSide*600*32); got != want {
		t.Errorf("normalizeCost(1000x600) = %d, want %d", got, want)
	}
}
