package cluster

import (
	"math"
	"sort"
)

// connectedComponents groups the integers 0..n-1 into connected components given
// undirected edges between them, using union-find. Each returned component is a
// slice of member indices sorted ascending, and the components themselves are
// ordered by their smallest member, so the result is deterministic for a fixed
// input. Singletons (indices with no edge) are returned as one-element
// components.
func connectedComponents(n int, edges [][2]int) [][]int {
	uf := newUnionFind(n)
	for _, e := range edges {
		uf.union(e[0], e[1])
	}
	groups := make(map[int][]int)
	for i := range n {
		root := uf.find(i)
		groups[root] = append(groups[root], i)
	}
	out := make([][]int, 0, len(groups))
	for _, members := range groups {
		sort.Ints(members)
		out = append(out, members)
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

// unionFind is a disjoint-set forest with union by rank and path compression.
type unionFind struct {
	parent []int
	rank   []int
}

// newUnionFind returns a unionFind over n singleton sets {0}, {1}, …, {n-1}.
func newUnionFind(n int) *unionFind {
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	return &unionFind{parent: parent, rank: make([]int, n)}
}

// find returns the representative (root) of x's set, compressing the path so
// later lookups are near-constant time.
func (u *unionFind) find(x int) int {
	for u.parent[x] != x {
		u.parent[x] = u.parent[u.parent[x]]
		x = u.parent[x]
	}
	return x
}

// union merges the sets containing a and b, attaching the shorter tree under the
// taller one to keep the forest shallow.
func (u *unionFind) union(a, b int) {
	ra, rb := u.find(a), u.find(b)
	if ra == rb {
		return
	}
	if u.rank[ra] < u.rank[rb] {
		ra, rb = rb, ra
	}
	u.parent[rb] = ra
	if u.rank[ra] == u.rank[rb] {
		u.rank[ra]++
	}
}

// centroid returns the L2-normalised mean of vectors, the cosine-space centre of
// a cluster. It returns nil when vectors is empty; a zero-magnitude mean (which
// cannot arise from normalised inputs) is returned without normalisation rather
// than producing NaNs.
func centroid(vectors [][]float32) []float32 {
	if len(vectors) == 0 {
		return nil
	}
	dim := len(vectors[0])
	sum := make([]float64, dim)
	for _, v := range vectors {
		for i := 0; i < dim && i < len(v); i++ {
			sum[i] += float64(v[i])
		}
	}
	mean := make([]float32, dim)
	for i := range sum {
		mean[i] = float32(sum[i] / float64(len(vectors)))
	}
	return normalize(mean)
}

// normalize scales v to unit L2 length, returning it unchanged when its magnitude
// is zero so the result never contains NaNs.
func normalize(v []float32) []float32 {
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

// cosineDistance returns the cosine distance (1 - cosine similarity) between a
// and b, in [0, 2]. Vectors of unequal length compare over their common prefix;
// a zero-magnitude operand yields the maximum distance of 1.
func cosineDistance(a, b []float32) float64 {
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

// nearestToCentroid returns the index of the face in faces whose embedding is
// closest to c by cosine distance — the cluster's representative. It returns 0
// for an empty slice (callers guard against empty clusters).
func nearestToCentroid(c []float32, faces []Face) int {
	best, bestDist := 0, math.Inf(1)
	for i := range faces {
		if d := cosineDistance(c, faces[i].Vector); d < bestDist {
			bestDist = d
			best = i
		}
	}
	return best
}
