package userpic_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/userpic"
)

// gradient builds a w×h image whose left half is black and whose right half is
// white, so a crop's position is visible in the result's own pixels.
func gradient(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			shade := uint8(0)
			if x >= w/2 {
				shade = 255
			}
			img.Set(x, y, color.RGBA{R: shade, G: shade, B: shade, A: 255})
		}
	}
	return img
}

// encodeJPEG renders img as JPEG bytes.
func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encoding the fixture: %v", err)
	}
	return buf.Bytes()
}

// decode reads back what Normalize produced, failing the test if it is not a
// decodable JPEG.
func decode(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("the normalized picture is not a JPEG: %v", err)
	}
	return img
}

func TestNormalize_cropsSquareAndBoundsTheSide(t *testing.T) {
	t.Parallel()

	out, err := userpic.Normalize(encodeJPEG(t, gradient(1600, 900)))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	bounds := decode(t, out).Bounds()
	if bounds.Dx() != bounds.Dy() {
		t.Errorf("normalized picture is %v, want a square", bounds)
	}
	if bounds.Dx() != userpic.MaxSide {
		t.Errorf("side = %d, want %d", bounds.Dx(), userpic.MaxSide)
	}
}

func TestNormalize_neverUpscales(t *testing.T) {
	t.Parallel()

	out, err := userpic.Normalize(encodeJPEG(t, gradient(64, 48)))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	// The square inside a 64×48 frame is 48 a side; inventing pixels to reach
	// MaxSide would only cost bytes.
	if side := decode(t, out).Bounds().Dx(); side != 48 {
		t.Errorf("side = %d, want 48 (the source's own square)", side)
	}
}

func TestNormalize_acceptsPNG(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := png.Encode(&buf, gradient(300, 300)); err != nil {
		t.Fatalf("encoding the PNG fixture: %v", err)
	}
	out, err := userpic.Normalize(buf.Bytes())
	if err != nil {
		t.Fatalf("Normalize(png): %v", err)
	}
	// Whatever went in, a JPEG comes out: the stored picture has one format.
	decode(t, out)
}

func TestNormalize_refusesAFormatTheLibraryDoesNotAccept(t *testing.T) {
	t.Parallel()

	// GIF decodes in this binary (the thumbnailer registers it), which is exactly
	// why the accepted set is a whitelist rather than "whatever decoded".
	var buf bytes.Buffer
	if err := gif.Encode(&buf, gradient(100, 100), nil); err != nil {
		t.Fatalf("encoding the GIF fixture: %v", err)
	}
	if _, err := userpic.Normalize(buf.Bytes()); !errors.Is(err, userpic.ErrUnsupportedFormat) {
		t.Errorf("Normalize(gif) error = %v, want ErrUnsupportedFormat", err)
	}
}

func TestNormalize_refusesBytesThatAreNotAnImage(t *testing.T) {
	t.Parallel()

	if _, err := userpic.Normalize([]byte("not a picture at all")); !errors.Is(err, userpic.ErrUnsupportedFormat) {
		t.Errorf("error = %v, want ErrUnsupportedFormat", err)
	}
}

func TestNormalize_refusesAnOversizedPicture(t *testing.T) {
	t.Parallel()

	oversized := make([]byte, userpic.MaxUploadBytes+1)
	if _, err := userpic.Normalize(oversized); !errors.Is(err, userpic.ErrTooLarge) {
		t.Errorf("error = %v, want ErrTooLarge", err)
	}
}

func TestReadUpload_stopsOneByteOverTheLimit(t *testing.T) {
	t.Parallel()

	source := strings.NewReader(strings.Repeat("x", int(userpic.MaxUploadBytes)+64))
	if _, err := userpic.ReadUpload(source); !errors.Is(err, userpic.ErrTooLarge) {
		t.Errorf("error = %v, want ErrTooLarge", err)
	}
}

func TestReadUpload_acceptsExactlyTheLimit(t *testing.T) {
	t.Parallel()

	source := strings.NewReader(strings.Repeat("x", int(userpic.MaxUploadBytes)))
	data, err := userpic.ReadUpload(source)
	if err != nil {
		t.Fatalf("ReadUpload: %v", err)
	}
	if int64(len(data)) != userpic.MaxUploadBytes {
		t.Errorf("read %d bytes, want %d", len(data), userpic.MaxUploadBytes)
	}
}
