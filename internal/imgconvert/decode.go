package imgconvert

import (
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"

	// Register the pure-Go raster decoders so image.DecodeConfig can read the
	// header of any format the pipeline decodes. HEIC/RAW/video sources are first
	// converted to an intermediate JPEG by EnsureDecodable, so the JPEG decoder
	// covers those too.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// ErrImageTooLarge is returned by EnforcePixelBound when a source image's pixel
// count (width×height) exceeds the configured cap. Callers branch on it with
// errors.Is to refuse a decompression bomb or an accidentally enormous panorama
// before the full bitmap is ever allocated.
var ErrImageTooLarge = errors.New("imgconvert: image exceeds maximum pixel count")

// EnforcePixelBound peeks the header of the image at path via image.DecodeConfig
// and returns ErrImageTooLarge when its width×height exceeds maxPixels, so a
// caller can reject an oversized source before image.Decode allocates its full
// RGBA bitmap (a 30000×30000 image is ~3.6 GB). It reads only the header, never
// the pixel data, so the check is cheap.
//
// A non-positive maxPixels disables the bound (every image passes). A header
// that cannot be parsed is not rejected here — EnforcePixelBound returns nil and
// leaves the caller's own decode to surface the real error — so this never
// changes the outcome for an image that would otherwise decode. path must be a
// file the registered pure-Go decoders can read; HEIC/RAW/video callers pass the
// EnsureDecodable output.
func EnforcePixelBound(path string, maxPixels int64) error {
	if maxPixels <= 0 {
		return nil
	}
	f, err := os.Open(path) //nolint:gosec // G304: path is the storage/imgconvert file the caller is about to decode.
	if err != nil {
		return fmt.Errorf("imgconvert: open %s: %w", filepath.Base(path), err)
	}
	defer func() { _ = f.Close() }()

	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		// Unreadable header: leave it to the caller's decode to report the true
		// error rather than masking it as an oversize rejection.
		return nil
	}
	if pixels := int64(cfg.Width) * int64(cfg.Height); pixels > maxPixels {
		return fmt.Errorf("%w: %d pixels (%dx%d) exceeds cap %d",
			ErrImageTooLarge, pixels, cfg.Width, cfg.Height, maxPixels)
	}
	return nil
}
