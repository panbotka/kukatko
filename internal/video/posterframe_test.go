package video

import (
	"image"
	"image/color"
	"math/rand/v2"
	"reflect"
	"testing"
	"time"
)

// solidFrame returns a frame filled with one grey level — the shape of a black
// intro, a white flash or a lens cap.
func solidFrame(level uint8) image.Image {
	img := image.NewGray(image.Rect(0, 0, 160, 120))
	for i := range img.Pix {
		img.Pix[i] = level
	}
	return img
}

// noisyFrame returns a frame whose tones are spread over the full range — the
// shape of a frame that actually shows something. The generator is seeded, so
// the frame (and every score derived from it) is identical on every run.
func noisyFrame() image.Image {
	img := image.NewGray(image.Rect(0, 0, 160, 120))
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range img.Pix {
		img.Pix[i] = uint8(rng.IntN(256))
	}
	return img
}

// dimFrame returns a dark but legible frame: a gradient confined to the bottom
// third of the tonal range, the way a night scene with a light source in it
// looks.
func dimFrame() image.Image {
	img := image.NewGray(image.Rect(0, 0, 160, 120))
	for y := range 120 {
		for x := range 160 {
			img.SetGray(x, y, color.Gray{Y: uint8((x + y) % 90)})
		}
	}
	return img
}

// TestScoreFrame_uniformFrames verifies a single-tone frame reads as uniform
// whatever its tone, and scores no better than nothing.
func TestScoreFrame_uniformFrames(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		level uint8
	}{
		{"black", 0},
		{"white", 255},
		{"mid grey", 128},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			score := scoreFrame(solidFrame(tc.level))
			if !score.uniform() {
				t.Errorf("uniform() = false for a solid frame (dominant %.3f)", score.dominant)
			}
			if score.value() != 0 {
				t.Errorf("value() = %.4f, want 0 for a solid frame", score.value())
			}
		})
	}
}

// TestScoreFrame_pictureBeatsDarkness verifies the numbers behave the way the
// rule assumes: a frame with real tonal spread outscores a near-black one, and
// a dark-but-legible frame outscores a solid black one.
func TestScoreFrame_pictureBeatsDarkness(t *testing.T) {
	t.Parallel()
	picture := scoreFrame(noisyFrame())
	dim := scoreFrame(dimFrame())
	black := scoreFrame(solidFrame(0))

	if picture.uniform() {
		t.Errorf("a full-range frame reads as uniform (dominant %.3f)", picture.dominant)
	}
	if picture.value() <= dim.value() {
		t.Errorf("value: picture %.4f <= dim %.4f", picture.value(), dim.value())
	}
	if dim.value() <= black.value() {
		t.Errorf("value: dim %.4f <= black %.4f", dim.value(), black.value())
	}
}

// TestScoreFrame_emptyImage verifies a frame with no pixels scores as uniform
// rather than dividing by zero.
func TestScoreFrame_emptyImage(t *testing.T) {
	t.Parallel()
	score := scoreFrame(image.NewGray(image.Rect(0, 0, 0, 0)))
	if !score.uniform() {
		t.Error("uniform() = false for an empty image")
	}
}

// TestPickFrame verifies the choosing rule over synthetic candidates: a usable
// later frame beats an all-black first one, an all-uniform set still yields a
// choice, a single candidate is that choice, and no candidates at all is -1.
func TestPickFrame(t *testing.T) {
	t.Parallel()
	black := scoreFrame(solidFrame(0))
	white := scoreFrame(solidFrame(255))
	dim := scoreFrame(dimFrame())
	picture := scoreFrame(noisyFrame())

	for _, tc := range []struct {
		name   string
		scores []frameScore
		want   int
	}{
		{"black first frame, usable later one", []frameScore{black, black, picture, dim}, 2},
		{"every candidate uniform", []frameScore{black, white, black}, 0},
		{"a single candidate", []frameScore{black}, 0},
		{"no candidates", nil, -1},
		{"the least bad of two dark ones", []frameScore{black, dim}, 1},
		{"clip order breaks a tie", []frameScore{picture, picture}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := pickFrame(tc.scores); got != tc.want {
				t.Errorf("pickFrame = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestPosterOffsets verifies the sampling window: offsets spread over a normal
// clip, a short clip still yields distinct usable offsets, a clip shorter than
// the sampling window collapses to one, and an unknown duration falls back to
// fixed seconds.
func TestPosterOffsets(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		duration time.Duration
		want     []float64
	}{
		{
			name:     "a ten second clip",
			duration: 10 * time.Second,
			want:     []float64{0.5, 1.5, 3, 5, 7},
		},
		{
			name:     "a two second clip",
			duration: 2 * time.Second,
			want:     []float64{0.1, 0.3, 0.6, 1, 1.4},
		},
		{
			name:     "a single-frame clip",
			duration: 40 * time.Millisecond,
			want:     []float64{0},
		},
		{
			name:     "an unknown duration",
			duration: 0,
			want:     posterBlindOffsets,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := posterOffsets(tc.duration)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("posterOffsets(%s) = %v, want %v", tc.duration, got, tc.want)
			}
		})
	}
}

// TestPosterOffsets_staysInsideTheClip verifies no sampled offset ever lands at
// or past the end of the clip, whatever its length.
func TestPosterOffsets_staysInsideTheClip(t *testing.T) {
	t.Parallel()
	for _, duration := range []time.Duration{
		30 * time.Millisecond,
		200 * time.Millisecond,
		time.Second,
		2 * time.Second,
		90 * time.Second,
		2 * time.Hour,
	} {
		offsets := posterOffsets(duration)
		if len(offsets) == 0 {
			t.Fatalf("posterOffsets(%s) is empty", duration)
		}
		for _, at := range offsets {
			if at < 0 || at >= duration.Seconds() {
				t.Errorf("posterOffsets(%s) yielded %.3f, outside the clip", duration, at)
			}
		}
	}
}

// TestSampleArgs verifies a candidate frame is seeked, downscaled without
// upscaling and coarsely encoded.
func TestSampleArgs(t *testing.T) {
	t.Parallel()
	got := sampleArgs("/tmp/in.mp4", "/tmp/out.jpg", 1.5)
	want := []string{
		"-nostdin", "-y", "-ss", "1.500", "-i", "/tmp/in.mp4",
		"-frames:v", "1", "-vf", "scale='min(160,iw)':-2", "-q:v", "5", "/tmp/out.jpg",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sampleArgs = %v, want %v", got, want)
	}
}
