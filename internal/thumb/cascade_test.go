package thumb

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/imgconvert"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
)

// TestRenderOrder verifies the cascade renders the largest size first, ties
// broken by name so the order never depends on the caller's.
func TestRenderOrder(t *testing.T) {
	t.Parallel()
	got := renderOrder([]string{"tile_100", "fit_720", "fit_3840", "tile_500", "fit_1280"})
	want := []string{"fit_3840", "fit_1280", "fit_720", "tile_500", "tile_100"}
	if !slices.Equal(got, want) {
		t.Errorf("renderOrder = %v, want %v", got, want)
	}
}

// TestBoxFactor verifies the integer pre-shrink never takes the base below what
// any needed size requires: the long side for a fit, the short side for a tile.
func TestBoxFactor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		w, h   int
		needed []string
		want   int
	}{
		{"200 MP square, every size", 14142, 14142, SizeNames(), 3},
		{"24 MP camera, every size", 6000, 4000, SizeNames(), 1},
		{"24 MP camera, fit_720 only", 6000, 4000, []string{"fit_720"}, 8},
		{"tile bounded by the short side", 6000, 1000, []string{"tile_500", "fit_720"}, 2},
		{"smaller than every size", 300, 200, SizeNames(), 1},
		{"nothing needed", 6000, 4000, nil, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := boxFactor(tt.w, tt.h, tt.needed); got != tt.want {
				t.Errorf("boxFactor(%dx%d) = %d, want %d", tt.w, tt.h, got, tt.want)
			}
		})
	}
}

// TestBoxReduce verifies the block average: k <= 1 is the image itself, every
// output pixel is the rounded mean of its k×k block, and a partial block at the
// right/bottom edge averages only the pixels it has. A non-RGBA source (gray) is
// read through draw's conversion.
func TestBoxReduce(t *testing.T) {
	t.Parallel()
	src := image.NewGray(image.Rect(10, 20, 15, 23)) // 5×3, offset bounds
	for i := range src.Pix {
		src.Pix[i] = uint8(i * 10)
	}
	if got := boxReduce(src, 1); got != image.Image(src) {
		t.Fatal("boxReduce(k=1) did not return the source itself")
	}
	got := boxReduce(src, 2)
	if b := got.Bounds(); b.Dx() != 3 || b.Dy() != 2 {
		t.Fatalf("boxReduce(5x3, 2) = %dx%d, want 3x2", b.Dx(), b.Dy())
	}
	// Rows of the source: 0 10 20 30 40 / 50 60 70 80 90 / 100 110 120 130 140.
	want := [][]uint8{
		{(0 + 10 + 50 + 60 + 2) / 4, (20 + 30 + 70 + 80 + 2) / 4, (40 + 90 + 1) / 2},
		{(100 + 110 + 1) / 2, (120 + 130 + 1) / 2, 140},
	}
	for y, row := range want {
		for x, v := range row {
			if c := color.GrayModel.Convert(got.At(x, y)).(color.Gray); c.Y != v {
				t.Errorf("pixel (%d,%d) = %d, want %d", x, y, c.Y, v)
			}
		}
	}
}

