package imgconvert

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

// magic byte prefixes for the formats DetectFormat classifies.
var (
	jpegMagic = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}
	pngMagic  = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0d")
	webpMagic = []byte("RIFF\x00\x00\x00\x00WEBPVP8 ")
	heicMagic = []byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00")
	tiffMagic = []byte{0x49, 0x49, 0x2A, 0x00, 0x08, 0x00} // little-endian TIFF (RAW container)
)

// writeFile renders content to a fresh file named base under dir and returns
// its path. The test fails on any IO error.
func writeFile(t *testing.T, dir, base string, content []byte) string {
	t.Helper()
	p := filepath.Join(dir, base)
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatalf("write %q: %v", p, err)
	}
	return p
}

// TestIsSupportedFormat covers recognised and unrecognised extensions, with and
// without the leading dot and in mixed case.
func TestIsSupportedFormat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		ext  string
		want bool
	}{
		{".jpg", true},
		{"jpg", true},
		{".JPG", true},
		{".HEIC", true},
		{".cr2", true},
		{".nef", true},
		{".bmp", true},
		{".gif", true},
		{".tif", true},
		{".tiff", true},
		{".3fr", true},
		{".iiq", true},
		{".x3f", true},
		{".mp4", true},
		{".mov", true},
		{".MKV", true},
		{".txt", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := IsSupportedFormat(tc.ext); got != tc.want {
			t.Errorf("IsSupportedFormat(%q) = %v, want %v", tc.ext, got, tc.want)
		}
	}
}

// TestDetectFormat exercises extension/magic agreement, magic overriding a wrong
// extension (including a JPEG misnamed .dng), and RAW detection falling through to
// the extension when the magic bytes match nothing recognised.
func TestDetectFormat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		file    string
		content []byte
		want    string
	}{
		{"jpeg by ext and magic", "a.jpg", jpegMagic, FormatJPEG},
		{"png by ext and magic", "a.png", pngMagic, FormatPNG},
		{"webp by ext and magic", "a.webp", webpMagic, FormatWebP},
		{"heic by ext and magic", "a.heic", heicMagic, FormatHEIC},
		{"raw by ext, tiff magic", "a.cr2", tiffMagic, FormatRAW},
		{"real dng, tiff magic", "a.dng", tiffMagic, FormatRAW},
		{"raw by ext, unknown magic", "a.nef", []byte{0x01, 0x02, 0x03, 0x04}, FormatRAW},
		{"jpeg misnamed dng, magic wins", "a.dng", jpegMagic, FormatJPEG},
		{"video by ext (mp4)", "a.mp4", []byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p'}, FormatVideo},
		{"video by ext (mkv)", "a.mkv", []byte{0x1a, 0x45, 0xdf, 0xa3}, FormatVideo},
		{"magic overrides wrong ext", "a.png", jpegMagic, FormatJPEG},
		{"unknown ext, jpeg magic", "a.bin", jpegMagic, FormatJPEG},
		{"unknown ext and magic", "a.bin", []byte{0x00, 0x01, 0x02, 0x03}, FormatUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := writeFile(t, t.TempDir(), tc.file, tc.content)
			if got := DetectFormat(p); got != tc.want {
				t.Errorf("DetectFormat(%q) = %q, want %q", tc.file, got, tc.want)
			}
		})
	}
}

// TestEnsureDecodable_passthrough confirms directly decodable formats return
// the input path unchanged with a non-nil, safe-to-call cleanup.
func TestEnsureDecodable_passthrough(t *testing.T) {
	t.Parallel()
	tests := []struct {
		file    string
		content []byte
	}{
		{"a.jpg", jpegMagic},
		{"a.png", pngMagic},
		{"a.webp", webpMagic},
	}
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			p := writeFile(t, t.TempDir(), tc.file, tc.content)
			got, cleanup, err := EnsureDecodable(context.Background(), p)
			if err != nil {
				t.Fatalf("EnsureDecodable: %v", err)
			}
			if got != p {
				t.Errorf("path = %q, want passthrough %q", got, p)
			}
			if cleanup == nil {
				t.Fatal("cleanup must be non-nil on success")
			}
			cleanup()
			cleanup() // must be safe to call again
		})
	}
}

