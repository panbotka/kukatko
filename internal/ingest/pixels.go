package ingest

import (
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"

	"github.com/panbotka/kukatko/internal/imgconvert"
)

// ErrDamaged indicates a still image whose signature is fine but whose data is
// cut short or corrupt beyond the header — `head -c 2000 photo.jpg`, an upload
// interrupted half-way. It passes the content check (its leading bytes are a
// JPEG's), but no thumbnail, pHash or sidecar job could ever read it, so it is
// refused before anything about it is stored.
var ErrDamaged = errors.New("ingest: damaged or incomplete image")

// CodeDamaged is the stable FileResult.Code of a file refused as ErrDamaged,
// for a client to translate (and to know a retry of the same bytes would fail
// again).
const CodeDamaged = "damaged"

// stagedPixels is what the pre-store decode learned about a staged still: the
// decoded image when its format is one Kukátko decodes in-process and the decode
// succeeded, nil otherwise (a HEIC/RAW/video, a format whose pure-Go decoder is
// incomplete, an image over the pixel cap). A nil img is "not checked here", not
// a verdict: the post-processing decodes the original itself and degrades to a
// warning, as it always did.
type stagedPixels struct {
	img image.Image
}

// verifyPixels decodes the staged still at path — named filename on arrival —
// when Kukátko can decode its format in-process, so a damaged one is refused
// with ErrDamaged before a row, an original or a job exists. The decode is not
// wasted: the image it yields is the one the pHash and the blurred placeholder
// are computed from, which is the decode ingest would have done next anyway.
//
// Only JPEG, PNG and GIF are judged. Their decoders are complete for every real
// file, and the two that name a valid-but-unsupported feature (an arithmetic-
// coded JPEG, say) say so with an UnsupportedError, which is not a reason to
// refuse. The x/image decoders are not complete — an animated WebP, a JPEG-
// compressed TIFF or an RLE BMP fail to decode although nothing is wrong with
// them — so those formats, like HEIC and RAW, are never refused for failing a
// decode here.
func (s *Service) verifyPixels(path, filename string) (stagedPixels, error) {
	switch imgconvert.DetectFormatNamed(path, filename) {
	case imgconvert.FormatJPEG, imgconvert.FormatPNG, imgconvert.FormatGIF:
	default:
		return stagedPixels{}, nil
	}
	if err := imgconvert.EnforcePixelBound(path, s.maxPixels); err != nil {
		// Too large to decode safely; hashPixels reports the cap as it always has.
		return stagedPixels{}, nil
	}
	file, err := os.Open(path) //nolint:gosec // G304: path is the pipeline's own staged temp file.
	if err != nil {
		return stagedPixels{}, fmt.Errorf("ingest: reopening staged file: %w", err)
	}
	defer func() { _ = file.Close() }()

	img, _, err := image.Decode(file)
	switch {
	case err == nil:
		return stagedPixels{img: img}, nil
	case isUnsupportedFeature(err):
		return stagedPixels{}, nil
	default:
		return stagedPixels{}, fmt.Errorf("%w: %s: %w", ErrDamaged, originalName(filename), err)
	}
}

// isUnsupportedFeature reports whether err is a decoder admitting the file uses
// a valid feature it does not implement, rather than finding the data broken.
func isUnsupportedFeature(err error) bool {
	var jpegErr jpeg.UnsupportedError
	var pngErr png.UnsupportedError
	return errors.As(err, &jpegErr) || errors.As(err, &pngErr)
}