// TestCoversAndPickSource verifies the cascade's choice of source: the smallest
// rendition already made that is at least cascadeMargin times the size, else
// the base — and never one that would make a size upscale.
func TestCoversAndPickSource(t *testing.T) {
	t.Parallel()
	base := image.NewRGBA(image.Rect(0, 0, 6000, 4000))
	fit3840 := image.NewRGBA(image.Rect(0, 0, 3840, 2560))
	fit720 := image.NewRGBA(image.Rect(0, 0, 720, 480))
	rendered := []image.Image{fit3840, fit720}

	if got := pickSource(base, rendered, sizes["fit_1280"]); got != image.Image(fit3840) {
		t.Errorf("fit_1280 source = %v, want fit_3840", got.Bounds())
	}
	// fit_3840 is not twice fit_2560: the margin sends it back to the base.
	if got := pickSource(base, rendered, sizes["fit_2560"]); got != image.Image(base) {
		t.Errorf("fit_2560 source = %v, want the base", got.Bounds())
	}
	if got := pickSource(base, rendered, sizes["tile_224"]); got != image.Image(fit720) {
		t.Errorf("tile_224 source = %v, want fit_720", got.Bounds())
	}
	// fit_720's short side is 480: not twice tile_500.
	if got := pickSource(base, rendered, sizes["tile_500"]); got != image.Image(fit3840) {
		t.Errorf("tile_500 source = %v, want fit_3840", got.Bounds())
	}
	if got := pickSource(base, nil, sizes["fit_3840"]); got != image.Image(base) {
		t.Errorf("fit_3840 source = %v, want the base", got.Bounds())
	}
	// A source smaller than the size: anything as large as the source covers it.
	if !covers(sizes["fit_3840"], 300, 200, 300, 200) || covers(sizes["tile_500"], 300, 100, 300, 200) {
		t.Error("covers misjudges a source smaller than the size")
	}
}

// TestRenderBytes verifies the scratch estimate is x/image/draw's dstW×srcH×32
// for a fit and side×square×32 for a tile, and nothing for a fit the source
// already satisfies.
func TestRenderBytes(t *testing.T) {
	t.Parallel()
	scratch, output := renderBytes(sizes["fit_3840"], 14142, 14142)
	if scratch != 3840*14142*32 || output != 3840*3840*4 {
		t.Errorf("fit_3840 of 14142² = (%d, %d)", scratch, output)
	}
	scratch, output = renderBytes(sizes["tile_100"], 6000, 4000)
	if scratch != 100*4000*32 || output != 100*100*4 {
		t.Errorf("tile_100 of 6000x4000 = (%d, %d)", scratch, output)
	}
	if scratch, output = renderBytes(sizes["fit_720"], 640, 480); scratch != 0 || output != 0 {
		t.Errorf("fit_720 of 640x480 = (%d, %d), want nothing allocated", scratch, output)
	}
}

// TestPureGoCost verifies what the thumbnailer reserves beyond the bitmap: the
// cascade alone for an upright, unedited photo; plus a full-size copy for an
// orientation that turns it; plus two more for an edit — and that the pre-shrink
// keeps the 200 MP bomb's rendering far below the ~5 GB it took from the source.
func TestPureGoCost(t *testing.T) {
	t.Parallel()
	cfg := image.Config{Width: 6000, Height: 4000}
	all := SizeNames()
	plain := pureGoCost(cfg, 1, photos.Edit{}, all)
	if plain != cascadeBytes(6000, 4000, all) {
		t.Errorf("upright cost = %d, want the cascade's %d", plain, cascadeBytes(6000, 4000, all))
	}
	turned := pureGoCost(cfg, 6, photos.Edit{}, all)
	if want := imgconvert.RGBABytes(6000, 4000) + cascadeBytes(4000, 6000, all); turned != want {
		t.Errorf("orientation-6 cost = %d, want %d", turned, want)
	}
	edited := pureGoCost(cfg, 1, photos.Edit{Brightness: 0.2}, all)
	if edited < plain+2*imgconvert.RGBABytes(6000, 4000) {
		t.Errorf("edited cost = %d, want at least two full copies above %d", edited, plain)
	}
	if bomb := cascadeBytes(14142, 14142, all); bomb > 1<<30 {
		t.Errorf("cascade of a 14142² source = %d bytes, want under 1 GiB", bomb)
	}
}

