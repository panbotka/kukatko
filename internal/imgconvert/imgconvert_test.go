package imgconvert

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
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
