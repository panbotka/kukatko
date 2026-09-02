// Package blurhash computes the tiny blurred stand-in a client paints while a
// photo's real thumbnail is still loading.
//
// The encoding is BlurHash (woltapp/blurhash): a few dozen ASCII characters
// describing the image as a handful of DCT components, cheap enough to ride
// along in every photo of every list payload and decodable in the browser by a
// small, maintained TypeScript library. It is deliberately not a thumbnail —
// it carries no detail, only the colours and roughly where they are, which is
// exactly what a placeholder needs and what keeps it this small.
//
// Everything here is pure Go (no CGO): the image is scaled down with
// golang.org/x/image/draw and encoded by github.com/bbrks/go-blurhash. The
// source is scaled to WorkingSide first because the encoder's cost is
// proportional to the pixel count, and a placeholder computed from a 64-pixel
// rendition is indistinguishable from one computed from the full original.
package blurhash

import (
	"errors"
	"fmt"
	"image"
	"image/color"

	goblurhash "github.com/bbrks/go-blurhash"
	"golang.org/x/image/draw"
)

// WorkingSide is the longest side the source image is scaled to before it is
// encoded. Images already within it are used as they are (never upscaled).
const WorkingSide = 64

// Components counts along the wide and the narrow axis of the encoded gradient.
// Four by three is the shape the reference implementation recommends for an
// ordinary photograph: enough to place a horizon and a bright corner, few
// enough to keep the string in the tens of bytes a list payload can carry per
// photo. A square image gets the square grid instead, so neither axis is
// favoured.
const (
	componentsLong  = 4
	componentsShort = 3
	// aspectNumerator and aspectDenominator define how far from square an image
	// must be before its grid stops being square: a side must exceed the other by
	// 20% before it is treated as the long one.
	aspectNumerator   = 12
	aspectDenominator = 10
)

// ErrEmptyImage indicates Encode was given no image, or one with an empty
// bounding rectangle. There is nothing to describe, and it is a caller error
// rather than an encoding failure.
var ErrEmptyImage = errors.New("blurhash: empty image")

// Encode returns the BlurHash of img: a short ASCII string a client decodes
// into a blurred approximation of the picture.
//
// The image is expected in display orientation — the placeholder stands in for
// the rendition the user will see, so an original still holding an EXIF
// rotation must be oriented (imgconvert.Orient) before it is passed here, or
// the blurred stand-in appears sideways under an upright photo.
//
// Transparency is composited over white rather than ignored, so a PNG with a
// cut-out background yields the light placeholder its rendering suggests
// instead of whatever colour happens to sit under the transparent pixels.
//
// It returns ErrEmptyImage for a nil or zero-sized image and a wrapped encoder
// error otherwise; a returned hash is always non-empty.
func Encode(img image.Image) (string, error) {
	if img == nil {
		return "", ErrEmptyImage
	}
	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return "", ErrEmptyImage
	}
	x, y := componentsFor(bounds.Dx(), bounds.Dy())
	hash, err := goblurhash.Encode(x, y, downscale(img))
	if err != nil {
		return "", fmt.Errorf("blurhash: encoding %dx%d image: %w", bounds.Dx(), bounds.Dy(), err)
	}
	return hash, nil
}

// componentsFor picks the component grid for an image of w by h pixels: more
// components along the longer axis, a square grid when the image is roughly
// square. Matching the grid to the aspect ratio is what stops a wide panorama
// being described as if it were a portrait.
func componentsFor(w, h int) (x, y int) {
	switch {
	case w*aspectDenominator > h*aspectNumerator:
		return componentsLong, componentsShort
	case h*aspectDenominator > w*aspectNumerator:
		return componentsShort, componentsLong
	default:
		return componentsLong, componentsLong
	}
}

// downscale renders img into an opaque NRGBA image whose longest side is at
// most WorkingSide, preserving the aspect ratio and never upscaling. The
// destination is pre-filled with white and drawn over, so a source with an
// alpha channel is composited rather than read through.
//
// NRGBA is chosen deliberately: it is the encoder's fast path, so the scaled
// copy is read straight out of its pixel buffer instead of through the
// color.Color interface once per pixel.
func downscale(img image.Image) *image.NRGBA {
	bounds := img.Bounds()
	w, h := scaledSize(bounds.Dx(), bounds.Dy())
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)
	return dst
}

// scaledSize returns the size w by h is reduced to so its longest side is at
// most WorkingSide, keeping the aspect ratio and never upscaling. Both returned
// sides are at least 1, so an extreme aspect ratio still yields a usable image.
func scaledSize(w, h int) (int, int) {
	if w <= WorkingSide && h <= WorkingSide {
		return w, h
	}
	if w >= h {
		return WorkingSide, max(h*WorkingSide/w, 1)
	}
	return max(w*WorkingSide/h, 1), WorkingSide
}
