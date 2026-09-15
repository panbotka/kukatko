package userpic

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // register the PNG decoder
	"io"

	goexif "github.com/rwcarlsen/goexif/exif"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // register the WebP decoder

	"github.com/panbotka/kukatko/internal/imgconvert"
)

// MaxSide is the edge of a stored profile picture, in pixels. It is larger than
// anything the app draws today (the navbar and a comment row paint a 32 px
// circle, the account page a little more), on the principle that the bytes are
// stored once and re-encoding them later from a picture that was already thrown
// away is impossible. A 512 px JPEG costs some tens of kilobytes.
const MaxSide = 512

// quality is the JPEG encoder quality of a stored picture. It matches the one
// the avatar renderer uses, so an uploaded picture and an inherited face are
// encoded alike.
const quality = 85

// decodableFormats are the formats an upload may arrive in: the ones the library
// decodes in pure Go, without a CGO codec or a shell-out. It is a whitelist
// rather than "whatever image.Decode managed", because the decoders registered
// in this binary are a global set another package contributes to (the
// thumbnailer registers GIF, BMP and TIFF) and accepting whatever happens to be
// linked in would make the accepted formats an accident of the import graph.
var decodableFormats = map[string]bool{"jpeg": true, "png": true, "webp": true}

// Normalize turns submitted image bytes into the picture that is actually
// stored: decoded, turned upright by its EXIF orientation, cropped square about
// its centre, scaled down to at most MaxSide a side and re-encoded as a JPEG.
//
// Re-encoding is the whole point and not an optimisation. The bytes a client
// uploads are arbitrary — a 40 Mpx panorama, a PNG with an embedded colour
// profile, a file carrying the GPS coordinates of somebody's house in its EXIF —
// and none of that should be handed back to every reader of a comment thread.
// What comes out of here is a small square with no metadata, and the submitted
// original is never kept.
//
// It never upscales: a picture smaller than MaxSide is stored at its own size,
// since inventing pixels would only cost bytes.
//
// It returns ErrUnsupportedFormat for bytes that are not a decodable JPEG, PNG
// or WebP, and ErrTooLarge for more than MaxUploadBytes of them.
func Normalize(data []byte) ([]byte, error) {
	if int64(len(data)) > MaxUploadBytes {
		return nil, ErrTooLarge
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnsupportedFormat, err)
	}
	if !decodableFormats[format] {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFormat, format)
	}
	img = imgconvert.Orient(img, uploadOrientation(data))

	rect := squareCrop(img.Bounds())
	side := min(MaxSide, rect.Dx())
	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, rect, draw.Src, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("userpic: encoding picture: %w", err)
	}
	return buf.Bytes(), nil
}

// uploadOrientation reads the EXIF orientation tag out of the submitted bytes,
// returning 1 (upright) when there is none to read — no EXIF block, an
// unparseable one, or a value outside 1-8.
//
// It matters because a phone writes an upright picture and a tag saying which
// way up it is, and everything downstream of Normalize sees only pixels: without
// this, a selfie taken in portrait would be stored on its side and stay that way
// for good, the original having been discarded.
func uploadOrientation(data []byte) int {
	decoded, err := goexif.Decode(bytes.NewReader(data))
	if err != nil {
		return 1
	}
	tag, err := decoded.Get(goexif.Orientation)
	if err != nil {
		return 1
	}
	orientation, err := tag.Int(0)
	if err != nil || orientation < 1 || orientation > 8 {
		return 1
	}
	return orientation
}

// squareCrop returns the largest square inside bounds, centred on it — the same
// crop the square thumbnail rungs and the subject avatar take, so a picture
// looks the same whichever of the three produced it.
func squareCrop(bounds image.Rectangle) image.Rectangle {
	side := min(bounds.Dx(), bounds.Dy())
	x := bounds.Min.X + (bounds.Dx()-side)/2
	y := bounds.Min.Y + (bounds.Dy()-side)/2
	return image.Rect(x, y, x+side, y+side)
}

// ReadUpload reads at most MaxUploadBytes from r and returns them, refusing
// anything longer with ErrTooLarge rather than buffering it.
//
// The bound is enforced while reading rather than after, so an oversized request
// costs one byte more than the limit instead of however much the client decided
// to send. The extra byte is how the difference between "exactly at the limit"
// and "over it" is told without trusting a declared length.
func ReadUpload(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxUploadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("userpic: reading upload: %w", err)
	}
	if int64(len(data)) > MaxUploadBytes {
		return nil, ErrTooLarge
	}
	return data, nil
}