// TestEnsureDecodable_converterMissing verifies the HEIC and RAW branches both
// report ErrConverterMissing when their external tool is absent from PATH —
// covering the command-dispatch branching without performing a conversion.
func TestEnsureDecodable_converterMissing(t *testing.T) {
	t.Setenv("PATH", "") // exec.LookPath finds nothing.
	tests := []struct {
		name    string
		file    string
		content []byte
	}{
		{"heic", "a.heic", heicMagic},
		{"raw", "a.cr2", tiffMagic},
	}
	for _, tc := range tests {
		p := writeFile(t, t.TempDir(), tc.file, tc.content)
		_, cleanup, err := EnsureDecodable(context.Background(), p)
		if !errors.Is(err, ErrConverterMissing) {
			t.Errorf("%s: error = %v, want ErrConverterMissing", tc.name, err)
		}
		if cleanup != nil {
			t.Errorf("%s: cleanup must be nil on error", tc.name)
		}
	}
}

// TestEnsureDecodable_videoMissingFFmpeg verifies the video branch reports a
// clear error (not a passthrough) when ffmpeg is absent from PATH.
func TestEnsureDecodable_videoMissingFFmpeg(t *testing.T) {
	t.Setenv("PATH", "") // exec.LookPath finds no ffmpeg.
	p := writeFile(t, t.TempDir(), "clip.mp4", []byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p'})
	_, cleanup, err := EnsureDecodable(context.Background(), p)
	if err == nil {
		t.Fatal("video without ffmpeg = nil error, want a clear failure")
	}
	if cleanup != nil {
		t.Error("cleanup must be nil on error")
	}
}

// TestEnsureDecodable_unsupported confirms a recognised-but-undecodable file
// yields ErrUnsupportedFormat, and a missing source yields a stat error.
func TestEnsureDecodable_unsupported(t *testing.T) {
	t.Parallel()
	p := writeFile(t, t.TempDir(), "a.bin", []byte{0x00, 0x01, 0x02, 0x03})
	if _, _, err := EnsureDecodable(context.Background(), p); !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("error = %v, want ErrUnsupportedFormat", err)
	}

	if _, _, err := EnsureDecodable(context.Background(), ""); err == nil {
		t.Error("empty srcPath should error")
	}
	if _, _, err := EnsureDecodable(context.Background(), filepath.Join(t.TempDir(), "missing.jpg")); err == nil {
		t.Error("missing file should error")
	}
}

// encodeImage renders a tiny 2x2 test image encoded in the named format ("bmp",
// "gif", "tiff" or "jpeg") and returns the bytes. Encoding real fixtures (rather
// than hand-written magic prefixes) means the same bytes exercise both magic
// detection and an actual image.Decode round-trip. It fails the test on error.
func encodeImage(t *testing.T, format string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 0xFF, A: 0xFF})
	img.Set(1, 1, color.RGBA{B: 0xFF, A: 0xFF})

	var buf bytes.Buffer
	var err error
	switch format {
	case "bmp":
		err = bmp.Encode(&buf, img)
	case "gif":
		err = gif.Encode(&buf, img, nil)
	case "tiff":
		err = tiff.Encode(&buf, img, nil)
	case "jpeg":
		err = jpeg.Encode(&buf, img, nil)
	default:
		t.Fatalf("encodeImage: unknown format %q", format)
	}
	if err != nil {
		t.Fatalf("encode %s: %v", format, err)
	}
	return buf.Bytes()
}

// decodeNonEmpty decodes the file at path with the registered pure-Go decoders
// and fails the test if it errors or yields an empty bounding box.
func decodeNonEmpty(t *testing.T, path string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %q: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("decode %q: %v", path, err)
	}
	if img.Bounds().Empty() {
		t.Fatalf("decoded %q has empty bounds", path)
	}
}

