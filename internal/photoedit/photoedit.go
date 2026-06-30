// Package photoedit applies a photo's non-destructive adjustments (crop,
// rotation, brightness and contrast) to a decoded image, entirely in pure Go
// (no CGO). It is the rendering half of the photo_edits feature: edits are
// stored as parameters (internal/photos.Edit) and materialised on demand here —
// for example when the download endpoint serves the edited image while keeping
// the original file untouched.
//
// The brightness and contrast conventions match the CSS filters the frontend
// uses for its live preview, so the downloaded image matches what the user saw:
// brightness multiplies each channel by (1+brightness) and contrast scales each
// channel around the mid-point by (1+contrast); both default to 0 (no change)
// and are meaningful in the range [-1, 1].
package photoedit

import (
	"image"
	"image/draw"

	"github.com/panbotka/kukatko/internal/photos"
)

// IsIdentity reports whether the edit leaves an image unchanged: no crop, no
// rotation and neutral brightness and contrast. Callers use it to skip the
// decode/re-encode pipeline entirely and stream the original bytes instead.
func IsIdentity(e photos.Edit) bool {
	return !hasCrop(e) && e.Rotation == 0 && e.Brightness == 0 && e.Contrast == 0
}

// hasCrop reports whether the edit carries a (complete) crop rectangle. The crop
// fields are all-or-nothing, so testing one is sufficient, but all four are
// checked defensively.
func hasCrop(e photos.Edit) bool {
	return e.CropX != nil && e.CropY != nil && e.CropW != nil && e.CropH != nil
}

// Apply returns a new image with the edit's crop, rotation and colour
// adjustments applied, in that order. The input image is assumed to already be
// EXIF-oriented (use Orient first), so the edit's rotation is relative to the
// upright photo the user sees. A no-op edit returns img unchanged.
func Apply(img image.Image, e photos.Edit) image.Image {
	out := img
	if hasCrop(e) {
		out = crop(out, *e.CropX, *e.CropY, *e.CropW, *e.CropH)
	}
	out = rotate(out, e.Rotation)
	out = adjustColour(out, e.Brightness, e.Contrast)
	return out
}

// crop returns the sub-rectangle of img described by the normalised (0..1)
// origin (x, y) and size (w, h), clamped to the image bounds. The result is a
// fresh image whose bounds start at the origin. An empty or out-of-range
// rectangle falls back to the whole image so a malformed crop never yields a
// zero-size image.
func crop(img image.Image, x, y, w, h float64) image.Image {
	bounds := img.Bounds()
	imgW, imgH := bounds.Dx(), bounds.Dy()

	x0 := bounds.Min.X + clampInt(int(x*float64(imgW)+0.5), imgW)
	y0 := bounds.Min.Y + clampInt(int(y*float64(imgH)+0.5), imgH)
	x1 := bounds.Min.X + clampInt(int((x+w)*float64(imgW)+0.5), imgW)
	y1 := bounds.Min.Y + clampInt(int((y+h)*float64(imgH)+0.5), imgH)
	if x1 <= x0 || y1 <= y0 {
		return img
	}

	dst := image.NewRGBA(image.Rect(0, 0, x1-x0, y1-y0))
	draw.Draw(dst, dst.Bounds(), img, image.Pt(x0, y0), draw.Src)
	return dst
}

// rotate returns img rotated clockwise by the given degrees, which must be one
// of 0, 90, 180 or 270; any other value (including 0) returns img unchanged.
// Quarter turns swap the width and height.
func rotate(img image.Image, degrees int) image.Image {
	if degrees != 90 && degrees != 180 && degrees != 270 {
		return img
	}
	bounds := img.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()

	dstW, dstH := srcW, srcH
	if degrees == 90 || degrees == 270 {
		dstW, dstH = srcH, srcW
	}

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for dy := range dstH {
		for dx := range dstW {
			sx, sy := mapRotation(degrees, dx, dy, srcW, srcH)
			dst.Set(dx, dy, img.At(bounds.Min.X+sx, bounds.Min.Y+sy))
		}
	}
	return dst
}

// mapRotation returns the source pixel coordinate that should appear at
// destination (dx, dy) after rotating a srcW × srcH image clockwise by degrees
// (90, 180 or 270). rotate handles the no-op cases before calling this.
func mapRotation(degrees, dx, dy, srcW, srcH int) (sx, sy int) {
	switch degrees {
	case 90: // 90° clockwise: top row becomes the right column.
		return dy, srcH - 1 - dx
	case 180:
		return srcW - 1 - dx, srcH - 1 - dy
	case 270: // 270° clockwise (= 90° counter-clockwise).
		return srcW - 1 - dy, dx
	}
	return dx, dy
}

// adjustColour returns img with brightness and contrast applied per channel,
// matching the CSS brightness()/contrast() filters used by the live preview:
// each channel is multiplied by (1+brightness) and then scaled around 127.5 by
// (1+contrast). A neutral edit (both 0) returns img unchanged. Alpha is
// preserved.
func adjustColour(img image.Image, brightness, contrast float64) image.Image {
	if brightness == 0 && contrast == 0 {
		return img
	}
	bounds := img.Bounds()
	dst := image.NewRGBA(bounds)
	draw.Draw(dst, bounds, img, bounds.Min, draw.Src)

	brightnessFactor := 1 + brightness
	contrastFactor := 1 + contrast
	pix := dst.Pix
	for i := 0; i < len(pix); i += 4 {
		pix[i] = adjustChannel(pix[i], brightnessFactor, contrastFactor)
		pix[i+1] = adjustChannel(pix[i+1], brightnessFactor, contrastFactor)
		pix[i+2] = adjustChannel(pix[i+2], brightnessFactor, contrastFactor)
		// pix[i+3] (alpha) is left untouched.
	}
	return dst
}

// adjustChannel applies the brightness multiply then the contrast scaling around
// the mid-point to a single 8-bit channel value, clamping the result to [0, 255].
func adjustChannel(value uint8, brightnessFactor, contrastFactor float64) uint8 {
	v := float64(value) * brightnessFactor
	v = (v-127.5)*contrastFactor + 127.5
	return clampByte(v)
}

// clampByte rounds v to the nearest integer and clamps it into the [0, 255]
// range of an 8-bit channel.
func clampByte(v float64) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 255:
		return 255
	default:
		return uint8(v + 0.5)
	}
}

// clampInt clamps v into the inclusive range [0, hi], the valid pixel-index
// range for a crop edge.
func clampInt(v, hi int) int {
	switch {
	case v < 0:
		return 0
	case v > hi:
		return hi
	default:
		return v
	}
}
