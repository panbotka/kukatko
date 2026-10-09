package thumb

import (
	"cmp"
	"image"
	"slices"

	"golang.org/x/image/draw"

	"github.com/panbotka/kukatko/internal/exif"
	"github.com/panbotka/kukatko/internal/imgconvert"
	"github.com/panbotka/kukatko/internal/photoedit"
	"github.com/panbotka/kukatko/internal/photos"
)

// The pure-Go engine's memory is dominated not by the decoded bitmap but by the
// resampler: golang.org/x/image/draw's two-pass kernel scaler allocates a
// dstWidth × srcHeight scratch of four float64s per pixel (32 bytes). Rendering
// fit_3840 straight from a 14142×14142 source needs 3840×14142×32 ≈ 1.7 GB of it,
// and rendering the eight sizes from the source in parallel needed several
// gigabytes for one photo (docs/SECURITY_AUDIT.md SEC-018). So sizes are rendered
// as a cascade instead:
//
//   - the source is first shrunk by an integer box filter (an exact average of
//     k×k blocks, streamed a band at a time) to the smallest size that is still
//     at least as large as every needed rendition — the shrink-on-load libvips
//     does — so the kernel only ever covers the last factor of less than two;
//   - the sizes are rendered largest first, each from the smallest rendition
//     already made that still covers it, so only the first one reads the big
//     base image at all.
//
// The renditions are the same geometry as before (to a pixel of rounding) and
// the kernel is still Catmull-Rom; only the memory and the time change.

// scratchBytesPerPixel is what x/image/draw's kernel scaler allocates per
// dstWidth×srcHeight scratch cell: four float64 channels.
const scratchBytesPerPixel = 32

// renderOrder returns the needed size names largest first (ties by name, so the
// order is deterministic), the order the cascade renders them in.
func renderOrder(needed []string) []string {
	order := slices.Clone(needed)
	slices.SortFunc(order, func(a, b string) int {
		if c := cmp.Compare(sizes[b].Max, sizes[a].Max); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})
	return order
}

// longShort returns the longer and the shorter side of a w×h image.
func longShort(w, h int) (long, short int) {
	return max(w, h), min(w, h)
}

// cascadeMargin is how much larger than a size a rendition must be to serve as
// its source. Every resampling step blurs a little, and a chain of steps that
// each shrink by a small factor compounds it; requiring a factor of at least two
// keeps each size within a level or so of rendering it straight from the
// original, while the source it reads is still a fraction of the original's.
const cascadeMargin = 2

// covers reports whether a rendition of w×h may be the source of spec, for a
// base whose sides are srcW×srcH: its side that spec reads — the long one for a
// fit, the short one (the square crop) for a tile — must be cascadeMargin times
// the size, or the base's whole side when the base is smaller than that.
func covers(spec sizeSpec, w, h, srcW, srcH int) bool {
	long, short := longShort(w, h)
	srcLong, srcShort := longShort(srcW, srcH)
	if spec.Mode == modeCropSquare {
		return short >= min(cascadeMargin*spec.Max, srcShort)
	}
	return long >= min(cascadeMargin*spec.Max, srcLong)
}

// boxFactor returns the largest integer k such that a w×h image shrunk by k
// (sides rounded up) still covers every needed size, or 1 when no shrink is
// possible.
func boxFactor(w, h int, needed []string) int {
	long, short := longShort(w, h)
	k := 0
	for _, name := range needed {
		spec := sizes[name]
		side := long
		if spec.Mode == modeCropSquare {
			side = short
		}
		f := max(side/max(spec.Max, 1), 1)
		if k == 0 || f < k {
			k = f
		}
	}
	return max(k, 1)
}

// boxReduce returns img shrunk by the integer factor k, each output pixel the
// average of a k×k block of source pixels (partial blocks at the right and
// bottom edge average what they have), as an *image.RGBA. k <= 1 returns img
// itself. The source is converted a band of k rows at a time, so beyond the
// output it allocates only a width×k band and one row of sums.
func boxReduce(img image.Image, k int) image.Image {
	if k <= 1 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	ow, oh := (w+k-1)/k, (h+k-1)/k
	dst := image.NewRGBA(image.Rect(0, 0, ow, oh))
	band := image.NewRGBA(image.Rect(0, 0, w, k))
	sums := make([]uint64, ow*4)
	for oy := range oh {
		rows := min(k, h-oy*k)
		draw.Draw(band, image.Rect(0, 0, w, rows), img, image.Pt(b.Min.X, b.Min.Y+oy*k), draw.Src)
		clear(sums)
		for r := range rows {
			addBandRow(sums, band.Pix[r*band.Stride:r*band.Stride+w*4], k)
		}
		writeAverages(dst.Pix[oy*dst.Stride:oy*dst.Stride+ow*4], sums, w, k, rows)
	}
	return dst
}

