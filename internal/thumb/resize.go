package thumb

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif" // register GIF decoder
	"image/jpeg"
	_ "image/png" // register PNG decoder
	"os"

	_ "golang.org/x/image/bmp"  // register BMP decoder
	_ "golang.org/x/image/tiff" // register TIFF decoder
	_ "golang.org/x/image/webp" // register WebP decoder

	"golang.org/x/image/draw"

	"github.com/panbotka/kukatko/internal/imgconvert"
)

// decodeAndOrient resolves srcPath to a directly decodable image — shelling out
// via imgconvert for HEIC/RAW originals — decodes it once with the registered
// JPEG/PNG/WebP decoders, and applies the EXIF orientation (imgconvert.Orient, the
// one implementation of that transform) so the returned image is in display
// orientation. The intermediate JPEG (if any) is cleaned up before returning.
//
// Before the bitmap is allocated, imgconvert.ReserveDecode gates it: maxPixels
// caps the source dimensions (an image whose width×height exceeds it is rejected
// with imgconvert.ErrImageTooLarge; a non-positive maxPixels disables the cap),
// and the decoded bytes plus extra(header) are reserved from budget, waiting
// while the process-wide budget is spent. The returned release gives that
// reservation back and must be called once the image is no longer used; it is
// a no-op when the call fails.
func decodeAndOrient(
	ctx context.Context, srcPath string, orientation int, maxPixels int64,
	budget *imgconvert.DecodeBudget, extra func(image.Config) int64,
) (image.Image, func(), error) {
	noop := func() {}
	decPath, cleanup, err := imgconvert.EnsureDecodable(ctx, srcPath)
	if err != nil {
		return nil, noop, fmt.Errorf("thumb: prepare %s: %w", srcPath, err)
	}
	defer cleanup()

	release, err := imgconvert.ReserveDecode(ctx, decPath, maxPixels, budget, extra)
	if err != nil {
		return nil, noop, fmt.Errorf("thumb: %s: %w", srcPath, err)
	}

	f, err := os.Open(decPath) //nolint:gosec // G304: decPath is from the trusted storage layer or imgconvert temp.
	if err != nil {
		release()
		return nil, noop, fmt.Errorf("thumb: open %s: %w", decPath, err)
	}
	defer func() { _ = f.Close() }()

	img, _, err := image.Decode(f)
	if err != nil {
		release()
		return nil, noop, fmt.Errorf("thumb: decode %s: %w", srcPath, err)
	}
	return imgconvert.Orient(img, orientation), release, nil
}

// resizeForSpec returns a new image rendered from img according to spec: a
// max-side fit (no upscaling) or a center-cropped square.
func resizeForSpec(img image.Image, spec sizeSpec) (image.Image, error) {
	b := img.Bounds()
	return renderSpec(img, spec, b.Dx(), b.Dy())
}

// renderSpec renders spec for a picture whose full size is w×h, scaling it from
// src — the picture itself, or a smaller copy of the whole of it (the cascade's
// pre-shrunk base or a larger rendition already made). The output's geometry is
// computed from w×h, never from src, so a size comes out the same to the pixel
// whichever copy it was scaled from.
func renderSpec(src image.Image, spec sizeSpec, w, h int) (image.Image, error) {
	switch spec.Mode {
	case modeFit:
		return resizeFit(src, w, h, spec.Max), nil
	case modeCropSquare:
		return resizeCropSquare(src, w, h, spec.Max), nil
	default:
		return nil, fmt.Errorf("thumb: invalid mode %q", spec.Mode)
	}
}

// resizeFit returns the w×h picture in src scaled so its longest side is at
// most maxSide, preserving aspect ratio (CatmullRom). A picture already within
// the bound is returned unchanged (no upscaling).
func resizeFit(src image.Image, w, h, maxSide int) image.Image {
	dw, dh := fitSize(w, h, maxSide)
	sb := src.Bounds()
	if sb.Dx() == dw && sb.Dy() == dh {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, sb, draw.Src, nil)
	return dst
}

// resizeCropSquare center-crops the w×h picture in src to the largest square
// that fits and resizes that crop to side × side using CatmullRom interpolation.
func resizeCropSquare(src image.Image, w, h, side int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, centredSquare(src.Bounds(), w, h), draw.Src, nil)
	return dst
}

// centredSquare returns the largest centred square of a w×h picture, in the
// coordinates of sb — a copy of the whole picture, possibly at a smaller scale.
// When sb is w×h itself this is the exact square; a smaller copy gets the same
// square rounded to its own pixels.
func centredSquare(sb image.Rectangle, w, h int) image.Rectangle {
	sq := min(w, h)
	x0, y0 := (w-sq)/2, (h-sq)/2
	scaleX := func(v int) int { return sb.Min.X + (v*sb.Dx()+w/2)/w }
	scaleY := func(v int) int { return sb.Min.Y + (v*sb.Dy()+h/2)/h }
	return image.Rect(scaleX(x0), scaleY(y0), scaleX(x0+sq), scaleY(y0+sq))
}

// encodeJPEG encodes img as a JPEG byte slice at the given quality.
func encodeJPEG(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("thumb: encode jpeg: %w", err)
	}
	return buf.Bytes(), nil
}