// TestDetectFormat_rasterFormats verifies BMP, GIF and TIFF are detected by
// their extension and — when misnamed — by their magic bytes, and that a JPEG
// misnamed .tif is still detected as JPEG (unambiguous magic wins over a wrong
// TIFF extension).
func TestDetectFormat_rasterFormats(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		file   string
		format string
		want   string
	}{
		{"bmp by ext and magic", "a.bmp", "bmp", FormatBMP},
		{"gif by ext and magic", "a.gif", "gif", FormatGIF},
		{"tiff by ext .tif and magic", "a.tif", "tiff", FormatTIFF},
		{"tiff by ext .tiff and magic", "a.tiff", "tiff", FormatTIFF},
		{"bmp misnamed .txt, magic wins", "a.txt", "bmp", FormatBMP},
		{"gif misnamed .dat, magic wins", "a.dat", "gif", FormatGIF},
		{"tiff misnamed .png, magic wins", "a.png", "tiff", FormatTIFF},
		{"jpeg misnamed .tif, magic wins", "a.tif", "jpeg", FormatJPEG},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := writeFile(t, t.TempDir(), tc.file, encodeImage(t, tc.format))
			if got := DetectFormat(p); got != tc.want {
				t.Errorf("DetectFormat(%q) = %q, want %q", tc.file, got, tc.want)
			}
		})
	}
}

// TestEnsureDecodable_rasterPassthrough confirms BMP, GIF and TIFF are treated as
// directly decodable (path returned unchanged, no external converter) and that
// the returned file decodes to a non-empty image with the registered decoders.
func TestEnsureDecodable_rasterPassthrough(t *testing.T) {
	t.Parallel()
	tests := []struct {
		file   string
		format string
	}{
		{"a.bmp", "bmp"},
		{"a.gif", "gif"},
		{"a.tif", "tiff"},
	}
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			p := writeFile(t, t.TempDir(), tc.file, encodeImage(t, tc.format))
			got, cleanup, err := EnsureDecodable(context.Background(), p)
			if err != nil {
				t.Fatalf("EnsureDecodable: %v", err)
			}
			defer cleanup()
			if got != p {
				t.Errorf("path = %q, want passthrough %q", got, p)
			}
			decodeNonEmpty(t, got)
		})
	}
}

// TestDetectFormat_tiffRawNotHijacked verifies a TIFF-based RAW container keeps
// its RAW extension authority: even with a valid TIFF header (real CR2/NEF/ARW/
// DNG/NRW/… are TIFF-based), it must route to the RAW embedded-preview path, not
// be decoded as a plain TIFF.
func TestDetectFormat_tiffRawNotHijacked(t *testing.T) {
	t.Parallel()
	tiffBytes := encodeImage(t, "tiff")
	for _, ext := range []string{".cr2", ".nef", ".arw", ".dng", ".nrw", ".3fr", ".iiq", ".mef"} {
		p := writeFile(t, t.TempDir(), "raw"+ext, tiffBytes)
		if got := DetectFormat(p); got != FormatRAW {
			t.Errorf("DetectFormat(%q) = %q, want %q (RAW extension must win over TIFF magic)", ext, got, FormatRAW)
		}
	}
}

// TestEnsureDecodable_tiffRawRoutesToConverter confirms a TIFF-based RAW is sent
// to the RAW converter (not decoded as a plain TIFF): with exiftool absent it
// reports ErrConverterMissing rather than passing through.
func TestEnsureDecodable_tiffRawRoutesToConverter(t *testing.T) {
	t.Setenv("PATH", "") // exec.LookPath finds no exiftool.
	p := writeFile(t, t.TempDir(), "shot.cr2", encodeImage(t, "tiff"))
	_, cleanup, err := EnsureDecodable(context.Background(), p)
	if !errors.Is(err, ErrConverterMissing) {
		t.Errorf("error = %v, want ErrConverterMissing (RAW must not decode as plain TIFF)", err)
	}
	if cleanup != nil {
		t.Error("cleanup must be nil on error")
	}
}
