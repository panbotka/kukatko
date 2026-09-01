package avatar

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// testHash is a hex file hash long enough to shard the cache tree.
const testHash = "aabbccddeeff00112233445566778899aabbccddeeff001122334455667788ff"

// fakeSource serves a fixed JPEG for every size, recording which size was asked
// for so a test can assert the ladder's choice.
type fakeSource struct {
	jpeg []byte
	// asked records the size of the last OpenOrGenerate call.
	asked string
	err   error
}

// OpenOrGenerate records the requested size and returns the fixed JPEG, or the
// configured error.
func (f *fakeSource) OpenOrGenerate(
	_ context.Context, _ photos.Photo, size string,
) (io.ReadCloser, error) {
	f.asked = size
	if f.err != nil {
		return nil, f.err
	}
	return io.NopCloser(bytes.NewReader(f.jpeg)), nil
}

// gradientJPEG encodes a width×height JPEG whose left half is black and right
// half white, so a test can tell which part of the frame a crop came from.
func gradientJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			shade := uint8(0)
			if x >= width/2 {
				shade = 255
			}
			img.Set(x, y, color.RGBA{R: shade, G: shade, B: shade, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encoding the test source: %v", err)
	}
	return buf.Bytes()
}

// decode reads a JPEG the renderer produced, failing the test if it is not one.
func decode(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decoding the rendered avatar: %v", err)
	}
	return img
}

// readAll drains and closes a reader the renderer returned.
func readAll(t *testing.T, reader io.ReadCloser) []byte {
	t.Helper()
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading the rendered avatar: %v", err)
	}
	return data
}

func TestRelPath_shardsAndNames(t *testing.T) {
	t.Parallel()

	got, err := RelPath(testHash, "cover")
	if err != nil {
		t.Fatalf("RelPath returned an error: %v", err)
	}
	want := "avatar/aa/bb/cc/" + testHash + "_cover_320.jpg"
	if got != want {
		t.Errorf("RelPath = %q, want %q", got, want)
	}
}

func TestRelPath_rejectsUnusableHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		hash string
	}{
		{name: "empty", hash: ""},
		{name: "too short", hash: "aabb"},
		{name: "not hex", hash: "zzbbccddeeff0011"},
		{name: "path traversal", hash: "../../etc/passwd0011"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := RelPath(tt.hash, "cover"); !errors.Is(err, ErrInvalidHash) {
				t.Errorf("RelPath(%q) error = %v, want ErrInvalidHash", tt.hash, err)
			}
		})
	}
}

func TestVariantKey_distinguishesCropsAndIsStable(t *testing.T) {
	t.Parallel()

	if got := variantKey(nil); got != "cover" {
		t.Errorf("variantKey(nil) = %q, want %q", got, "cover")
	}
	left := variantKey(&Box{X: 0.1, Y: 0.1, W: 0.2, H: 0.2})
	right := variantKey(&Box{X: 0.7, Y: 0.1, W: 0.2, H: 0.2})
	if left == right {
		t.Errorf("two different faces share the variant key %q", left)
	}
	if again := variantKey(&Box{X: 0.1, Y: 0.1, W: 0.2, H: 0.2}); again != left {
		t.Errorf("variantKey is not stable: %q then %q", left, again)
	}
	// Noise below the fourth decimal must not fork the cache.
	if nudged := variantKey(&Box{X: 0.100000001, Y: 0.1, W: 0.2, H: 0.2}); nudged != left {
		t.Errorf("variantKey changed on floating-point noise: %q vs %q", nudged, left)
	}
}

func TestFaceSquare_squaresAndStaysInsideTheFrame(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		bounds image.Rectangle
		face   Box
	}{
		{
			name:   "centre of a wide frame",
			bounds: image.Rect(0, 0, 1200, 800),
			face:   Box{X: 0.4, Y: 0.4, W: 0.1, H: 0.15},
		},
		{
			name:   "face at the top-left corner",
			bounds: image.Rect(0, 0, 1200, 800),
			face:   Box{X: 0, Y: 0, W: 0.1, H: 0.1},
		},
		{
			name:   "face at the bottom-right corner",
			bounds: image.Rect(0, 0, 1200, 800),
			face:   Box{X: 0.9, Y: 0.9, W: 0.1, H: 0.1},
		},
		{
			name:   "face filling a tall frame",
			bounds: image.Rect(0, 0, 400, 1200),
			face:   Box{X: 0, Y: 0, W: 1, H: 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := faceSquare(tt.bounds, tt.face)
			if !got.In(tt.bounds) {
				t.Errorf("faceSquare = %v, which leaves the frame %v", got, tt.bounds)
			}
			if diff := got.Dx() - got.Dy(); diff < -1 || diff > 1 {
				t.Errorf("faceSquare = %v, which is not square (%d×%d)", got, got.Dx(), got.Dy())
			}
			if got.Dx() <= 0 {
				t.Errorf("faceSquare = %v, which is empty", got)
			}
		})
	}
}

