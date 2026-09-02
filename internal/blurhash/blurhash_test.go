package blurhash

import (
	"errors"
	"image"
	"image/color"
	"testing"

	goblurhash "github.com/bbrks/go-blurhash"
)

// solid returns an opaque w by h image filled with c.
func solid(w, h int, c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	return img
}

// halves returns a w by h image whose left half is left and right half is right.
func halves(w, h int, left, right color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := left
			if x >= w/2 {
				c = right
			}
			img.Set(x, y, c)
		}
	}
	return img
}

// decodedAverage decodes hash into a small image and returns the mean R, G and B
// of its pixels, so a test can assert the placeholder carries the right colour
// without depending on the exact string.
func decodedAverage(t *testing.T, hash string) (r, g, b float64) {
	t.Helper()
	const side = 8
	img, err := goblurhash.Decode(hash, side, side, 1)
	if err != nil {
		t.Fatalf("decoding %q: %v", hash, err)
	}
	var sr, sg, sb float64
	for y := range side {
		for x := range side {
			cr, cg, cb, _ := img.At(x, y).RGBA()
			sr += float64(cr >> 8)
			sg += float64(cg >> 8)
			sb += float64(cb >> 8)
		}
	}
	n := float64(side * side)
	return sr / n, sg / n, sb / n
}

func TestEncode_componentGridFollowsAspectRatio(t *testing.T) {
	tests := []struct {
		name  string
		w, h  int
		wantX int
		wantY int
	}{
		{name: "landscape", w: 400, h: 200, wantX: componentsLong, wantY: componentsShort},
		{name: "portrait", w: 200, h: 400, wantX: componentsShort, wantY: componentsLong},
		{name: "square", w: 300, h: 300, wantX: componentsLong, wantY: componentsLong},
		{name: "almost square stays square", w: 300, h: 280, wantX: componentsLong, wantY: componentsLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := Encode(solid(tt.w, tt.h, color.RGBA{R: 10, G: 120, B: 200, A: 255}))
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			x, y, err := goblurhash.Components(hash)
			if err != nil {
				t.Fatalf("Components(%q): %v", hash, err)
			}
			if x != tt.wantX || y != tt.wantY {
				t.Errorf("components = %dx%d, want %dx%d", x, y, tt.wantX, tt.wantY)
			}
			if want := 4 + 2*tt.wantX*tt.wantY; len(hash) != want {
				t.Errorf("len(hash) = %d, want %d", len(hash), want)
			}
			// The whole point of the field: it has to be cheap to carry per photo.
			if len(hash) > 64 {
				t.Errorf("hash of %d bytes is too large for a list payload", len(hash))
			}
		})
	}
}

func TestEncode_carriesTheDominantColour(t *testing.T) {
	hash, err := Encode(solid(120, 90, color.RGBA{R: 200, G: 30, B: 40, A: 255}))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	r, g, b := decodedAverage(t, hash)
	if r < 150 || g > 90 || b > 90 {
		t.Errorf("decoded average = (%.0f, %.0f, %.0f), want a red-dominant colour", r, g, b)
	}
}

func TestEncode_keepsCoarseStructure(t *testing.T) {
	hash, err := Encode(halves(120, 90, color.Black, color.White))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	img, err := goblurhash.Decode(hash, 8, 8, 1)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	leftR, _, _, _ := img.At(0, 4).RGBA()
	rightR, _, _, _ := img.At(7, 4).RGBA()
	if leftR >= rightR {
		t.Errorf("left luminance %d not darker than right %d; the gradient was lost", leftR>>8, rightR>>8)
	}
}

func TestEncode_compositesTransparencyOverWhite(t *testing.T) {
	// A fully transparent image whose colour channels are black: read through, it
	// would encode as black; composited over white it must be light.
	img := image.NewNRGBA(image.Rect(0, 0, 40, 40))
	for y := range 40 {
		for x := range 40 {
			img.SetNRGBA(x, y, color.NRGBA{R: 0, G: 0, B: 0, A: 0})
		}
	}
	hash, err := Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	r, g, b := decodedAverage(t, hash)
	if r < 200 || g < 200 || b < 200 {
		t.Errorf("decoded average = (%.0f, %.0f, %.0f), want near-white", r, g, b)
	}
}

func TestEncode_rejectsAnEmptyImage(t *testing.T) {
	tests := []struct {
		name string
		img  image.Image
	}{
		{name: "nil", img: nil},
		{name: "zero size", img: image.NewRGBA(image.Rect(0, 0, 0, 0))},
		{name: "zero height", img: image.NewRGBA(image.Rect(0, 0, 10, 0))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Encode(tt.img); !errors.Is(err, ErrEmptyImage) {
				t.Errorf("Encode error = %v, want ErrEmptyImage", err)
			}
		})
	}
}

func TestEncode_isDeterministic(t *testing.T) {
	img := halves(64, 64, color.RGBA{R: 20, G: 60, B: 90, A: 255}, color.RGBA{R: 240, G: 200, B: 30, A: 255})
	first, err := Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	second, err := Encode(img)
	if err != nil {
		t.Fatalf("Encode again: %v", err)
	}
	if first != second {
		t.Errorf("Encode is not deterministic: %q then %q", first, second)
	}
}

func TestScaledSize(t *testing.T) {
	tests := []struct {
		name         string
		w, h         int
		wantW, wantH int
	}{
		{name: "already small is untouched", w: 30, h: 20, wantW: 30, wantH: 20},
		{name: "landscape fits the long side", w: 640, h: 320, wantW: WorkingSide, wantH: WorkingSide / 2},
		{name: "portrait fits the long side", w: 320, h: 640, wantW: WorkingSide / 2, wantH: WorkingSide},
		{name: "extreme ratio keeps one pixel", w: 10000, h: 5, wantW: WorkingSide, wantH: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotW, gotH := scaledSize(tt.w, tt.h)
			if gotW != tt.wantW || gotH != tt.wantH {
				t.Errorf("scaledSize(%d, %d) = (%d, %d), want (%d, %d)",
					tt.w, tt.h, gotW, gotH, tt.wantW, tt.wantH)
			}
		})
	}
}

func TestEncode_scalesDownALargeImage(t *testing.T) {
	// A large source must not be encoded pixel by pixel: the working image is
	// bounded, which is what keeps the ingest cost of a placeholder negligible.
	hash, err := Encode(solid(4000, 3000, color.RGBA{R: 60, G: 160, B: 80, A: 255}))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if _, _, err := goblurhash.Components(hash); err != nil {
		t.Fatalf("Components(%q): %v", hash, err)
	}
	r, g, b := decodedAverage(t, hash)
	if g <= r || g <= b {
		t.Errorf("decoded average = (%.0f, %.0f, %.0f), want a green-dominant colour", r, g, b)
	}
}

// TestEncode_offsetBoundsAreHandled guards the sub-image case: an image.Image
// whose bounds do not start at the origin (a crop of another image) must encode
// from its own pixels rather than from an area that is not there.
func TestEncode_offsetBoundsAreHandled(t *testing.T) {
	full := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := range 100 {
		for x := range 100 {
			c := color.RGBA{R: 255, A: 255}
			if x >= 50 {
				c = color.RGBA{B: 255, A: 255}
			}
			full.Set(x, y, c)
		}
	}
	sub := full.SubImage(image.Rect(50, 0, 100, 100))
	hash, err := Encode(sub)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	r, _, b := decodedAverage(t, hash)
	if b <= r {
		t.Errorf("decoded average = (r %.0f, b %.0f), want the blue half only", r, b)
	}
}