// addBandRow adds one RGBA row of source pixels into sums, block by block of k
// pixels (sums holds four channels per output pixel).
func addBandRow(sums []uint64, row []byte, k int) {
	for x := range len(row) / 4 {
		o := (x / k) * 4
		p := row[x*4 : x*4+4 : x*4+4]
		sums[o] += uint64(p[0])
		sums[o+1] += uint64(p[1])
		sums[o+2] += uint64(p[2])
		sums[o+3] += uint64(p[3])
	}
}

// writeAverages divides one output row's block sums by each block's pixel
// count (rounded to nearest) into out. w is the source width, so the last block
// of a row that does not divide evenly counts only the columns it has.
func writeAverages(out []byte, sums []uint64, w, k, rows int) {
	for ox := range len(out) / 4 {
		n := uint64(min(k, w-ox*k) * rows) //nolint:gosec // G115: both factors are small positive ints.
		for c := range 4 {
			out[ox*4+c] = uint8((sums[ox*4+c] + n/2) / n) //nolint:gosec // G115: an average of bytes fits a byte.
		}
	}
}

// pickSource returns the image the spec should be rendered from: the smallest
// of the renditions made so far that still covers it, else base. base covers
// every needed size by construction (boxFactor).
func pickSource(base image.Image, rendered []image.Image, spec sizeSpec) image.Image {
	bb := base.Bounds()
	best := base
	bestArea := bb.Dx() * bb.Dy()
	for _, r := range rendered {
		rb := r.Bounds()
		if area := rb.Dx() * rb.Dy(); area < bestArea && covers(spec, rb.Dx(), rb.Dy(), bb.Dx(), bb.Dy()) {
			best, bestArea = r, area
		}
	}
	return best
}

// fitSize returns the w×h a srcW×srcH picture is rendered to by a fit of
// maxSide: the longest side scaled down to maxSide, never up.
func fitSize(srcW, srcH, maxSide int) (w, h int) {
	if srcW <= maxSide && srcH <= maxSide {
		return srcW, srcH
	}
	if srcW >= srcH {
		return maxSide, max(srcH*maxSide/srcW, 1)
	}
	return max(srcW*maxSide/srcH, 1), maxSide
}

// renderBytes returns the scale scratch and the output bitmap renderSpec
// allocates rendering spec from a w×h source.
func renderBytes(spec sizeSpec, w, h int) (scratch, output int64) {
	if spec.Mode == modeCropSquare {
		sq := int64(min(w, h))
		side := int64(spec.Max)
		return side * sq * scratchBytesPerPixel, imgconvert.RGBABytes(spec.Max, spec.Max)
	}
	dw, dh := fitSize(w, h, spec.Max)
	if dw == w && dh == h {
		return 0, 0 // returned unchanged, nothing allocated
	}
	return int64(dw) * int64(h) * scratchBytesPerPixel, imgconvert.RGBABytes(dw, dh)
}

// cascadeBytes is an upper bound on what rendering needed from a w×h image
// allocates beyond the image itself: the box-reduced base and its band, the
// largest single scale scratch (the cascade renders one size at a time, and
// every later size reads a smaller source than the base), and every rendition,
// which may all be alive at once while their encodes run.
func cascadeBytes(w, h int, needed []string) int64 {
	k := boxFactor(w, h, needed)
	bw, bh := w, h
	var total int64
	if k > 1 {
		bw, bh = (w+k-1)/k, (h+k-1)/k
		total += imgconvert.RGBABytes(bw, bh) + imgconvert.RGBABytes(w, k)
	}
	var maxScratch int64
	for _, name := range needed {
		scratch, output := renderBytes(sizes[name], bw, bh)
		maxScratch = max(maxScratch, scratch)
		total += output
	}
	return total + maxScratch
}

// pureGoCost is the extra memory, beyond the decoded bitmap itself, that the
// pure-Go engine holds while rendering needed for a source with header cfg: the
// full-size oriented copy when the orientation turns or flips the picture, two
// full-size copies for a non-destructive edit (crop/rotate/colour each allocate
// one, and the previous stays referenced until the next is made), and the
// cascade. It is what encodePureGo reserves from the decode budget alongside the
// bitmap.
func pureGoCost(cfg image.Config, orientation int, edit photos.Edit, needed []string) int64 {
	w, h := cfg.Width, cfg.Height
	var total int64
	if orientation > 1 && orientation <= 8 {
		total += imgconvert.RGBABytes(w, h)
		if exif.QuarterTurn(orientation) {
			w, h = h, w
		}
	}
	if photoedit.IsIdentity(edit) {
		return total + cascadeBytes(w, h, needed)
	}
	// An edit may crop (smaller) and turn the picture a quarter (sides swap);
	// charge the larger of the two layouts.
	const editCopies = 2
	return total + editCopies*imgconvert.RGBABytes(w, h) +
		max(cascadeBytes(w, h, needed), cascadeBytes(h, w, needed))
}