func TestCentreSquare_takesTheMiddleOfTheFrame(t *testing.T) {
	t.Parallel()

	got := centreSquare(image.Rect(0, 0, 1000, 600))
	want := image.Rect(200, 0, 800, 600)
	if got != want {
		t.Errorf("centreSquare = %v, want %v", got, want)
	}
}

func TestSourceSize_climbsOnlyForSmallFaces(t *testing.T) {
	t.Parallel()

	frame := photos.Photo{FileWidth: 6000, FileHeight: 4000}
	tests := []struct {
		name  string
		photo photos.Photo
		face  *Box
		want  string
	}{
		{name: "a cover photo takes the square grid size", photo: frame, face: nil, want: "tile_500"},
		{
			name:  "a face filling a third of the frame is sharp at the bottom rung",
			photo: frame,
			face:  &Box{X: 0.3, Y: 0.3, W: 0.33, H: 0.33},
			want:  "fit_720",
		},
		{
			name:  "a face a fifth of the frame needs the middle rung",
			photo: frame,
			face:  &Box{X: 0.3, Y: 0.3, W: 0.2, H: 0.2},
			want:  "fit_1280",
		},
		{
			name:  "a face a fiftieth of the frame stops at the ceiling",
			photo: frame,
			face:  &Box{X: 0.3, Y: 0.3, W: 0.02, H: 0.02},
			want:  "fit_1920",
		},
		{
			name:  "a degenerate frame falls back to the cheapest rung",
			photo: photos.Photo{},
			face:  &Box{X: 0.3, Y: 0.3, W: 0.1, H: 0.1},
			want:  "fit_720",
		},
		{
			// The same wide box on the same stored file: upright it spans 2880 px
			// and is sharp at the bottom rung, turned it spans 1920 and is not.
			name:  "a quarter-turn orientation swaps the frame the box is measured against",
			photo: photos.Photo{FileWidth: 6000, FileHeight: 4000, FileOrientation: 6},
			face:  &Box{X: 0.3, Y: 0.3, W: 0.3, H: 0.05},
			want:  "fit_1280",
		},
		{
			name:  "the same box upright is sharp at the bottom rung",
			photo: frame,
			face:  &Box{X: 0.3, Y: 0.3, W: 0.3, H: 0.05},
			want:  "fit_720",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sourceSize(tt.photo, tt.face); got != tt.want {
				t.Errorf("sourceSize = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOpen_rendersASquareJPEGOfTheFace(t *testing.T) {
	t.Parallel()

	source := &fakeSource{jpeg: gradientJPEG(t, 1200, 800)}
	renderer := New(source, t.TempDir())
	photo := photos.Photo{UID: "ph_1", FileHash: testHash, FileWidth: 1200, FileHeight: 800}

	// A face on the white right-hand half of the frame.
	reader, etag, err := renderer.Open(t.Context(), photo, &Box{X: 0.7, Y: 0.4, W: 0.2, H: 0.3})
	if err != nil {
		t.Fatalf("Open returned an error: %v", err)
	}
	img := decode(t, readAll(t, reader))
	if img.Bounds().Dx() != img.Bounds().Dy() {
		t.Errorf("rendered avatar is %v, want a square", img.Bounds())
	}
	if img.Bounds().Dx() > Side {
		t.Errorf("rendered avatar is %d px, want at most %d", img.Bounds().Dx(), Side)
	}
	if r, _, _, _ := img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2).RGBA(); r < 0x8000 {
		t.Errorf("the crop landed on the dark half of the frame (centre red = %d)", r)
	}
	if !strings.Contains(etag, testHash) {
		t.Errorf("ETag %q does not name the photo's file hash", etag)
	}
}

func TestOpen_cachesTheRenditionAndServesItBack(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()
	source := &fakeSource{jpeg: gradientJPEG(t, 1200, 800)}
	renderer := New(source, cacheDir)
	photo := photos.Photo{UID: "ph_1", FileHash: testHash, FileWidth: 1200, FileHeight: 800}
	face := &Box{X: 0.4, Y: 0.4, W: 0.2, H: 0.2}

	first, firstTag, err := renderer.Open(t.Context(), photo, face)
	if err != nil {
		t.Fatalf("first Open returned an error: %v", err)
	}
	firstData := readAll(t, first)

	rel, err := RelPath(photo.FileHash, variantKey(face))
	if err != nil {
		t.Fatalf("RelPath returned an error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, filepath.FromSlash(rel))); err != nil {
		t.Fatalf("the rendition was not cached: %v", err)
	}

	// A second call must be answered from the cache, without touching the source.
	source.err = errors.New("the source must not be read again")
	second, secondTag, err := renderer.Open(t.Context(), photo, face)
	if err != nil {
		t.Fatalf("second Open returned an error: %v", err)
	}
	if secondData := readAll(t, second); !bytes.Equal(firstData, secondData) {
		t.Errorf("the cached rendition differs from the rendered one (%d vs %d bytes)",
			len(firstData), len(secondData))
	}
	if secondTag != firstTag {
		t.Errorf("ETag changed between calls: %q then %q", firstTag, secondTag)
	}
}

func TestOpen_coverPhotoIsShownWhole(t *testing.T) {
	t.Parallel()

	source := &fakeSource{jpeg: gradientJPEG(t, 500, 500)}
	renderer := New(source, t.TempDir())
	photo := photos.Photo{UID: "ph_1", FileHash: testHash, FileWidth: 4000, FileHeight: 3000}

	reader, _, err := renderer.Open(t.Context(), photo, nil)
	if err != nil {
		t.Fatalf("Open returned an error: %v", err)
	}
	img := decode(t, readAll(t, reader))
	if source.asked != "tile_500" {
		t.Errorf("a cover photo was cut from %q, want the square grid size", source.asked)
	}
	if img.Bounds().Dx() != Side || img.Bounds().Dy() != Side {
		t.Errorf("rendered cover is %v, want %d×%d", img.Bounds(), Side, Side)
	}
}

func TestOpen_reportsAnUnreadableSource(t *testing.T) {
	t.Parallel()

	source := &fakeSource{jpeg: gradientJPEG(t, 400, 400), err: os.ErrNotExist}
	renderer := New(source, t.TempDir())
	photo := photos.Photo{UID: "ph_1", FileHash: testHash, FileWidth: 400, FileHeight: 400}

	if _, _, err := renderer.Open(t.Context(), photo, nil); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Open error = %v, want the source's own error", err)
	}
}

func TestOpen_rejectsAPhotoWithoutAUsableHash(t *testing.T) {
	t.Parallel()

	renderer := New(&fakeSource{jpeg: gradientJPEG(t, 400, 400)}, t.TempDir())
	photo := photos.Photo{UID: "ph_1", FileHash: "nope", FileWidth: 400, FileHeight: 400}

	if _, _, err := renderer.Open(t.Context(), photo, nil); !errors.Is(err, ErrInvalidHash) {
		t.Errorf("Open error = %v, want ErrInvalidHash", err)
	}
}

func TestOpen_neverUpscalesATinyCrop(t *testing.T) {
	t.Parallel()

	source := &fakeSource{jpeg: gradientJPEG(t, 200, 200)}
	renderer := New(source, t.TempDir())
	photo := photos.Photo{UID: "ph_1", FileHash: testHash, FileWidth: 200, FileHeight: 200}

	reader, _, err := renderer.Open(t.Context(), photo, &Box{X: 0.45, Y: 0.45, W: 0.1, H: 0.1})
	if err != nil {
		t.Fatalf("Open returned an error: %v", err)
	}
	img := decode(t, readAll(t, reader))
	if img.Bounds().Dx() >= Side {
		t.Errorf("a %d px crop was rendered at %d px, want no upscaling", 32, img.Bounds().Dx())
	}
}
