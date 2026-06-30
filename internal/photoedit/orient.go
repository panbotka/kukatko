package photoedit

import "image"

// Orient returns img rotated and/or flipped according to the EXIF orientation
// value (1-8) so it appears upright, mirroring the thumbnailer's orientation
// handling. Orientations 5-8 swap the output width and height. Values <= 1 or
// > 8 are a no-op and img is returned unchanged. The download pipeline orients
// the decoded original before applying the user's edit so the edit's rotation
// is relative to the upright photo.
func Orient(img image.Image, orientation int) image.Image {
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
// srcW × srcH image. Behaviour is defined for orientation values 2-8; Orient
// handles the no-op cases before calling this.
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
