package video

import (
	"image"
	"image/color"
	"math"
	"time"
)

const (
	// posterSampleWidth is the width a candidate frame is decoded at while the
	// rule compares candidates. The choice only needs the frame's tonal
	// distribution, which survives a heavy downscale, and a 160 px JPEG is orders
	// of magnitude cheaper to encode and to decode in Go than the full frame.
	posterSampleWidth = 160
	// posterBuckets is the number of luminance buckets the histogram is built
	// over. 64 is fine enough that a flat frame collapses into one or two buckets
	// and coarse enough that sensor noise does not read as detail.
	posterBuckets = 64
	// posterScoreGrid caps how many sample points per axis a score reads, so
	// scoring stays constant-time regardless of how large the decoded frame is.
	posterScoreGrid = 96
	// posterUniformDominance is the share of pixels in a single luminance bucket
	// above which a frame counts as near-uniform — a black intro, a white flash,
	// a lens cap. Such a frame is rejected in favour of any candidate that is not
	// near-uniform, and only wins when every candidate is.
	posterUniformDominance = 0.90
	// posterExposureFloor is how far the mean luminance must be from pure black
	// (and from pure white) before the frame stops being discounted: a frame
	// averaging below 10 % — or above 90 % — of full scale is scaled down towards
	// zero in proportion to how close to the extreme it is.
	posterExposureFloor = 0.10
	// posterEndGuard keeps every sample offset this far from the end of the clip,
	// so seeking never lands past the last frame of a short one.
	posterEndGuard = 0.05
	// posterGoodEnough is the value at which a candidate stops the search: a frame
	// this far from flat is a picture, and no later candidate could make the
	// poster meaningfully better. It is what keeps the ordinary clip — whose first
	// candidate is already fine — at roughly the cost of the old fixed seek, and
	// spends the extra decodes only on the clips that open on darkness.
	posterGoodEnough = 0.30
)

// posterFractions are the points in a clip the rule samples, as fractions of its
// duration. They spread across the clip but lean towards its first half, because
// a poster is meant to say what the clip is about and the opening usually does —
// while skipping the very first moments, which are where fades, autofocus and
// lens caps live.
var posterFractions = []float64{0.05, 0.15, 0.30, 0.50, 0.70}

// posterBlindOffsets are the sample offsets (seconds) used when the clip's
// duration is unknown — no ffprobe and no exiftool, so there is nothing to take
// fractions of. Offsets past the end of a short clip simply yield no frame and
// are skipped, which costs a fast failing ffmpeg run and never a wrong poster.
var posterBlindOffsets = []float64{0, 1, 3, 7}

// posterOffsets returns the offsets, in seconds and ascending, at which the
// poster rule samples a clip of the given duration. A non-positive duration
// (unknown) falls back to fixed offsets. Offsets are rounded to whole
// milliseconds and de-duplicated, so a clip too short to spread them over
// collapses to a single offset rather than sampling the same frame five times.
func posterOffsets(duration time.Duration) []float64 {
	if duration <= 0 {
		return append([]float64(nil), posterBlindOffsets...)
	}
	last := math.Max(0, duration.Seconds()-posterEndGuard)
	offsets := make([]float64, 0, len(posterFractions))
	for _, fraction := range posterFractions {
		offsets = append(offsets, roundMillis(math.Min(duration.Seconds()*fraction, last)))
	}
	return dedupOffsets(offsets)
}

// dedupOffsets returns offsets with duplicates removed, keeping the first
// occurrence of each so a ranking already imposed on the slice survives.
func dedupOffsets(offsets []float64) []float64 {
	seen := make(map[int64]struct{}, len(offsets))
	out := make([]float64, 0, len(offsets))
	for _, at := range offsets {
		key := int64(math.Round(at * 1000))
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, at)
	}
	return out
}

// roundMillis rounds a seek offset to whole milliseconds, the resolution the
// offsets are compared and formatted at.
func roundMillis(seconds float64) float64 {
	return math.Round(seconds*1000) / 1000
}

// frameScore is what the rule knows about one candidate frame: the shape of its
// luminance histogram, reduced to the three numbers the choice is made from. It
// is deliberately a value type computed by a pure function, so the rule can be
// tested against synthetic frames without ffmpeg.
type frameScore struct {
	// mean is the average luminance, 0 (black) to 1 (white).
	mean float64
	// spread is the normalised Shannon entropy of the luminance histogram, 0 (all
	// pixels the same tone) to 1 (every tone equally represented). It is the
	// "there is a picture here" measure.
	spread float64
	// dominant is the share of pixels falling in the single most populated
	// luminance bucket, 0 to 1.
	dominant float64
}

