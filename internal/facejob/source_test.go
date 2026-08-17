package facejob

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/panbotka/kukatko/internal/imgconvert"
	"github.com/panbotka/kukatko/internal/photos"
)

// staticMaterializer hands out a fixed local path regardless of input, counting
// how many times its cleanup ran so tests can prove the original is released.
type staticMaterializer struct {
	abs      string
	err      error
	released int
}

// Materialize returns the configured path, or the configured error.
func (s *staticMaterializer) Materialize(_ context.Context, _ string) (string, func(), error) {
	if s.err != nil {
		return "", func() {}, s.err
	}
	return s.abs, func() { s.released++ }, nil
}

// quadrantImage builds a landscape test image whose four quadrants have distinct
// colours, so a rotation is visible in the pixels and not only in the dimensions.
// Top-left is red, top-right green, bottom-left blue, bottom-right white.
func quadrantImage(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	colours := [2][2]color.RGBA{
		{{R: 255, A: 255}, {G: 255, A: 255}},
		{{B: 255, A: 255}, {R: 255, G: 255, B: 255, A: 255}},
	}
	for y := range height {
		for x := range width {
			row, col := 0, 0
			if y >= height/2 {
				row = 1
			}
			if x >= width/2 {
				col = 1
			}
			img.Set(x, y, colours[row][col])
		}
	}
	return img
}

// exifAPP1 builds a minimal EXIF APP1 segment holding nothing but the orientation
// tag: big-endian TIFF header, one IFD entry (0x0112, SHORT), no next IFD. Writing
// it by hand keeps the fixtures pure Go — no exiftool, no extra dependency — and it
// is the only EXIF a face-detection test needs.
func exifAPP1(orientation uint16) []byte {
	var tiff bytes.Buffer
	tiff.WriteString("MM")                                   // big-endian
	_ = binary.Write(&tiff, binary.BigEndian, uint16(42))    // TIFF magic
	_ = binary.Write(&tiff, binary.BigEndian, uint32(8))     // offset of IFD0
	_ = binary.Write(&tiff, binary.BigEndian, uint16(1))     // one entry
	_ = binary.Write(&tiff, binary.BigEndian, uint16(0x112)) // Orientation
	_ = binary.Write(&tiff, binary.BigEndian, uint16(3))     // type SHORT
	_ = binary.Write(&tiff, binary.BigEndian, uint32(1))     // count
	_ = binary.Write(&tiff, binary.BigEndian, orientation)   // value
	_ = binary.Write(&tiff, binary.BigEndian, uint16(0))     // value padding
	_ = binary.Write(&tiff, binary.BigEndian, uint32(0))     // no next IFD

	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	segment := make([]byte, 4, 4+len(payload))
	segment[0], segment[1] = 0xFF, 0xE1
	binary.BigEndian.PutUint16(segment[2:], uint16(len(payload)+2))
	return append(segment, payload...)
}

// writeJPEG writes img as a JPEG at path, splicing in an EXIF orientation tag when
// orientation is positive. The result is a file that says it still has to be turned
// — exactly what an iPhone hands over.
func writeJPEG(t *testing.T, dir string, img image.Image, orientation int) string {
	t.Helper()
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	out := encoded.Bytes()
	if orientation > 0 {
		// After the SOI marker, before everything else.
		out = append(append(append([]byte{}, out[:2]...), exifAPP1(uint16(orientation))...), out[2:]...)
	}
	path := filepath.Join(dir, "img.jpg")
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// writeTempFile writes arbitrary bytes to a temp file and returns its path. It is
// for the error/cleanup paths, which never look at the content.
func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "img.bin")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

// sourceOver builds a StorageSource whose decoder returns path unchanged.
func sourceOver(store Materializer, path string, cleaned *bool) *StorageSource {
	return &StorageSource{
		storage: store,
		decode: func(_ context.Context, _ string) (string, func(), error) {
			return path, func() {
				if cleaned != nil {
					*cleaned = true
				}
			}, nil
		},
	}
}

