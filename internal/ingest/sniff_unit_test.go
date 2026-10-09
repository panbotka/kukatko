package ingest

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/panbotka/kukatko/internal/exif"
)

// junkBytes returns n deterministic pseudo-random bytes — the shape of
// `head -c 5000 /dev/urandom > broken.jpg`, without the one-in-millions chance
// of a real random stream starting with a start code.
func junkBytes(n int) []byte {
	rng := rand.New(rand.NewChaCha8([32]byte{'k', 'u', 'k', 'a', 't', 'k', 'o'}))
	out := make([]byte, n)
	for i := range out {
		out[i] = byte(rng.UintN(256))
	}
	return out
}

// readFixture reads a file committed to the repository, failing the test when
// it is missing.
func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}
	return data
}

// encodedJPEG returns the real camera JPEG committed as the exif fixture.
func encodedJPEG(t *testing.T) []byte {
	t.Helper()
	return readFixture(t, filepath.Join("..", "exif", "testdata", "sample_gps.jpg"))
}

// encodedPNG returns a small real PNG.
func encodedPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 6))); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// withHeader pads a format's leading signature to a plausible file head.
func withHeader(head string) []byte {
	return append([]byte(head), make([]byte, 64)...)
}

// transportStream builds the head of an MPEG transport stream with packets of
// size packet whose sync byte sits offset bytes into each.
func transportStream(offset, packet int) []byte {
	out := make([]byte, 3*packet)
	for i := offset; i < len(out); i += packet {
		out[i] = 0x47
	}
	return out
}

// TestLooksLikeMedia_realFormats verifies every format the pipeline ingests is
// recognised by its leading bytes — the committed JPEG and MP4 fixtures and an
// encoded PNG, plus the signatures of the formats the repository holds no
// fixture of (WebP, HEIC, the RAW families, MOV/WebM and the other containers).
func TestLooksLikeMedia_realFormats(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		head []byte
	}{
		{"jpeg fixture", encodedJPEG(t)},
		{"png", encodedPNG(t)},
		{"mp4 fixture", readFixture(t, filepath.Join("testdata", "sample.mp4"))},
		{"webp", withHeader("RIFF\x10\x00\x00\x00WEBPVP8 ")},
		{"heic", withHeader("\x00\x00\x00\x18ftypheic")},
		{"avif", withHeader("\x00\x00\x00\x1cftypavif")},
		{"cr2 (tiff le)", withHeader("II*\x00\x10\x00\x00\x00CR\x02\x00")},
		{"nef (tiff be)", withHeader("MM\x00*\x00\x00\x00\x08")},
		{"dng", withHeader("II*\x00\x08\x00\x00\x00")},
		{"cr3", withHeader("\x00\x00\x00\x18ftypcrx ")},
		{"orf", withHeader("IIRO\x08\x00\x00\x00")},
		{"rw2", withHeader("IIU\x00\x18\x00\x00\x00")},
		{"raf", withHeader("FUJIFILMCCD-RAW 0201")},
		{"x3f", withHeader("FOVb")},
		{"mrw", withHeader("\x00MRM\x00\x00")},
		{"mov", withHeader("\x00\x00\x00\x14ftypqt  ")},
		{"mov without ftyp", withHeader("\x00\x00\x00\x08wide\x00\x00")},
		{"webm/mkv", withHeader("\x1a\x45\xdf\xa3\x9f\x42\x86\x81")},
		{"avi", withHeader("RIFF\x10\x00\x00\x00AVI LIST")},
		{"wmv", withHeader("\x30\x26\xb2\x75\x8e\x66\xcf\x11\xa6\xd9")},
		{"flv", withHeader("FLV\x01\x05")},
		{"mpeg program stream", withHeader("\x00\x00\x01\xba\x44")},
		{"h264 elementary stream", withHeader("\x00\x00\x00\x01\x67")},
		{"mpeg transport stream", transportStream(0, mpegTSPacket)},
		{"m2ts", transportStream(4, m2tsPacket)},
		{"gif", withHeader("GIF89a")},
		{"bmp", withHeader("BM")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !looksLikeMedia(tt.head) {
				t.Errorf("looksLikeMedia(%q…) = false, want true", tt.head[:min(len(tt.head), 16)])
			}
		})
	}
}

// TestLooksLikeMedia_junk verifies bytes that merely carry a media name are not
// taken for media: random bytes, text, an empty file, a lone MPEG-TS sync byte.
func TestLooksLikeMedia_junk(t *testing.T) {
	t.Parallel()
	loneSync := junkBytes(sniffLen)
	loneSync[0] = 0x47
	loneSync[mpegTSPacket] = 0x00
	tests := []struct {
		name string
		head []byte
	}{
		{"random bytes", junkBytes(sniffLen)},
		{"text", []byte("hello, this is not a photo at all\n")},
		{"html", []byte("<!doctype html><html><body>nope</body></html>")},
		{"empty", nil},
		{"three bytes", []byte{0xFF, 0xD8, 0x00}},
		{"lone transport-stream sync byte", loneSync},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if looksLikeMedia(tt.head) {
				t.Errorf("looksLikeMedia(%s) = true, want false", tt.name)
			}
		})
	}
}

