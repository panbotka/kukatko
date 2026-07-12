// Package imgconvert wraps the external decoders (heif-convert, exiftool/dcraw)
// that turn HEIC/HEIF and RAW originals into an intermediate JPEG so the rest
// of Kukátko's image pipeline — image.Decode, the thumbnailer, perceptual
// hashes — can handle them with only the pure-Go raster decoders
// (JPEG/PNG/WebP/BMP/GIF/TIFF) and keep the binary CGO-free.
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

	"github.com/panbotka/kukatko/internal/video"
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
	FormatBMP     = "bmp"
	FormatGIF     = "gif"
	FormatTIFF    = "tiff"
	FormatHEIC    = "heic"
	FormatRAW     = "raw"
	FormatVideo   = "video"
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
	".bmp":  FormatBMP,
	".gif":  FormatGIF,
	".tif":  FormatTIFF,
	".tiff": FormatTIFF,
	".cr2":  FormatRAW,
	".cr3":  FormatRAW,
	".nef":  FormatRAW,
	".nrw":  FormatRAW,
	".arw":  FormatRAW,
	".srf":  FormatRAW,
	".dng":  FormatRAW,
	".raf":  FormatRAW,
	".orf":  FormatRAW,
	".rw2":  FormatRAW,
	".pef":  FormatRAW,
	".srw":  FormatRAW,
	".3fr":  FormatRAW,
	".iiq":  FormatRAW,
	".x3f":  FormatRAW,
	".kdc":  FormatRAW,
	".mrw":  FormatRAW,
	".mef":  FormatRAW,
	".heic": FormatHEIC,
	".heif": FormatHEIC,
}

// IsSupportedFormat reports whether the pipeline can ingest a file with this
// extension — a directly decodable image, a convertible HEIC/RAW, or a video.
// The extension may include or omit the leading dot and is case-insensitive.
func IsSupportedFormat(ext string) bool {
	if ext == "" {
		return false
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	if _, ok := extFormats[strings.ToLower(ext)]; ok {
		return true
	}
	return video.IsVideoExt(ext)
}

// DetectFormat returns one of "jpeg", "png", "webp", "bmp", "gif", "tiff",
// "heic", "raw", "video", or "unknown" for the file at path. Video is decided on
// extension alone (the many container brands have no single magic to match).
// Otherwise the magic bytes are authoritative whenever they recognise a directly
// decodable format: a file whose content is JPEG/PNG/WebP/BMP/GIF/TIFF/HEIC is
// treated as such even when the extension says otherwise — e.g. a plain JPEG
// misnamed .dng, which must not be sent down the RAW embedded-preview path (it
// has no preview to extract).
//
// TIFF is the one ambiguous signature: most camera RAW containers (CR2, NEF,
// ARW, DNG, NRW, …) are TIFF-based and share the II*/MM* header, so TIFF magic
// must NOT hijack a real RAW. A RAW extension therefore wins over TIFF magic and
// the file goes down the embedded-preview path exactly as before; every other
// recognised magic still overrides the extension.
//
// Only when the magic bytes match nothing we recognise (a RAW whose header is
// not TIFF, or a genuinely invalid file) does the extension decide on its own.
func DetectFormat(path string) string {
	if video.IsVideoPath(path) {
		return FormatVideo
	}
	extFmt := formatByExt(path)
	magic := magicFormat(path)
	if magic == FormatUnknown {
		// Magic bytes told us nothing — a non-TIFF RAW container, or a genuinely
		// invalid file. Trust the extension; an invalid file produces a converter
		// or decoder error later.
		return extFmt
	}
	if magic == FormatTIFF && extFmt == FormatRAW {
		// A TIFF-based RAW container: the RAW extension is authoritative so the
		// file keeps going through the embedded-preview path, not decoded as a
		// plain TIFF (which would ignore the demosaic and lose the real image).
		return FormatRAW
	}
	if magic == extFmt {
		return extFmt
	}
	// Extension and magic disagree and the magic bytes recognise a real format;
	// they win, so a misnamed file is decoded by its true content.
	return magic
}

// EnsureDecodable returns a path to a file that image.Decode (with the JPEG,
// PNG, WebP, BMP, GIF, and TIFF decoders registered) can handle, together with a
// cleanup function the caller MUST defer.
//
// If the input is already a pure-Go decodable raster (JPEG/PNG/WebP/BMP/GIF/
// TIFF), EnsureDecodable returns srcPath unchanged with a no-op cleanup and a
// nil error. If the input is HEIC/HEIF, a supported RAW, or a video, the
// matching external tool (heif-convert, exiftool, or ffmpeg for a video poster
// frame) is invoked to produce a temporary JPEG under os.TempDir(); the temp
// path is returned with a cleanup that removes it (safe to call multiple times).
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
	case FormatJPEG, FormatPNG, FormatWebP, FormatBMP, FormatGIF, FormatTIFF:
		return srcPath, func() {}, nil
	case FormatHEIC:
		return convertHEIC(ctx, srcPath)
	case FormatRAW:
		return convertRAW(ctx, srcPath)
	case FormatVideo:
		decPath, cleanup, err := video.ExtractPoster(ctx, srcPath)
		if err != nil {
			return "", nil, fmt.Errorf("imgconvert: video poster: %w", err)
		}
		return decPath, cleanup, nil
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
// JPEG (FF D8 FF), PNG (89 50 4E 47 ...), WebP (RIFF....WEBP), HEIC (an ISO Base
// Media file with "ftyp" at offset 4 and a HEIC/HEIF major brand), BMP ("BM"),
// GIF ("GIF87a"/"GIF89a"), and TIFF (II*\0 little-endian or MM\0* big-endian).
// TIFF is reported as such even though most camera RAW containers share the
// header; DetectFormat resolves that ambiguity in favour of a RAW extension.
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
	case isBMPMagic(b):
		return FormatBMP
	case isGIFMagic(b):
		return FormatGIF
	case isTIFFMagic(b):
		return FormatTIFF
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

// isBMPMagic reports whether b begins with the 2-byte BMP signature "BM".
func isBMPMagic(b []byte) bool {
	return len(b) >= 2 && b[0] == 'B' && b[1] == 'M'
}

// isGIFMagic reports whether b begins with a GIF87a or GIF89a signature.
func isGIFMagic(b []byte) bool {
	if len(b) < 6 {
		return false
	}
	sig := string(b[:6])
	return sig == "GIF87a" || sig == "GIF89a"
}

// isTIFFMagic reports whether b begins with a TIFF header — little-endian
// (49 49 2A 00, "II*\0") or big-endian (4D 4D 00 2A, "MM\0*"). Most camera RAW
// containers reuse this header; DetectFormat keeps them on the RAW path.
func isTIFFMagic(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	littleEndian := b[0] == 'I' && b[1] == 'I' && b[2] == 0x2A && b[3] == 0x00
	bigEndian := b[0] == 'M' && b[1] == 'M' && b[2] == 0x00 && b[3] == 0x2A
	return littleEndian || bigEndian
}
