// Package imgconvert wraps the external decoders (heif-convert, exiftool/dcraw)
// that turn HEIC/HEIF and RAW originals into an intermediate JPEG so the rest
// of Kukátko's image pipeline — image.Decode, the thumbnailer, perceptual
// hashes — can handle them with only the pure-Go JPEG/PNG/WebP decoders and
// keep the binary CGO-free.
//
// The package is intentionally narrow: it inspects a file's extension and magic
// bytes, dispatches to the right converter when one is needed, and otherwise
// returns the input path untouched. It does not modify the original file, talk
// to a database, or know anything about uploads or the catalogue.
//
// RAW handling deliberately extracts the camera's embedded JPEG preview (via
// exiftool) instead of running a full demosaic: the preview is large enough for
// every thumbnail tier and is orders of magnitude cheaper to obtain on a
// Raspberry Pi.
package imgconvert

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Sentinel errors so callers can branch with errors.Is — most importantly to
// distinguish "the external tool is not installed" (operator action required)
// from a genuine decode failure.
var (
	// ErrConverterMissing is returned (wrapped) when the external converter
	// binary required for the input's format is not installed on PATH. Callers
	// can errors.Is against it to surface an actionable "install
	// heif-convert/exiftool" message rather than a generic failure.
	ErrConverterMissing = errors.New("imgconvert: external converter not installed")
	// ErrUnsupportedFormat is returned when the input is neither directly
	// decodable nor a format imgconvert knows how to convert.
	ErrUnsupportedFormat = errors.New("imgconvert: unsupported format")
	// ErrNoEmbeddedPreview is returned when a RAW file carries no extractable
	// embedded JPEG preview (no full-demosaic fallback is attempted).
	ErrNoEmbeddedPreview = errors.New("imgconvert: no embedded JPEG preview in RAW")
)

// Format constants returned by DetectFormat. FormatUnknown is used both when
// the extension is unrecognised and when magic bytes fail to identify the file.
const (
	FormatJPEG    = "jpeg"
	FormatPNG     = "png"
	FormatWebP    = "webp"
	FormatHEIC    = "heic"
	FormatRAW     = "raw"
	FormatUnknown = "unknown"
)

// extFormats maps lowercased file extensions (including the leading dot) to the
// format constant returned by DetectFormat. The RAW entries cover the major
// vendors Kukátko ingests; everything else is reported as FormatUnknown.
var extFormats = map[string]string{
	".jpg":  FormatJPEG,
	".jpeg": FormatJPEG,
	".png":  FormatPNG,
	".webp": FormatWebP,
	".heic": FormatHEIC,
	".heif": FormatHEIC,
	".cr2":  FormatRAW,
	".cr3":  FormatRAW,
	".nef":  FormatRAW,
	".arw":  FormatRAW,
	".dng":  FormatRAW,
	".raf":  FormatRAW,
	".orf":  FormatRAW,
	".rw2":  FormatRAW,
	".pef":  FormatRAW,
	".srw":  FormatRAW,
}