// TestRecognisedImage verifies what counts as the metadata tools recognising a
// file: dimensions or an image/video media type, never a generic sniff.
func TestRecognisedImage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		meta exif.Metadata
		want bool
	}{
		{"dimensions", exif.Metadata{Width: 4000, Height: 3000}, true},
		{"image type without dimensions", exif.Metadata{Mime: "image/x-phaseone-iiq"}, true},
		{"video type", exif.Metadata{Mime: "video/quicktime"}, true},
		{"octet-stream", exif.Metadata{Mime: "application/octet-stream"}, false},
		{"text", exif.Metadata{Mime: "text/plain; charset=utf-8"}, false},
		{"one dimension only", exif.Metadata{Width: 10}, false},
		{"nothing", exif.Metadata{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := recognisedImage(tt.meta); got != tt.want {
				t.Errorf("recognisedImage(%+v) = %v, want %v", tt.meta, got, tt.want)
			}
		})
	}
}

// TestAdmit verifies the whole admission over files on disk: real media passes
// on its bytes, and junk is refused with ErrNotMedia whatever name it carries —
// the metadata probe it falls through to recognises nothing in it either.
func TestAdmit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		filename string
		data     []byte
		wantErr  error
	}{
		{"jpeg", "IMG_0001.jpg", encodedJPEG(t), nil},
		{"png", "screen.png", encodedPNG(t), nil},
		{"mp4", "clip.mp4", readFixture(t, filepath.Join("testdata", "sample.mp4")), nil},
		{"jpeg under a RAW name", "IMG_0001.CR2", encodedJPEG(t), nil},
		{"random bytes named jpg", "broken.jpg", junkBytes(5000), ErrNotMedia},
		{"random bytes named mp4", "broken.mp4", junkBytes(5000), ErrNotMedia},
		{"random bytes named heic", "broken.heic", junkBytes(5000), ErrNotMedia},
		{"text named png", "notes.png", []byte("not a picture\n"), ErrNotMedia},
		{"empty file", "empty.jpg", nil, ErrNotMedia},
		{"dash manifest named mp4", "clip.mp4", []byte(dashManifest), ErrNotMedia},
		{"hls playlist named mp4", "clip.mp4", []byte(hlsPlaylist), ErrNotMedia},
		{"ffconcat script named mov", "clip.mov", []byte(concatScript), ErrNotMedia},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "kukatko-ingest-123")
			if err := os.WriteFile(path, tt.data, 0o600); err != nil {
				t.Fatalf("writing staged file: %v", err)
			}
			err := admit(t.Context(), path, tt.filename)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("admit(%s) = %v, want %v", tt.filename, err, tt.wantErr)
			}
		})
	}
}

// dashManifest, hlsPlaylist and concatScript are the manifests libavformat
// follows: each names a URL that ffprobe would fetch if it took the file for a
// video (SEC-017). The address is TEST-NET-1, never contacted — admit refuses
// them before any tool reads them.
const (
	dashManifest = `<?xml version="1.0" encoding="UTF-8"?>
<MPD xmlns="urn:mpeg:dash:schema:mpd:2011" type="static" mediaPresentationDuration="PT10S">
  <BaseURL>http://192.0.2.1/dash/</BaseURL>
</MPD>
`
	hlsPlaylist  = "#EXTM3U\n#EXT-X-TARGETDURATION:10\n#EXTINF:10,\nhttp://192.0.2.1/seg.ts\n#EXT-X-ENDLIST\n"
	concatScript = "ffconcat version 1.0\nfile http://192.0.2.1/clip.mp4\n"
)

// TestIsStreamingManifest verifies the manifest shapes are recognised — also
// behind a byte-order mark or leading whitespace — and that other text and XML
// are not: an XML declaration counts only with an MPD root behind it.
func TestIsStreamingManifest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		head string
		want bool
	}{
		{"dash", dashManifest, true},
		{"dash without declaration", "<MPD type=\"static\"></MPD>", true},
		{"dash behind a bom", "\xEF\xBB\xBF" + dashManifest, true},
		{"hls", hlsPlaylist, true},
		{"hls behind whitespace", "\r\n  " + hlsPlaylist, true},
		{"ffconcat", concatScript, true},
		{"other xml", `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"/>`, false},
		{"plain text", "not a video\n", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isStreamingManifest([]byte(tt.head)); got != tt.want {
				t.Errorf("isStreamingManifest(%q) = %v, want %v", tt.head, got, tt.want)
			}
		})
	}
}

// TestAdmit_missingFile verifies a staged file that cannot be read is an error,
// but not a "not media" verdict about content nobody saw.
func TestAdmit_missingFile(t *testing.T) {
	t.Parallel()
	err := admit(t.Context(), filepath.Join(t.TempDir(), "gone"), "a.jpg")
	if err == nil || errors.Is(err, ErrNotMedia) {
		t.Errorf("admit(missing) = %v, want a read error", err)
	}
}