// TestOpenUpright_allOrientations is the regression test for the production bug:
// whatever orientation the file carries, what reaches the sidecar must already be
// upright, and the frame reported with it must be measured on those very bytes.
//
// Each case sends a 120×80 quadrant image tagged with one orientation and checks
// three things about the result: the reported frame, the decoded pixel dimensions
// (they must agree — a frame that disagrees with the bytes is what put boxes beside
// faces), and the colour of the top-left quadrant, which says the picture really was
// turned the right way rather than merely resized.
func TestOpenUpright_allOrientations(t *testing.T) {
	t.Parallel()

	const width, height = 120, 80
	tests := []struct {
		name        string
		orientation int
		wantW       int
		wantH       int
		wantTopLeft color.RGBA
	}{
		{"1 upright, no tag", 0, width, height, color.RGBA{R: 255, A: 255}},
		{"1 upright", 1, width, height, color.RGBA{R: 255, A: 255}},
		// 180°: the white bottom-right quadrant becomes the top-left one.
		{"3 rotate 180", 3, width, height, color.RGBA{R: 255, G: 255, B: 255, A: 255}},
		// 90° CW: the blue bottom-left quadrant becomes the top-left one.
		{"6 rotate 90 cw", 6, height, width, color.RGBA{B: 255, A: 255}},
		// 270° CW: the green top-right quadrant becomes the top-left one.
		{"8 rotate 270 cw", 8, height, width, color.RGBA{G: 255, A: 255}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := writeJPEG(t, t.TempDir(), quadrantImage(width, height), tt.orientation)
			src := sourceOver(&staticMaterializer{abs: path}, path, nil)

			upright, err := src.OpenUpright(context.Background(),
				photos.Photo{UID: "ph1", FileWidth: width, FileHeight: height, FileOrientation: tt.orientation})
			if err != nil {
				t.Fatalf("OpenUpright: %v", err)
			}
			defer func() { _ = upright.Reader.Close() }()

			if upright.Width != tt.wantW || upright.Height != tt.wantH {
				t.Errorf("reported frame = %dx%d, want %dx%d",
					upright.Width, upright.Height, tt.wantW, tt.wantH)
			}
			img, _, err := image.Decode(upright.Reader)
			if err != nil {
				t.Fatalf("decode what was sent: %v", err)
			}
			bounds := img.Bounds()
			if bounds.Dx() != upright.Width || bounds.Dy() != upright.Height {
				t.Errorf("bytes sent are %dx%d but the frame says %dx%d",
					bounds.Dx(), bounds.Dy(), upright.Width, upright.Height)
			}
			if got := img.At(bounds.Min.X+5, bounds.Min.Y+5); !colourClose(got, tt.wantTopLeft) {
				t.Errorf("top-left pixel = %v, want %v — the picture was not turned upright",
					got, tt.wantTopLeft)
			}
		})
	}
}

// colourClose compares two colours with a tolerance wide enough for JPEG's lossy
// round trip but far below the distance between the fixture's quadrants.
func colourClose(got color.Color, want color.RGBA) bool {
	const tolerance = 0x2000
	gr, gg, gb, _ := got.RGBA()
	wr, wg, wb, _ := want.RGBA()
	return absDiff(gr, wr) < tolerance && absDiff(gg, wg) < tolerance && absDiff(gb, wb) < tolerance
}

// absDiff returns the absolute difference of two colour channel values.
func absDiff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

// TestOpenUpright_noTagStreamsTheFile sends an untagged file byte-for-byte instead
// of re-encoding it: the common case must not pay for the fix, and a re-encode would
// cost detection quality for nothing.
func TestOpenUpright_noTagStreamsTheFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeJPEG(t, dir, quadrantImage(64, 48), 0)
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	store := &staticMaterializer{abs: path}
	cleaned := false
	src := sourceOver(store, path, &cleaned)

	upright, err := src.OpenUpright(context.Background(), photos.Photo{UID: "ph1"})
	if err != nil {
		t.Fatalf("OpenUpright: %v", err)
	}
	got, err := io.ReadAll(upright.Reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Error("an untagged file was not streamed unchanged")
	}
	if err := upright.Reader.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !cleaned {
		t.Error("decoder cleanup was not run on Close")
	}
	if store.released != 1 {
		t.Errorf("materialized original released %d times, want 1", store.released)
	}
}

// TestOpenUpright_rotatedReleasesEagerly proves the rotated path frees the files it
// no longer needs: the bytes live in memory, so the converted temp file and the
// materialized original must be released before the reader is even read.
func TestOpenUpright_rotatedReleasesEagerly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	temp := writeJPEG(t, dir, quadrantImage(64, 48), 6)
	store := &staticMaterializer{abs: "/originals/photo.heic"}
	src := &StorageSource{
		storage: store,
		decode: func(_ context.Context, _ string) (string, func(), error) {
			return temp, func() { _ = os.Remove(temp) }, nil
		},
	}

	upright, err := src.OpenUpright(context.Background(), photos.Photo{UID: "ph1"})
	if err != nil {
		t.Fatalf("OpenUpright: %v", err)
	}
	if _, err := os.Stat(temp); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp converted file still present while its pixels are in memory: %v", err)
	}
	if store.released != 1 {
		t.Errorf("materialized original released %d times, want 1", store.released)
	}
	if _, err := io.ReadAll(upright.Reader); err != nil {
		t.Fatalf("reading the rotated bytes: %v", err)
	}
	if err := upright.Reader.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestOpenUpright_convertedCleanup opens a temporary converted file and removes it