// TestGenerateAll_cascadeMatchesDirectResize verifies the cascade changes the
// memory, not the picture: every size rendered from a larger rendition stays,
// on average, within a level and a half of rendering it straight from the
// source (measured: under 0.8 for every size of this scene).
func TestGenerateAll_cascadeMatchesDirectResize(t *testing.T) {
	t.Parallel()
	th, store := newThumbnailer(t)
	src := scene(1600, 1100)
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	sf, err := store.Store(t.Context(), &buf, time.Time{}, "scene.png")
	if err != nil {
		t.Fatal(err)
	}
	photo := photos.Photo{FileHash: sf.Hash, FilePath: sf.RelPath}
	paths, err := th.GenerateAll(t.Context(), photo)
	if err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	for _, name := range SizeNames() {
		direct, err := resizeForSpec(src, sizes[name])
		if err != nil {
			t.Fatal(err)
		}
		// Both through the same JPEG encoder, so the comparison measures the
		// resampling and not the compression.
		data, err := encodeJPEG(direct, sizes[name].Quality)
		if err != nil {
			t.Fatal(err)
		}
		want, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		got := decodeFile(t, paths[name])
		if diff := meanAbsDiff(got, want); diff > 1.5 {
			t.Errorf("%s differs from a direct resize by %.2f levels on average", name, diff)
		}
	}
}

// TestGenerate_decodeBudget verifies the thumbnailer reserves from its decode
// budget: a budget smaller than the photo's cost refuses it (ErrImageTooLarge,
// ErrOverBudget), and an adequate one renders it and hands every byte back.
func TestGenerate_decodeBudget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatal(err)
	}
	photo := storeJPEG(t, store, 640, 480, 0)

	tiny := New(store, filepath.Join(root, "cache-a"), WithDecodeBudget(imgconvert.NewDecodeBudget(1024)))
	if _, err := tiny.Generate(t.Context(), photo, "fit_720"); !errors.Is(err, imgconvert.ErrOverBudget) {
		t.Fatalf("Generate under a 1 KiB budget = %v, want ErrOverBudget", err)
	}

	budget := imgconvert.NewDecodeBudget(64 << 20)
	th := New(store, filepath.Join(root, "cache-b"), WithDecodeBudget(budget))
	if _, err := th.GenerateAll(t.Context(), photo); err != nil {
		t.Fatalf("GenerateAll under a 64 MiB budget = %v", err)
	}
	release, err := budget.Reserve(t.Context(), budget.Capacity())
	if err != nil {
		t.Fatalf("the whole budget is not free after GenerateAll: %v", err)
	}
	release()
}

// scene renders a w×h test picture with what a photograph has: smooth colour
// gradients, hard-edged shapes and fine detail (a one-pixel grid in one
// corner) — the content a resampler can get visibly wrong.
func scene(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{R: uint8(x * 255 / w), G: uint8(y * 255 / h), B: 96, A: 255}
			switch {
			case x > w/5 && x < w/2 && y > h/4 && y < h*3/4:
				c = color.RGBA{R: 240, G: 230, B: 40, A: 255}
			case (x-w*3/4)*(x-w*3/4)+(y-h/2)*(y-h/2) < (h/5)*(h/5):
				c = color.RGBA{R: 20, G: 30, B: 200, A: 255}
			case x < w/8 && y < h/8 && (x%8 == 0 || y%8 == 0):
				c = color.RGBA{A: 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// decodeFile decodes the image at path.
func decodeFile(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return img
}

// meanAbsDiff is the mean absolute difference of the RGB channels of two
// same-sized images, in 8-bit levels (an image of different size is 255).
func meanAbsDiff(a, b image.Image) float64 {
	ab, bb := a.Bounds(), b.Bounds()
	if ab.Dx() != bb.Dx() || ab.Dy() != bb.Dy() {
		return 255
	}
	var sum, n float64
	for y := range ab.Dy() {
		for x := range ab.Dx() {
			r1, g1, b1, _ := a.At(ab.Min.X+x, ab.Min.Y+y).RGBA()
			r2, g2, b2, _ := b.At(bb.Min.X+x, bb.Min.Y+y).RGBA()
			for _, d := range []float64{
				float64(r1>>8) - float64(r2>>8), float64(g1>>8) - float64(g2>>8), float64(b1>>8) - float64(b2>>8),
			} {
				if d < 0 {
					d = -d
				}
				sum += d
				n++
			}
		}
	}
	return sum / n
}
