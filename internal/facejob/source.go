package facejob

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"

	// Register the pure-Go raster decoders, so image.Decode/DecodeConfig can read
	// every format that reaches this package (HEIC/RAW/video arrive as the
	// intermediate JPEG imgconvert produced).
	_ "image/gif"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"github.com/panbotka/kukatko/internal/exif"
	"github.com/panbotka/kukatko/internal/imgconvert"
	"github.com/panbotka/kukatko/internal/photos"
)

// uprightJPEGQuality is the quality of the re-encoded upright JPEG. It is high
// because the only consumer is a face detector: the re-encode exists to turn the
// picture, not to save bytes, and detection quality must not pay for it.
const uprightJPEGQuality = 95

// Materializer yields a real local file for a photo's stored relative path. It
// is the subset of storage.Storage StorageSource needs.
type Materializer interface {
	// Materialize returns a local path for relPath together with a cleanup the
	// caller must always run once it is done with the file.
	Materialize(ctx context.Context, relPath string) (path string, cleanup func(), err error)
}

// Decoder turns a media path into a path the standard image decoders can read,
// converting HEIC/RAW/video as needed. It is satisfied by imgconvert.EnsureDecodable
// and is injectable so StorageSource can be tested without the external tools.
type Decoder func(ctx context.Context, srcPath string) (path string, cleanup func(), err error)

// StorageSource opens an upright copy of a photo's original from storage. The
// full-resolution original (decoded if HEIC/RAW/video, and rotated if it still
// carries an EXIF orientation) is what the face_detect handler streams to the
// sidecar.
type StorageSource struct {
	storage   Materializer
	decode    Decoder
	maxPixels int64
}

// NewStorageSource builds a StorageSource over storage, using
// imgconvert.EnsureDecodable as its decoder. maxPixels caps the pixel count of an
// image it will fully rasterize in order to rotate it (a non-positive value
// disables the cap); nothing else is decoded.
func NewStorageSource(storage Materializer, maxPixels int64) *StorageSource {
	return &StorageSource{storage: storage, decode: imgconvert.EnsureDecodable, maxPixels: maxPixels}
}

// cleanupReadCloser wraps an open file with the cleanup that releases the
// materialized original and any temporary converted copy, so closing the reader
// both closes the file and frees everything behind it.
type cleanupReadCloser struct {
	file    *os.File
	cleanup func()
}

// Read reads from the underlying file.
func (c *cleanupReadCloser) Read(p []byte) (int, error) {
	n, err := c.file.Read(p)
	if err != nil && err != io.EOF {
		return n, fmt.Errorf("facejob: reading image: %w", err)
	}
	return n, err //nolint:wrapcheck // io.EOF must pass through unwrapped for callers.
}

// Close closes the file and then runs the cleanup.
func (c *cleanupReadCloser) Close() error {
	err := c.file.Close()
	c.cleanup()
	if err != nil {
		return fmt.Errorf("facejob: closing image: %w", err)
	}
	return nil
}

// OpenUpright materializes the photo's original, ensures it is decodable
// (converting non-native formats to a temporary JPEG), applies any orientation
// the resulting file still carries and returns the bytes to send together with
// the frame they measure.
//
// The rotation is decided from the file being sent, never from the catalogue: an
// intermediate copy produced by imgconvert may already have the rotation baked
// into its pixels, and re-applying the original's tag would turn the picture
// twice. That reasoning holds only for an intermediate, so when the bytes ARE the
// untouched original and the original says nothing, the catalogue breaks the tie
// (see uprightFrom). A file with no orientation at all is streamed untouched —
// the common case pays nothing — and only a file that really has to turn is
// decoded, rotated and re-encoded.
//
// The returned reader's Close releases the temporary files behind it; every error
// path here releases them too.
func (s *StorageSource) OpenUpright(ctx context.Context, photo photos.Photo) (UprightImage, error) {
	decodable, untouched, cleanup, err := s.materializeDecodable(ctx, photo)
	if err != nil {
		return UprightImage{}, err
	}
	upright, err := s.uprightFrom(decodable, photo, untouched)
	if err != nil {
		cleanup()
		return UprightImage{}, err
	}
	if upright.Reader != nil {
		// Rotated in memory: the files behind it are no longer needed.
		cleanup()
		return upright, nil
	}
	file, err := os.Open(decodable) //nolint:gosec // G304: path derived from the storage-confined original.
	if err != nil {
		cleanup()
		return UprightImage{}, fmt.Errorf("facejob: opening image for %s: %w", photo.UID, err)
	}
	upright.Reader = &cleanupReadCloser{file: file, cleanup: cleanup}
	return upright, nil
}

