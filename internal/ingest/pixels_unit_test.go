package ingest

import (
	"bytes"
	"errors"
	"image"
	"image/gif"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/webp"
)

// avifHead is the leading ftyp box of an AVIF still as ffmpeg/libavif write it,
// padded so it also clears the media sniff's minimum length.
var avifHead = withHeader("\x00\x00\x00\x1cftypavif\x00\x00\x00\x00avifmif1miaf")

// encodedGIF returns a small real GIF.
func encodedGIF(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := gif.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 6)), nil); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	return buf.Bytes()
}

// truncated returns the first half of data — `head -c` of a real file.
func truncated(data []byte) []byte {
	return data[:len(data)/2]
}

// stageFile writes data where the pipeline stages an upload: under a temp name
// that carries no extension, which is why every check takes the upload name.
func stageFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kukatko-ingest-123")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing staged file: %v", err)
	}
	return path
}

// TestVerifyPixels verifies the pre-store decode: an intact JPEG/PNG/GIF comes
// back decoded (the image the pHash then reuses), a truncated one is refused as
// ErrDamaged, and a format Kukátko does not decode in-process — HEIC, a
// TIFF-based RAW, a video, an undecodable-but-fine WebP — is never refused for it.
func TestVerifyPixels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		filename    string
		data        []byte
		wantErr     error
		wantDecoded bool
	}{
		{"intact jpeg", "IMG_0001.jpg", encodedJPEG(t), nil, true},
		{"intact png", "screen.png", encodedPNG(t), nil, true},
		{"intact gif", "anim.gif", encodedGIF(t), nil, true},
		{"jpeg under a RAW name", "IMG_0001.CR2", encodedJPEG(t), nil, true},
		{"truncated jpeg", "cut.jpg", encodedJPEG(t)[:2000], ErrDamaged, false},
		{"truncated png", "cut.png", truncated(encodedPNG(t)), ErrDamaged, false},
		{"truncated gif", "cut.gif", truncated(encodedGIF(t)), ErrDamaged, false},
		{"truncated jpeg named png", "cut.png", encodedJPEG(t)[:2000], ErrDamaged, false},
		{"heic is not decoded here", "IMG_0001.heic", withHeader("\x00\x00\x00\x18ftypheic"), nil, false},
		{"tiff RAW is not decoded here", "IMG_0001.CR2", withHeader("II*\x00\x08\x00\x00\x00"), nil, false},
		{"tiff is left to the thumbnailer", "scan.tif", withHeader("II*\x00\x08\x00\x00\x00"), nil, false},
		{"webp is left to the thumbnailer", "a.webp", withHeader("RIFF\x10\x00\x00\x00WEBPVP8 "), nil, false},
		{"jpeg bytes named as a video", "clip.mp4", encodedJPEG(t)[:2000], nil, false},
	}
	svc := New(Config{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pixels, err := svc.verifyPixels(stageFile(t, tt.data), tt.filename)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("verifyPixels(%s) error = %v, want %v", tt.filename, err, tt.wantErr)
			}
			if got := pixels.img != nil; got != tt.wantDecoded {
				t.Errorf("verifyPixels(%s) decoded = %v, want %v", tt.filename, got, tt.wantDecoded)
			}
		})
	}
}

// TestVerifyPixels_overThePixelCapIsNotChecked verifies an image above the
// decode cap is neither decoded nor refused here: it is too large to decode
// safely, which says nothing about whether it is damaged.
func TestVerifyPixels_overThePixelCapIsNotChecked(t *testing.T) {
	t.Parallel()
	pixels, err := New(Config{MaxPixels: 1}).verifyPixels(stageFile(t, encodedJPEG(t)), "big.jpg")
	if err != nil || pixels.img != nil {
		t.Errorf("verifyPixels(over cap) = (%v, %v), want not checked", pixels.img, err)
	}
}

// TestVerifyPixels_unsupportedFeatureIsNotDamage verifies a JPEG whose decoder
// reports a valid-but-unimplemented feature is admitted undecoded: an
// arithmetic-coded JPEG (SOF9) is a real photo, merely not one Go decodes.
func TestVerifyPixels_unsupportedFeatureIsNotDamage(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 16, 16)), nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	data := buf.Bytes()
	sof := bytes.Index(data, []byte{0xFF, 0xC0})
	if sof < 0 {
		t.Fatal("encoded JPEG has no SOF0 marker")
	}
	data[sof+1] = 0xC9 // SOF9: arithmetic coding, which image/jpeg does not implement.

	pixels, err := New(Config{}).verifyPixels(stageFile(t, data), "arith.jpg")
	if err != nil || pixels.img != nil {
		t.Errorf("verifyPixels(arithmetic JPEG) = (%v, %v), want admitted undecoded", pixels.img, err)
	}
}

// TestIsUnsupportedFeature verifies the decoder errors that admit a valid
// feature are told apart from the ones that find the data broken.
func TestIsUnsupportedFeature(t *testing.T) {
	t.Parallel()
	_, webpErr := webp.Decode(bytes.NewReader([]byte("RIFF")))
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"jpeg unsupported", jpeg.UnsupportedError("SOF type"), true},
		{"wrapped jpeg unsupported", errors.Join(errors.New("ctx"), jpeg.UnsupportedError("x")), true},
		{"jpeg format error", jpeg.FormatError("short Huffman data"), false},
		{"other decoder", webpErr, false},
	}
	for _, tt := range tests {
		if got := isUnsupportedFeature(tt.err); got != tt.want {
			t.Errorf("isUnsupportedFeature(%s) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestAdmit_refusesAVIF verifies AVIF is refused as ErrUnsupportedType on its
// bytes whatever its name, and on its name whatever its bytes — never as "not
// media", which it is, and never admitted, which no decoder could follow up.
func TestAdmit_refusesAVIF(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		filename string
		data     []byte
	}{
		{"avif", "f.avif", avifHead},
		{"avif renamed jpg", "f.jpg", avifHead},
		{"avif under the generic HEIF brand", "f.heic",
			withHeader("\x00\x00\x00\x1cftypmif1\x00\x00\x00\x00mif1avifmiaf")},
		{"junk named avif", "f.AVIF", junkBytes(5000)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := admit(t.Context(), stageFile(t, tt.data), tt.filename)
			if !errors.Is(err, ErrUnsupportedType) {
				t.Errorf("admit(%s) = %v, want ErrUnsupportedType", tt.filename, err)
			}
		})
	}
}
