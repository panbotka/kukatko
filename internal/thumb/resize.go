package thumb

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // register PNG decoder
	"os"

	_ "golang.org/x/image/webp" // register WebP decoder

	"golang.org/x/image/draw"

	"github.com/panbotka/kukatko/internal/imgconvert"
)

// decodeAndOrient resolves srcPath to a directly decodable image — shelling out
// via imgconvert for HEIC/RAW originals — decodes it once with the registered
// JPEG/PNG/WebP decoders, and applies the EXIF orientation so the returned
// image is in display orientation. The intermediate JPEG (if any) is cleaned up
// before returning.
func decodeAndOrient(ctx context.Context, srcPath string, orientation int) (image.Image, error) {
	decPath, cleanup, err := imgconvert.EnsureDecodable(ctx, srcPath)
	if err != nil {
		return nil, fmt.Errorf("thumb: prepare %s: %w", srcPath, err)
	}
	defer cleanup()

	f, err := os.Open(decPath) //nolint:gosec // G304: decPath is from the trusted storage layer or imgconvert temp.
	if err != nil {
		return nil, fmt.Errorf("thumb: open %s: %w", decPath, err)
	}
	defer func() { _ = f.Close() }()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("thumb: decode %s: %w", srcPath, err)
	}
	return applyOrientation(img, orientation), nil
}

// applyOrientation rotates and/or flips img according to the EXIF orientation
// value (1-8). Orientations 5-8 swap the output width and height. Values <= 1
// or > 8 are treated as a no-op and img is returned unchanged.
func applyOrientation(img image.Image, orientation int) image.Image {
	if orientation <= 1 || orientation > 8 {
		return img
	}
	bounds := img.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()

	dstW, dstH := srcW, srcH
	switch orientation {
	case 5, 6, 7, 8:
		dstW, dstH = srcH, srcW
	}

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for y := range dstH {
		for x := range dstW {
			sx, sy := mapOrientation(orientation, x, y, srcW, srcH)
			dst.Set(x, y, img.At(bounds.Min.X+sx, bounds.Min.Y+sy))
		}
	}
	return dst
}

// mapOrientation returns the source pixel coordinate that should appear at
// destination (x, y) after applying the given EXIF orientation transform to a
// srcW × srcH image. Behaviour is defined for orientation values 2-8;
// applyOrientation handles the no-op cases before calling this.
func mapOrientation(orientation, x, y, srcW, srcH int) (sx, sy int) {
	switch orientation {
	case 2: // Mirror horizontal.
		return srcW - 1 - x, y
	case 3: // Rotate 180.
		return srcW - 1 - x, srcH - 1 - y
	case 4: // Mirror vertical.
		return x, srcH - 1 - y
	case 5: // Transpose.
		return y, x
	case 6: // Rotate 90 CW.
		return y, srcH - 1 - x
	case 7: // Transverse.
		return srcW - 1 - y, srcH - 1 - x
	case 8: // Rotate 270 CW (= 90 CCW).
		return srcW - 1 - y, x
	}
	return x, y
}

// resizeForSpec returns a new image rendered from img according to spec: a
// max-side fit (no upscaling) or a center-cropped square.
func resizeForSpec(img image.Image, spec sizeSpec) (image.Image, error) {
	switch spec.Mode {
	case modeFit:
		return resizeFit(img, spec.Max), nil
	case modeCropSquare:
		return resizeCropSquare(img, spec.Max), nil
	default:
		return nil, fmt.Errorf("thumb: invalid mode %q", spec.Mode)
	}
}

// resizeFit returns an image whose longest side is at most maxSide, preserving
// aspect ratio. Images already within the bound are returned unchanged (no
// upscaling).
func resizeFit(img image.Image, maxSide int) image.Image {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= maxSide && h <= maxSide {
		return img
	}
	var newW, newH int
	if w >= h {
		newW = maxSide
		newH = h * maxSide / w
	} else {
		newH = maxSide
		newW = w * maxSide / h
	}
	newW = max(newW, 1)
	newH = max(newH, 1)
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Src, nil)
	return dst
}

// resizeCropSquare center-crops img to the largest square that fits and resizes
// that crop to side × side using CatmullRom interpolation.
func resizeCropSquare(img image.Image, side int) image.Image {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	sq := min(w, h)
	x0 := bounds.Min.X + (w-sq)/2
	y0 := bounds.Min.Y + (h-sq)/2
	cropRect := image.Rect(x0, y0, x0+sq, y0+sq)
	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, cropRect, draw.Src, nil)
	return dst
}

// encodeJPEG encodes img as a JPEG byte slice at the given quality.
func encodeJPEG(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("thumb: encode jpeg: %w", err)
	}
	return buf.Bytes(), nil
}
