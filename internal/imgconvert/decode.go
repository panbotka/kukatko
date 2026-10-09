package imgconvert

import (
	"context"
	"errors"
	"fmt"
	"image"

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

// ErrImageTooLarge is returned by EnforcePixelBound and ReserveDecode when a
// source image's pixel count (width×height) exceeds the configured cap, and by
// a DecodeBudget (with ErrOverBudget) when its decoded bytes exceed the whole
// budget. Callers branch on it with errors.Is to refuse a decompression bomb or
// an accidentally enormous panorama before the full bitmap is ever allocated.
var ErrImageTooLarge = errors.New("imgconvert: image too large to decode")

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
	cfg, ok, err := PeekConfig(path)
	if err != nil || !ok {
		// An unreadable header is left to the caller's decode to report as the
		// true error rather than masked as an oversize rejection.
		return err
	}
	return checkPixels(cfg, maxPixels)
}

// checkPixels returns ErrImageTooLarge when cfg's width×height exceeds a
// positive maxPixels.
func checkPixels(cfg image.Config, maxPixels int64) error {
	if pixels := int64(cfg.Width) * int64(cfg.Height); maxPixels > 0 && pixels > maxPixels {
		return fmt.Errorf("%w: %d pixels (%dx%d) exceeds cap %d",
			ErrImageTooLarge, pixels, cfg.Width, cfg.Height, maxPixels)
	}
	return nil
}

// ReserveDecode is the gate in front of an in-process decode of the image at
// path: it peeks the header, refuses an image over maxPixels
// (ErrImageTooLarge), and reserves from budget the bytes the decode will hold —
// DecodedBytes of the header, plus whatever extra(header) says the caller
// derives from the bitmap while it is alive (a full-size Orient copy, resize
// scratch). The returned release gives the reservation back; call it once the
// bitmap is no longer referenced.
//
// A header that cannot be parsed reserves nothing and returns no error, so the
// caller's decode reports the real problem. A nil budget reserves nothing; a nil
// extra adds nothing. Errors: ErrImageTooLarge (over the pixel cap, or — with
// ErrOverBudget — over the whole budget), ctx's error while waiting, or a
// failure to open path.
func ReserveDecode(
	ctx context.Context, path string, maxPixels int64, budget *DecodeBudget, extra func(image.Config) int64,
) (func(), error) {
	cfg, ok, err := PeekConfig(path)
	if err != nil {
		return func() {}, err
	}
	if !ok {
		return func() {}, nil
	}
	return ReserveConfig(ctx, cfg, maxPixels, budget, extra)
}

// ReserveConfig is ReserveDecode for a header the caller has already read (an
// image held in memory rather than on disk): it refuses cfg over maxPixels with
// ErrImageTooLarge and reserves DecodedBytes(cfg) plus extra(cfg) from budget.
// Errors and the returned release are ReserveDecode's.
func ReserveConfig(
	ctx context.Context, cfg image.Config, maxPixels int64, budget *DecodeBudget, extra func(image.Config) int64,
) (func(), error) {
	if err := checkPixels(cfg, maxPixels); err != nil {
		return func() {}, err
	}
	cost := DecodedBytes(cfg)
	if extra != nil {
		cost += extra(cfg)
	}
	return budget.Reserve(ctx, cost)
}
