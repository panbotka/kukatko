package photoedit

import (
	"image"
	"image/color"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// solid builds a w×h opaque RGBA image filled with a single colour.
func solid(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestIsIdentity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		edit photos.Edit
		want bool
	}{
		{name: "zero value is identity", edit: photos.Edit{}, want: true},
		{name: "rotation is not identity", edit: photos.Edit{Rotation: 90}, want: false},
		{name: "brightness is not identity", edit: photos.Edit{Brightness: 0.2}, want: false},
		{name: "contrast is not identity", edit: photos.Edit{Contrast: -0.2}, want: false},
		{
			name: "crop is not identity",
			edit: photos.Edit{CropX: new(0.0), CropY: new(0.0), CropW: new(0.5), CropH: new(0.5)},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsIdentity(tt.edit); got != tt.want {
				t.Errorf("IsIdentity(%+v) = %v, want %v", tt.edit, got, tt.want)
			}
		})
	}
}

func TestApply_identityReturnsInput(t *testing.T) {
	t.Parallel()
	img := solid(4, 4, color.RGBA{R: 100, G: 100, B: 100, A: 255})
	if got := Apply(img, photos.Edit{}); got != image.Image(img) {
		t.Errorf("Apply with identity edit must return the input image unchanged")
	}
}

func TestApply_cropDimensions(t *testing.T) {
	t.Parallel()
	img := solid(100, 80, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	edit := photos.Edit{CropX: new(0.25), CropY: new(0.5), CropW: new(0.5), CropH: new(0.25)}
	out := Apply(img, edit)
	if got := out.Bounds().Dx(); got != 50 {
		t.Errorf("cropped width = %d, want 50", got)
	}
	if got := out.Bounds().Dy(); got != 20 {
		t.Errorf("cropped height = %d, want 20", got)
	}
}

func TestApply_rotationSwapsDimensions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		degrees      int
		wantW, wantH int
	}{
		{name: "0 keeps dimensions", degrees: 0, wantW: 100, wantH: 80},
		{name: "90 swaps dimensions", degrees: 90, wantW: 80, wantH: 100},
		{name: "180 keeps dimensions", degrees: 180, wantW: 100, wantH: 80},
		{name: "270 swaps dimensions", degrees: 270, wantW: 80, wantH: 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			img := solid(100, 80, color.RGBA{R: 1, G: 2, B: 3, A: 255})
			out := Apply(img, photos.Edit{Rotation: tt.degrees})
			if out.Bounds().Dx() != tt.wantW || out.Bounds().Dy() != tt.wantH {
				t.Errorf("rotated bounds = %dx%d, want %dx%d",
					out.Bounds().Dx(), out.Bounds().Dy(), tt.wantW, tt.wantH)
			}
		})
	}
}

func TestApply_rotation90MovesTopLeftToTopRight(t *testing.T) {
	t.Parallel()
	// A 2×2 image: only the top-left pixel is white, the rest black. A 90° CW
	// rotation moves the top-left pixel to the top-right corner.
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	black := color.RGBA{A: 255}
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	for y := range 2 {
		for x := range 2 {
			img.Set(x, y, black)
		}
	}
	img.Set(0, 0, white)

	out := Apply(img, photos.Edit{Rotation: 90})
	gotR, _, _, _ := out.At(1, 0).RGBA()
	if gotR>>8 != 255 {
		t.Errorf("after 90° rotation, top-right pixel R = %d, want 255", gotR>>8)
	}
}

func TestApply_brightnessIncreasesChannels(t *testing.T) {
	t.Parallel()
	img := solid(2, 2, color.RGBA{R: 100, G: 100, B: 100, A: 255})
	out := Apply(img, photos.Edit{Brightness: 0.5})
	r, _, _, _ := out.At(0, 0).RGBA()
	if got := r >> 8; got != 150 {
		t.Errorf("brightness 0.5 on 100 = %d, want 150", got)
	}
}

func TestApply_brightnessClampsToWhite(t *testing.T) {
	t.Parallel()
	img := solid(2, 2, color.RGBA{R: 200, G: 200, B: 200, A: 255})
	out := Apply(img, photos.Edit{Brightness: 1})
	r, _, _, _ := out.At(0, 0).RGBA()
	if got := r >> 8; got != 255 {
		t.Errorf("brightness 1 on 200 must clamp to 255, got %d", got)
	}
}

func TestApply_contrastPushesAwayFromMidpoint(t *testing.T) {
	t.Parallel()
	// A value above the 127.5 mid-point gets brighter with positive contrast.
	img := solid(2, 2, color.RGBA{R: 200, G: 60, B: 127, A: 255})
	out := Apply(img, photos.Edit{Contrast: 0.5})
	r, g, _, _ := out.At(0, 0).RGBA()
	if got := r >> 8; got <= 200 {
		t.Errorf("contrast on 200 (above mid) = %d, want > 200", got)
	}
	if got := g >> 8; got >= 60 {
		t.Errorf("contrast on 60 (below mid) = %d, want < 60", got)
	}
}

func TestApply_preservesAlpha(t *testing.T) {
	t.Parallel()
	img := solid(2, 2, color.RGBA{R: 100, G: 100, B: 100, A: 255})
	out := Apply(img, photos.Edit{Brightness: 0.3, Contrast: 0.3})
	_, _, _, a := out.At(0, 0).RGBA()
	if got := a >> 8; got != 255 {
		t.Errorf("alpha = %d, want 255", got)
	}
}

func TestOrient_dimensions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		orientation  int
		wantW, wantH int
	}{
		{name: "1 is a no-op", orientation: 1, wantW: 100, wantH: 80},
		{name: "3 (rotate 180) keeps dimensions", orientation: 3, wantW: 100, wantH: 80},
		{name: "6 (rotate 90) swaps dimensions", orientation: 6, wantW: 80, wantH: 100},
		{name: "8 (rotate 270) swaps dimensions", orientation: 8, wantW: 80, wantH: 100},
		{name: "out of range is a no-op", orientation: 99, wantW: 100, wantH: 80},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			img := solid(100, 80, color.RGBA{R: 1, G: 2, B: 3, A: 255})
			out := Orient(img, tt.orientation)
			if out.Bounds().Dx() != tt.wantW || out.Bounds().Dy() != tt.wantH {
				t.Errorf("oriented bounds = %dx%d, want %dx%d",
					out.Bounds().Dx(), out.Bounds().Dy(), tt.wantW, tt.wantH)
			}
		})
	}
}

func TestOrient_noOpReturnsInput(t *testing.T) {
	t.Parallel()
	img := solid(4, 4, color.RGBA{A: 255})
	if got := Orient(img, 1); got != image.Image(img) {
		t.Errorf("Orient with orientation 1 must return the input image unchanged")
	}
}