// scoreFrame reduces a decoded candidate frame to its frameScore. It reads at
// most posterScoreGrid points per axis on a regular grid, so the cost does not
// depend on the frame's size, and an empty image scores as a uniform black one.
func scoreFrame(img image.Image) frameScore {
	bounds := img.Bounds()
	if bounds.Empty() {
		return frameScore{dominant: 1}
	}
	stepX := max(1, bounds.Dx()/posterScoreGrid)
	stepY := max(1, bounds.Dy()/posterScoreGrid)

	var histogram [posterBuckets]int
	total, sum := 0, 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y += stepY {
		for x := bounds.Min.X; x < bounds.Max.X; x += stepX {
			lum := luminance8(img.At(x, y))
			histogram[lum*posterBuckets/256]++
			sum += lum
			total++
		}
	}
	if total == 0 {
		return frameScore{dominant: 1}
	}
	return frameScore{
		mean:     float64(sum) / float64(total) / 255,
		spread:   normalisedEntropy(histogram[:], total),
		dominant: float64(peak(histogram[:])) / float64(total),
	}
}

// luminance8 returns the Rec. 601 luma of a colour as an 8-bit value. The colour
// model's 16-bit channels are used directly, so no intermediate conversion loses
// precision on the way.
func luminance8(c color.Color) int {
	r, g, b, _ := c.RGBA()
	y := (299*int(r) + 587*int(g) + 114*int(b)) / 1000
	return y >> 8 // 16-bit channel to 8-bit tone
}

// normalisedEntropy returns the Shannon entropy of the histogram divided by its
// maximum (log2 of the bucket count), giving 0 for a single-tone frame and 1 for
// a perfectly even spread.
func normalisedEntropy(histogram []int, total int) float64 {
	entropy := 0.0
	for _, count := range histogram {
		if count == 0 {
			continue
		}
		p := float64(count) / float64(total)
		entropy -= p * math.Log2(p)
	}
	return entropy / math.Log2(float64(len(histogram)))
}

// peak returns the largest count in the histogram.
func peak(histogram []int) int {
	most := 0
	for _, count := range histogram {
		most = max(most, count)
	}
	return most
}

// uniform reports whether the frame is near-uniform — almost every pixel the
// same tone, black and white alike. Such a frame carries no picture and is what
// the rule exists to reject.
func (s frameScore) uniform() bool {
	return s.dominant >= posterUniformDominance
}

// exposure discounts a frame whose average luminance sits against either end of
// the scale: full scale in the usable middle, falling linearly to 0 at pure
// black and pure white. It is what separates "dark but legible" from "the lens
// cap is on" when both happen to carry a little tonal variation.
func (s frameScore) exposure() float64 {
	return clamp01(s.mean/posterExposureFloor) * clamp01((1-s.mean)/posterExposureFloor)
}

// value is the single number candidates are ranked by, higher being better: how
// much tonal spread the frame has, discounted by how badly exposed it is.
func (s frameScore) value() float64 {
	return s.spread * s.exposure()
}

// clamp01 confines v to the closed unit interval.
func clamp01(v float64) float64 {
	return math.Min(1, math.Max(0, v))
}

// betterFrame reports whether a is a better poster than b: a frame that is not
// near-uniform always beats one that is, and otherwise the higher value wins.
// The comparison is strict, so a stable sort over it keeps the earlier (and thus
// earlier-in-the-clip) candidate ahead of an equally good later one.
func betterFrame(a, b frameScore) bool {
	if a.uniform() != b.uniform() {
		return !a.uniform()
	}
	return a.value() > b.value()
}

// goodEnough reports whether the frame is unambiguously a picture, so sampling
// can stop here instead of decoding the candidates after it.
func (s frameScore) goodEnough() bool {
	return !s.uniform() && s.value() >= posterGoodEnough
}

// pickFrame returns the index of the candidate the rule chooses, or -1 when
// there are no candidates at all. It never returns "none of these": with every
// candidate uniform it still answers with the least bad one, because a poster
// from a dark clip is worth more than no poster.
func pickFrame(scores []frameScore) int {
	best := -1
	for i, score := range scores {
		if best < 0 || betterFrame(score, scores[best]) {
			best = i
		}
	}
	return best
}
