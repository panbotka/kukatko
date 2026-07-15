package stacks

import "sort"

// unionFind is a disjoint-set forest over the integer indices 0..n-1, used to
// merge candidates linked by the detection rules into connected components. It
// uses path compression and union by rank for near-constant-time operations.
type unionFind struct {
	parent []int
	rank   []int
}

// newUnionFind returns a forest of n singleton sets, each element its own parent.
func newUnionFind(n int) *unionFind {
	uf := &unionFind{parent: make([]int, n), rank: make([]int, n)}
	for i := range uf.parent {
		uf.parent[i] = i
	}
	return uf
}

// find returns the representative (root) of the set containing i, compressing
// the path to the root as it goes.
func (uf *unionFind) find(i int) int {
	for uf.parent[i] != i {
		uf.parent[i] = uf.parent[uf.parent[i]]
		i = uf.parent[i]
	}
	return i
}

// union merges the sets containing a and b, attaching the shallower tree under
// the deeper one to keep the forest flat.
func (uf *unionFind) union(a, b int) {
	ra, rb := uf.find(a), uf.find(b)
	if ra == rb {
		return
	}
	if uf.rank[ra] < uf.rank[rb] {
		ra, rb = rb, ra
	}
	uf.parent[rb] = ra
	if uf.rank[ra] == uf.rank[rb] {
		uf.rank[ra]++
	}
}

// components returns the connected components of at least two elements, each as a
// sorted slice of member indices, ordered by their smallest member so the result
// is deterministic. Singleton sets (unstacked photos) are omitted.
func components(uf *unionFind, n int) [][]int {
	groups := make(map[int][]int, n)
	for i := range n {
		root := uf.find(i)
		groups[root] = append(groups[root], i)
	}
	out := make([][]int, 0, len(groups))
	for _, members := range groups {
		if len(members) < 2 {
			continue
		}
		sort.Ints(members)
		out = append(out, members)
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}