// IsSupportedFormat reports whether the pipeline can ingest a file with this
// extension. The extension may include or omit the leading dot and is
// case-insensitive.
func IsSupportedFormat(ext string) bool {
	if ext == "" {
		return false
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	_, ok := extFormats[strings.ToLower(ext)]
	return ok
}

// DetectFormat returns one of "jpeg", "png", "webp", "heic", "raw", or
// "unknown" for the file at path. The extension is consulted first and then
// verified by magic bytes. When extension and magic disagree, the magic-byte
// result wins for JPEG/PNG/WebP/HEIC. RAW formats are accepted on extension
// alone — every vendor's RAW container has a different header, so there is no
// universal "this is a RAW" magic to match against.
func DetectFormat(path string) string {
	extFmt := formatByExt(path)
	magic := magicFormat(path)
	if magic == FormatUnknown {
		// Magic bytes told us nothing; trust the extension. A genuinely invalid
		// file produces a converter/decoder error later.
		return extFmt
	}
	if extFmt == FormatRAW {
		// RAW extensions override magic because most RAWs are TIFF-based and
		// magicFormat could never spot the vendor brand from the leading bytes.
		return FormatRAW
	}
	if magic == extFmt {
		return extFmt
	}
	// Extension and magic disagree; the magic bytes are authoritative.
	return magic
}

// EnsureDecodable returns a path to a file that image.Decode (with the JPEG,
// PNG, and WebP decoders registered) can handle, together with a cleanup
// function the caller MUST defer.
//
// If the input is already JPEG/PNG/WebP, EnsureDecodable returns srcPath
// unchanged with a no-op cleanup and a nil error. If the input is HEIC/HEIF or
// a supported RAW, the matching external converter is invoked to produce a
// temporary JPEG under os.TempDir(); the temp path is returned with a cleanup
// that removes it (safe to call multiple times).
//
// On error the returned cleanup is nil (nothing to clean up); on any successful
// return it is non-nil, so callers can unconditionally defer cleanup().
func EnsureDecodable(ctx context.Context, srcPath string) (string, func(), error) {
	if srcPath == "" {
		return "", nil, errors.New("imgconvert: srcPath must not be empty")
	}
	if _, err := os.Stat(srcPath); err != nil {
		return "", nil, fmt.Errorf("imgconvert: stat %s: %w", filepath.Base(srcPath), err)
	}
	switch DetectFormat(srcPath) {
	case FormatJPEG, FormatPNG, FormatWebP:
		return srcPath, func() {}, nil
	case FormatHEIC:
		return convertHEIC(ctx, srcPath)
	case FormatRAW:
		return convertRAW(ctx, srcPath)
	default:
		return "", nil, fmt.Errorf("%w: %s", ErrUnsupportedFormat, filepath.Base(srcPath))
	}
}

// formatByExt looks up the format constant for a path's extension, or
// FormatUnknown if the extension is not in extFormats.
func formatByExt(path string) string {
	if f, ok := extFormats[strings.ToLower(filepath.Ext(path))]; ok {
		return f
	}
	return FormatUnknown
}

// magicFormat inspects the first bytes of the file and classifies it by magic
// bytes. It returns FormatUnknown for files that don't match a format we
// explicitly recognise, including every RAW variant.
func magicFormat(path string) string {
	f, err := os.Open(path) //nolint:gosec // G304: caller-supplied path already Stat'ed by EnsureDecodable.
	if err != nil {
		return FormatUnknown
	}
	defer func() { _ = f.Close() }()

	var head [16]byte
	n, _ := f.Read(head[:])
	if n < 4 {
		return FormatUnknown
	}
	return classifyMagic(head[:n])
}

// classifyMagic identifies common image formats from their leading bytes:
// JPEG (FF D8 FF), PNG (89 50 4E 47 ...), WebP (RIFF....WEBP), and HEIC (an
// ISO Base Media file with "ftyp" at offset 4 and a HEIC/HEIF major brand).
func classifyMagic(b []byte) string {
	switch {
	case isJPEGMagic(b):
		return FormatJPEG
	case isPNGMagic(b):
		return FormatPNG
	case isWebPMagic(b):
		return FormatWebP
	case isHEICMagic(b):
		return FormatHEIC
	default:
		return FormatUnknown
	}
}

// isJPEGMagic reports whether b begins with the JPEG SOI marker (FF D8)
// followed by the start of an APPn/DQT segment marker.
func isJPEGMagic(b []byte) bool {
	return len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF
}

// isPNGMagic reports whether b begins with the 8-byte PNG signature.
func isPNGMagic(b []byte) bool {
	return len(b) >= 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n"
}

// isWebPMagic reports whether b begins with the RIFF/WEBP container head.
func isWebPMagic(b []byte) bool {
	return len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP"
}

// isHEICMagic reports whether b is an ISO Base Media file ("ftyp" at offset 4)
// carrying one of the HEIC/HEIF major brands.
func isHEICMagic(b []byte) bool {
	return len(b) >= 12 && string(b[4:8]) == "ftyp" && isHEIFBrand(string(b[8:12]))
}

// isHEIFBrand reports whether brand is one of the ISO Base Media major brands
// that designates a HEIC/HEIF container.
func isHEIFBrand(brand string) bool {
	switch brand {
	case "heic", "heix", "hevc", "hevx", "heim", "heis", "mif1", "msf1":
		return true
	}
	return false
}
