package vectors

import (
	"math"
	"sort"
)

// Centroid returns the L2-normalised mean of vecs, the cosine-space centre of a
// set of embeddings. It returns nil when vecs is empty; a zero-magnitude mean
// (which cannot arise from normalised inputs) is returned without normalisation
// rather than producing NaNs. Vectors shorter than the first are summed over
// their common prefix only.
func Centroid(vecs [][]float32) []float32 {
	if len(vecs) == 0 {
		return nil
	}
	dim := len(vecs[0])
	sum := make([]float64, dim)
	for _, v := range vecs {
		for i := 0; i < dim && i < len(v); i++ {
			sum[i] += float64(v[i])
		}
	}
	mean := make([]float32, dim)
	for i := range sum {
		mean[i] = float32(sum[i] / float64(len(vecs)))
	}
	return Normalize(mean)
}

// TrimmedCentroid returns the centroid of vecs computed with a trimmed mean:
// the plain Centroid first, then the trim vectors furthest from it (by cosine
// distance) are discarded and the centroid is recomputed over the remainder.
// A handful of badly misplaced vectors would otherwise drag the plain mean
// toward themselves and mask exactly the outliers a caller wants to score, so
// the recomputed centre is robust against them. The selection is deterministic:
// distance ties keep the earlier vector. A trim of zero or below, or one that
// would leave no vectors, falls back to the plain Centroid; an empty vecs
// yields nil.
func TrimmedCentroid(vecs [][]float32, trim int) []float32 {
	if trim <= 0 || len(vecs)-trim < 1 {
		return Centroid(vecs)
	}
	centroid := Centroid(vecs)
	distances := make([]float64, len(vecs))
	order := make([]int, len(vecs))
	for i := range vecs {
		distances[i] = CosineDistance(centroid, vecs[i])
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return distances[order[a]] < distances[order[b]]
	})
	kept := make([][]float32, 0, len(vecs)-trim)
	for _, idx := range order[:len(vecs)-trim] {
		kept = append(kept, vecs[idx])
	}
	return Centroid(kept)
}

// Normalize scales v to unit L2 length, returning it unchanged when its
// magnitude is zero so the result never contains NaNs.
func Normalize(v []float32) []float32 {
	var sumSq float64
	for _, x := range v {
		sumSq += float64(x) * float64(x)
	}
	norm := math.Sqrt(sumSq)
	if norm == 0 {
		return v
	}
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) / norm)
	}
	return out
}

// CosineDistance returns the cosine distance (1 - cosine similarity) between a
// and b, in [0, 2]. Vectors of unequal length compare over their common prefix;
// a zero-magnitude operand yields the maximum distance of 1.
func CosineDistance(a, b []float32) float64 {
	var dot, na, nb float64
	n := min(len(b), len(a))
	for i := range n {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 1
	}
	return 1 - dot/(math.Sqrt(na)*math.Sqrt(nb))
}