// on Close, mirroring the HEIC/RAW/video path for a file with nothing to rotate.
func TestOpenUpright_convertedCleanup(t *testing.T) {
	t.Parallel()

	temp := writeJPEG(t, t.TempDir(), quadrantImage(32, 32), 0)
	store := &staticMaterializer{abs: "/originals/photo.heic"}
	src := &StorageSource{
		storage: store,
		decode: func(_ context.Context, _ string) (string, func(), error) {
			return temp, func() { _ = os.Remove(temp) }, nil
		},
	}

	upright, err := src.OpenUpright(context.Background(), photos.Photo{UID: "ph1"})
	if err != nil {
		t.Fatalf("OpenUpright: %v", err)
	}
	if err := upright.Reader.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(temp); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp converted file still present after Close: %v", err)
	}
	if store.released != 1 {
		t.Errorf("materialized original released %d times, want 1", store.released)
	}
}

// TestOpenUpright_pixelBoundRefusesRotation refuses to rasterize an image beyond the
// configured cap instead of allocating its bitmap. The job then fails visibly rather
// than sending a sideways image whose boxes would silently be in the wrong frame.
func TestOpenUpright_pixelBoundRefusesRotation(t *testing.T) {
	t.Parallel()

	path := writeJPEG(t, t.TempDir(), quadrantImage(120, 80), 6)
	store := &staticMaterializer{abs: path}
	src := sourceOver(store, path, nil)
	src.maxPixels = 100 // 120x80 = 9600 pixels

	if _, err := src.OpenUpright(context.Background(), photos.Photo{UID: "ph1"}); !errors.Is(err, imgconvert.ErrImageTooLarge) {
		t.Fatalf("OpenUpright over the cap = %v, want ErrImageTooLarge", err)
	}
	if store.released != 1 {
		t.Errorf("materialized original released %d times after a refusal, want 1", store.released)
	}
}

// TestOpenUpright_undecodableFails reports an error for a file whose frame cannot be
// measured, rather than sending it with a zero frame.
func TestOpenUpright_undecodableFails(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t, "not an image")
	store := &staticMaterializer{abs: path}
	cleaned := false
	src := sourceOver(store, path, &cleaned)

	if _, err := src.OpenUpright(context.Background(), photos.Photo{UID: "ph1"}); err == nil {
		t.Fatal("OpenUpright on a non-image = nil, want an error")
	}
	if !cleaned {
		t.Error("cleanup was not run after an unreadable frame")
	}
	if store.released != 1 {
		t.Errorf("materialized original released %d times, want 1", store.released)
	}
}

// TestOpenUpright_materializeError surfaces a storage failure and never calls the
// decoder.
func TestOpenUpright_materializeError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("offline")
	src := &StorageSource{
		storage: &staticMaterializer{err: wantErr},
		decode: func(_ context.Context, _ string) (string, func(), error) {
			t.Error("decoder ran despite a materialize failure")
			return "", func() {}, nil
		},
	}
	if _, err := src.OpenUpright(context.Background(), photos.Photo{UID: "ph1"}); !errors.Is(err, wantErr) {
		t.Errorf("OpenUpright error = %v, want %v", err, wantErr)
	}
}

// TestOpenUpright_decodeError surfaces a decoder failure.
func TestOpenUpright_decodeError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	store := &staticMaterializer{abs: "/x"}
	src := &StorageSource{
		storage: store,
		decode: func(_ context.Context, _ string) (string, func(), error) {
			return "", nil, wantErr
		},
	}
	if _, err := src.OpenUpright(context.Background(), photos.Photo{UID: "ph1"}); !errors.Is(err, wantErr) {
		t.Errorf("OpenUpright error = %v, want %v", err, wantErr)
	}
	if store.released != 1 {
		t.Errorf("materialized original released %d times after a decode failure, want 1", store.released)
	}
}

// TestOpenUpright_openError runs cleanup when the decodable file cannot be opened,
// so a converted temp file is not leaked.
func TestOpenUpright_openError(t *testing.T) {
	t.Parallel()

	cleaned := false
	store := &staticMaterializer{abs: "/x"}
	src := &StorageSource{
		storage: store,
		decode: func(_ context.Context, _ string) (string, func(), error) {
			return filepath.Join(t.TempDir(), "does-not-exist.jpg"), func() { cleaned = true }, nil
		},
	}
	if _, err := src.OpenUpright(context.Background(), photos.Photo{UID: "ph1"}); err == nil {
		t.Fatal("OpenUpright = nil, want an error opening a missing file")
	}
	if !cleaned {
		t.Error("cleanup was not run after an open failure")
	}
	if store.released != 1 {
		t.Errorf("materialized original released %d times after an open failure, want 1", store.released)
	}
}
