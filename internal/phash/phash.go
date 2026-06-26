// Package phash computes perceptual hashes (a DCT-based pHash and a difference
// dHash) for decoded images and measures the Hamming distance between them.
//
// The hashes power near-duplicate detection: unlike the SHA256 content hash,
// which only matches byte-identical files, perceptual hashes stay close for
// images that are visually similar (re-encoded, lightly cropped, resized or
// re-compressed copies). Both are 64-bit and are stored as signed integers in
// photo_phashes; a small Hamming distance means "looks the same".
//
// The implementation is pure Go (no CGO): images are reduced to small
// grayscale buffers with golang.org/x/image/draw and the pHash uses a
// hand-rolled separable 2-D DCT.
package phash

import (
	"image"
	"math"
	"math/bits"

	"golang.org/x/image/draw"
)

const (
	// dctSize is the side length of the square grayscale buffer the pHash DCT
	// operates on. 32 is the canonical pHash working size.
	dctSize = 32
	// lowFreq is the side length of the low-frequency DCT block kept for the
	// pHash; 8x8 = 64 coefficients yield a 64-bit hash.
	lowFreq = 8
	// dhashWidth and dhashHeight size the dHash grayscale buffer. The extra
	// column makes width-1 = 8 horizontal comparisons per row, 8 rows = 64 bits.
	dhashWidth  = 9
	dhashHeight = 8
)

// Hashes bundles the two perceptual hashes computed for one image. Both are
// stored as signed 64-bit integers (the raw bit pattern reinterpreted) so they
// map directly onto the photo_phashes columns.
type Hashes struct {
	// Phash is the DCT-based perceptual hash, robust to re-encoding and scaling.
	Phash int64
	// Dhash is the gradient/difference hash, robust to small brightness shifts.
	Dhash int64
}

// dctCoeffs holds the precomputed DCT-II basis: dctCoeffs[u][x] is
// cos((2x+1)·u·π / 2N) for u in [0,lowFreq) and x in [0,dctSize). It is built
// once at package init because the basis is identical for every image.
var dctCoeffs = buildDCTCoeffs()

// buildDCTCoeffs precomputes the separable DCT-II cosine basis used by pHash.
func buildDCTCoeffs() [lowFreq][dctSize]float64 {
	var c [lowFreq][dctSize]float64
	for u := range lowFreq {
		for x := range dctSize {
			c[u][x] = math.Cos((2*float64(x) + 1) * float64(u) * math.Pi / (2 * dctSize))
		}
	}
	return c
}

// Compute returns the perceptual hashes of img. It never fails: any image,
// including a 1x1 pixel, yields a well-defined pair of hashes.
func Compute(img image.Image) Hashes {
	return Hashes{
		//nolint:gosec // G115: hashes are 64 opaque bits; the uint64->int64 reinterpret is deliberate for storage.
		Phash: int64(computePHash(img)),
		//nolint:gosec // G115: hashes are 64 opaque bits; the uint64->int64 reinterpret is deliberate for storage.
		Dhash: int64(computeDHash(img)),
	}
}

// Distance returns the Hamming distance (number of differing bits) between two
// 64-bit hashes. A distance of 0 means identical hashes; small values mean the
// source images are perceptually similar. It works on either hash kind.
func Distance(a, b int64) int {
	//nolint:gosec // G115: reinterpreting the stored hash bits back to uint64 for an XOR popcount is lossless.
	return bits.OnesCount64(uint64(a) ^ uint64(b))
}

// toGray scales src into a fresh w×h grayscale image using bilinear sampling,
// which both resizes and converts to luminance in one pass.
func toGray(src image.Image, width, height int) *image.Gray {
	dst := image.NewGray(image.Rect(0, 0, width, height))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	return dst
}

// computeDHash reduces img to a 9x8 grayscale buffer and sets one bit per
// horizontally adjacent pixel pair (left brighter than right), producing the
// 64-bit difference hash.
func computeDHash(img image.Image) uint64 {
	gray := toGray(img, dhashWidth, dhashHeight)
	var hash uint64
	bit := 0
	for y := range dhashHeight {
		for x := range dhashWidth - 1 {
			if gray.GrayAt(x, y).Y < gray.GrayAt(x+1, y).Y {
				hash |= uint64(1) << bit
			}
			bit++
		}
	}
	return hash
}

// computePHash reduces img to a 32x32 grayscale buffer, takes the top-left 8x8
// block of its 2-D DCT, and sets one bit per coefficient that exceeds the
// block's median (excluding the DC term), producing the 64-bit perceptual hash.
func computePHash(img image.Image) uint64 {
	gray := toGray(img, dctSize, dctSize)
	block := dct8x8(gray)
	threshold := medianExcludingDC(block)

	var hash uint64
	bit := 0
	for u := range lowFreq {
		for v := range lowFreq {
			if block[u][v] > threshold {
				hash |= uint64(1) << bit
			}
			bit++
		}
	}
	return hash
}

// dct8x8 returns the top-left 8x8 block of the 2-D DCT-II of the dctSize-square
// grayscale image, computed separably (rows then columns) using the
// precomputed cosine basis.
func dct8x8(gray *image.Gray) [lowFreq][lowFreq]float64 {
	// rows[u][y] is the 1-D DCT of column y reduced to frequency u.
	var rows [lowFreq][dctSize]float64
	for u := range lowFreq {
		for y := range dctSize {
			var sum float64
			for x := range dctSize {
				sum += float64(gray.GrayAt(x, y).Y) * dctCoeffs[u][x]
			}
			rows[u][y] = sum
		}
	}

	var block [lowFreq][lowFreq]float64
	for u := range lowFreq {
		for v := range lowFreq {
			var sum float64
			for y := range dctSize {
				sum += rows[u][y] * dctCoeffs[v][y]
			}
			block[u][v] = sum
		}
	}
	return block
}

// medianExcludingDC returns the median of the 8x8 DCT block's coefficients
// excluding the [0][0] DC term, which dominates magnitude and would otherwise
// bias the threshold.
func medianExcludingDC(block [lowFreq][lowFreq]float64) float64 {
	values := make([]float64, 0, lowFreq*lowFreq-1)
	for u := range lowFreq {
		for v := range lowFreq {
			if u == 0 && v == 0 {
				continue
			}
			values = append(values, block[u][v])
		}
	}
	insertionSort(values)
	mid := len(values) / 2
	if len(values)%2 == 1 {
		return values[mid]
	}
	return (values[mid-1] + values[mid]) / 2
}

// insertionSort sorts values ascending in place. The slice has 63 elements, so
// an allocation-free insertion sort is simpler than pulling in sort.Float64s
// and well within its efficient range.
func insertionSort(values []float64) {
	for i := 1; i < len(values); i++ {
		v := values[i]
		j := i - 1
		for j >= 0 && values[j] > v {
			values[j+1] = values[j]
			j--
		}
		values[j+1] = v
	}
}
