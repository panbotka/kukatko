package duplicates

import (
	"math/bits"
	"sort"
)

// unionFind is a disjoint-set forest over a fixed number of integer nodes with
// path compression and union by rank. It builds the connected components of the
// duplicate graph (nodes are photos, edges are pHash/embedding similarities).
type unionFind struct {
	parent []int
	rank   []int
}

// newUnionFind returns a unionFind over n singleton nodes [0, n).
func newUnionFind(n int) *unionFind {
	uf := &unionFind{parent: make([]int, n), rank: make([]int, n)}
	for i := range uf.parent {
		uf.parent[i] = i
	}
	return uf
}

// find returns the representative root of x, compressing the path on the way up.
func (uf *unionFind) find(x int) int {
	for uf.parent[x] != x {
		uf.parent[x] = uf.parent[uf.parent[x]]
		x = uf.parent[x]
	}
	return x
}

// union merges the sets containing a and b, attaching the shorter tree under the
// taller one. It is a no-op when a and b are already in the same set.
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

// hamming returns the number of differing bits between two 64-bit hashes, i.e.
// the Hamming distance used to compare perceptual hashes.
func hamming(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

// bandCount returns the number of LSH bands to split a 64-bit hash into so that
// any two hashes within maxDiff differing bits are guaranteed to share at least
// one identical band. By the pigeonhole principle that needs maxDiff+1 bands;
// the count is clamped into [1, 64].
func bandCount(maxDiff int) int {
	bandsWanted := maxDiff + 1
	if bandsWanted < 1 {
		return 1
	}
	if bandsWanted > 64 {
		return 64
	}
	return bandsWanted
}

// bandKeys splits hash into bands near-equal segments and returns one key per
// band, each namespaced by its band index so that an equal segment value in a
// different band position never collides. Two hashes that share any returned key
// agree exactly on that band — the candidate-generation step for banded LSH.
func bandKeys(hash uint64, bands int) []uint64 {
	keys := make([]uint64, 0, bands)
	bitsTotal := 64
	start := 0
	for b := range bands {
		// Distribute the 64 bits as evenly as possible across the bands; the
		// first (64 mod bands) bands get one extra bit.
		width := bitsTotal / bands
		if b < bitsTotal%bands {
			width++
		}
		var segment uint64
		if width > 0 {
			segment = (hash >> uint(start)) & ((1 << uint(width)) - 1)
		}
		// Namespace by band index (shift into the high bits) to keep band
		// values from different positions distinct.
		keys = append(keys, (uint64(b)<<56)^segment)
		start += width
	}
	return keys
}

// phashEntry pairs a photo's index with its 64-bit perceptual hash for banded
// scanning.
type phashEntry struct {
	idx   int
	phash uint64
}

// phashUnion connects, via uf, every pair of entries whose perceptual hashes are
// within maxDiff bits. It uses banded LSH to avoid an O(n^2) all-pairs scan:
// entries are bucketed by exact-band agreement, then only the (usually tiny)
// candidate pairs inside each bucket are verified with the full Hamming
// distance. It returns the set of node indices that gained at least one pHash
// edge, so callers can record why a group was formed. maxDiff < 0 disables pHash
// grouping entirely.
func phashUnion(entries []phashEntry, maxDiff int, uf *unionFind) map[int]bool {
	matched := make(map[int]bool)
	if maxDiff < 0 {
		return matched
	}
	buckets := bandBuckets(entries, bandCount(maxDiff))
	seen := make(map[[2]int]bool)
	for _, members := range buckets {
		unionBucket(entries, members, maxDiff, uf, seen, matched)
	}
	return matched
}

// bandBuckets groups entry positions by shared band key: two entries land in the
// same bucket when they agree exactly on at least one of their bands. It is the
// candidate-generation half of the banded LSH.
func bandBuckets(entries []phashEntry, bands int) map[uint64][]int {
	buckets := make(map[uint64][]int)
	for i, e := range entries {
		for _, key := range bandKeys(e.phash, bands) {
			buckets[key] = append(buckets[key], i)
		}
	}
	return buckets
}

// unionBucket verifies each candidate pair in one band bucket with the full
// Hamming distance, unioning and marking those within maxDiff. seen de-duplicates
// pairs that share more than one band so each pair is checked once.
func unionBucket(
	entries []phashEntry, members []int, maxDiff int,
	uf *unionFind, seen map[[2]int]bool, matched map[int]bool,
) {
	for a := range members {
		for b := a + 1; b < len(members); b++ {
			i, j := members[a], members[b]
			pair := orderedPair(i, j)
			if seen[pair] {
				continue
			}
			seen[pair] = true
			if hamming(entries[i].phash, entries[j].phash) <= maxDiff {
				uf.union(entries[i].idx, entries[j].idx)
				matched[entries[i].idx] = true
				matched[entries[j].idx] = true
			}
		}
	}
}

// orderedPair returns a and b as a sorted 2-tuple so an undirected candidate pair
// is keyed identically regardless of argument order.
func orderedPair(a, b int) [2]int {
	if a <= b {
		return [2]int{a, b}
	}
	return [2]int{b, a}
}

// selectKeeperIndex returns the index into members of the best photo to keep: the
// highest pixel resolution, breaking ties by larger file size, then the oldest
// capture/creation time, then the lexicographically smallest uid. members must be
// non-empty.
func selectKeeperIndex(members []Member) int {
	best := 0
	for i := 1; i < len(members); i++ {
		if keeperLess(members[best], members[i]) {
			best = i
		}
	}
	return best
}

// keeperLess reports whether candidate b is a better keeper than a, applying the
// resolution → file size → age → uid preference order.
func keeperLess(a, b Member) bool {
	ra, rb := a.FileWidth*a.FileHeight, b.FileWidth*b.FileHeight
	if ra != rb {
		return rb > ra
	}
	if a.FileSize != b.FileSize {
		return b.FileSize > a.FileSize
	}
	if !a.sortTime.Equal(b.sortTime) {
		return b.sortTime.Before(a.sortTime)
	}
	return b.UID < a.UID
}

// sortGroups orders groups for stable pagination: larger groups first, then by
// the keeper's capture/creation time (newest first), then by group id. The slice
// is sorted in place.
func sortGroups(groups []Group) {
	sort.SliceStable(groups, func(i, j int) bool {
		gi, gj := groups[i], groups[j]
		if len(gi.Members) != len(gj.Members) {
			return len(gi.Members) > len(gj.Members)
		}
		ti, tj := gi.keeperSortTime, gj.keeperSortTime
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return gi.ID < gj.ID
	})
}
