package imgconvert

import (
	"image"

	"github.com/panbotka/kukatko/internal/exif"
)

// Orient returns img with its EXIF orientation applied, i.e. in display
// orientation: the picture a viewer shows and a person sees. Orientations 5-8
// (the quarter turns) exchange the returned image's width and height; 2-4 (the
// mirrors and the 180° flip) keep them. A value of 1 or less, or above 8, is a
// no-op and img is returned unchanged.
//
// It is the one implementation of the transform in the codebase: the thumbnailer
// applies it before rendering, and the face detector applies it before handing an
// image to the sidecar (which does not read EXIF). Two copies of a pixel mapping
// this fiddly drift, and a box detected in one frame then divided by another is
// exactly how faces end up beside faces.
func Orient(img image.Image, orientation int) image.Image {
	if orientation <= 1 || orientation > 8 {
		return img
	}
	bounds := img.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()

	dstW, dstH := srcW, srcH
	if exif.QuarterTurn(orientation) {
		dstW, dstH = srcH, srcW
	}

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for y := range dstH {
		for x := range dstW {
			sx, sy := orientedPixel(orientation, x, y, srcW, srcH)
			dst.Set(x, y, img.At(bounds.Min.X+sx, bounds.Min.Y+sy))
		}
	}
	return dst
}

// orientedPixel returns the source pixel coordinate that should appear at
// destination (x, y) after applying the given EXIF orientation transform to a
// srcW × srcH image. Behaviour is defined for orientation values 2-8; Orient
// handles the no-op cases before calling this.
func orientedPixel(orientation, x, y, srcW, srcH int) (sx, sy int) {
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