// materializeDecodable pulls the original out of storage and converts it to
// something the pure-Go decoders can read, returning the local path, whether that
// path still holds the untouched original, and the cleanup that releases both
// temporary files.
//
// The untouched flag is decided by comparing the path the decoder returned with
// the path it was given: imgconvert.EnsureDecodable returns its input unchanged
// on the passthrough path (JPEG/PNG/WebP/BMP/GIF/TIFF need no conversion), so an
// identical path means the bytes about to be sent are the original itself, while
// any other path is an intermediate whose pixels may already carry the rotation.
// That distinction is what makes the catalogue fallback in uprightFrom safe.
func (s *StorageSource) materializeDecodable(
	ctx context.Context, photo photos.Photo,
) (path string, untouched bool, cleanup func(), err error) {
	abs, releaseOriginal, err := s.storage.Materialize(ctx, photo.FilePath)
	if err != nil {
		return "", false, nil, fmt.Errorf("facejob: materializing image for %s: %w", photo.UID, err)
	}
	decodable, releaseDecoded, err := s.decode(ctx, abs)
	if err != nil {
		releaseOriginal()
		return "", false, nil, fmt.Errorf("facejob: ensuring decodable image for %s: %w", photo.UID, err)
	}
	// The decoded file may be derived from the original, so drop it first.
	return decodable, decodable == abs, func() { releaseDecoded(); releaseOriginal() }, nil
}

// uprightFrom decides what to send for the decodable file at path: an
// UprightImage with the rotated bytes in Reader when the file still has to be
// turned, or one with a nil Reader and only the frame filled in when the file is
// already upright and can be streamed as it is. untouched says the path still
// holds the original (see materializeDecodable).
//
// Kukátko has two orientation readers and they can disagree, which is the hazard
// this function is built around. The one here (exif.FileOrientation) reads the
// file it is about to send; ingest's (exiftool, via internal/exif.Extract) read
// the original and wrote photos.file_orientation. Only the first can be trusted
// for an imgconvert intermediate — its pixels may already be turned — so the
// catalogue is consulted ONLY when the bytes are the untouched original and the
// original itself yields nothing. That is the case that broke: a JPEG carrying
// its orientation exclusively in XMP read as 0 here, was streamed as it lay, and
// the detector got a picture on its side. The fallback stays conditional so the
// converted case keeps behaving exactly as it did.
func (s *StorageSource) uprightFrom(path string, photo photos.Photo, untouched bool) (UprightImage, error) {
	orientation := exif.FileOrientation(path)
	if orientation == 0 && untouched {
		orientation = photo.FileOrientation
	}
	if orientation <= 1 {
		width, height, err := imageFrame(path)
		if err != nil {
			return UprightImage{}, fmt.Errorf("facejob: reading frame of %s: %w", photo.UID, err)
		}
		// Nothing was applied here, so the frame is the one the bytes already
		// embody: the photo's own orientation, whether they are the untouched
		// original (which then says nothing either) or an intermediate imgconvert
		// already turned.
		return UprightImage{Width: width, Height: height, Orientation: photo.FileOrientation}, nil
	}
	return s.rotate(path, photo.UID, orientation)
}

// rotate decodes the file, applies the given orientation and re-encodes it as a
// JPEG with no EXIF block at all, so the bytes sent are upright however the
// receiver treats metadata. The frame reported is measured on the rotated image,
// not derived from the tag, and the orientation reported is the one that was
// applied — the two describe the same picture by construction.
func (s *StorageSource) rotate(path, photoUID string, orientation int) (UprightImage, error) {
	if err := imgconvert.EnforcePixelBound(path, s.maxPixels); err != nil {
		return UprightImage{}, fmt.Errorf("facejob: rotating image for %s: %w", photoUID, err)
	}
	file, err := os.Open(path) //nolint:gosec // G304: path derived from the storage-confined original.
	if err != nil {
		return UprightImage{}, fmt.Errorf("facejob: opening image for %s: %w", photoUID, err)
	}
	defer func() { _ = file.Close() }()

	img, _, err := image.Decode(file)
	if err != nil {
		return UprightImage{}, fmt.Errorf("facejob: decoding image for %s: %w", photoUID, err)
	}
	oriented := imgconvert.Orient(img, orientation)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, oriented, &jpeg.Options{Quality: uprightJPEGQuality}); err != nil {
		return UprightImage{}, fmt.Errorf("facejob: encoding upright image for %s: %w", photoUID, err)
	}
	bounds := oriented.Bounds()
	return UprightImage{
		Reader:      io.NopCloser(&buf),
		Width:       bounds.Dx(),
		Height:      bounds.Dy(),
		Orientation: orientation,
	}, nil
}

// imageFrame reads the pixel dimensions of the image at path from its header
// only, without rasterizing it.
func imageFrame(path string) (width, height int, err error) {
	file, err := os.Open(path) //nolint:gosec // G304: path derived from the storage-confined original.
	if err != nil {
		return 0, 0, fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	cfg, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0, fmt.Errorf("decoding header of %s: %w", path, err)
	}
	return cfg.Width, cfg.Height, nil
}
