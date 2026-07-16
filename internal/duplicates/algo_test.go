package duplicates

import (
	"testing"
	"time"
)

// TestHamming checks the bit-difference count on representative inputs.
func TestHamming(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a, b uint64
		want int
	}{
		{name: "identical", a: 0xDEAD, b: 0xDEAD, want: 0},
		{name: "one bit", a: 0, b: 1, want: 1},
		{name: "three low bits", a: 0, b: 0b111, want: 3},
		{name: "all bits", a: 0, b: ^uint64(0), want: 64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := hamming(tt.a, tt.b); got != tt.want {
				t.Errorf("hamming(%x, %x) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestBandCount checks the pigeonhole band count and its clamping.
func TestBandCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		maxDiff int
		want    int
	}{
		{name: "zero diff needs one band", maxDiff: 0, want: 1},
		{name: "diff eight needs nine bands", maxDiff: 8, want: 9},
		{name: "negative clamps to one", maxDiff: -5, want: 1},
		{name: "huge clamps to 64", maxDiff: 1000, want: 64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := bandCount(tt.maxDiff); got != tt.want {
				t.Errorf("bandCount(%d) = %d, want %d", tt.maxDiff, got, tt.want)
			}
		})
	}
}

// TestBandKeys_pigeonhole verifies the core LSH guarantee: two hashes within
// maxDiff bits share at least one band key, while clearly distinct hashes do not.
func TestBandKeys_pigeonhole(t *testing.T) {
	t.Parallel()
	bands := bandCount(8)
	base := uint64(0x0123456789ABCDEF)
	near := base ^ 0b1011 // three differing bits, well within 8

	if !sharesKey(bandKeys(base, bands), bandKeys(near, bands)) {
		t.Errorf("near-duplicate hashes did not share a band key")
	}

	far := ^base // every bit flipped
	if sharesKey(bandKeys(base, bands), bandKeys(far, bands)) {
		t.Errorf("opposite hashes unexpectedly shared a band key")
	}
}

// TestBandKeys_count checks that the number of keys equals the band count.
func TestBandKeys_count(t *testing.T) {
	t.Parallel()
	for _, bands := range []int{1, 4, 9, 64} {
		if got := len(bandKeys(0xFF00FF00FF00FF00, bands)); got != bands {
			t.Errorf("bandKeys with %d bands returned %d keys", bands, got)
		}
	}
}

// sharesKey reports whether the two key slices have any value in common.
func sharesKey(a, b []uint64) bool {
	set := make(map[uint64]bool, len(a))
	for _, k := range a {
		set[k] = true
	}
	for _, k := range b {
		if set[k] {
			return true
		}
	}
	return false
}

// TestUnionFind checks merge and root resolution.
func TestUnionFind(t *testing.T) {
	t.Parallel()
	uf := newUnionFind(5)
	uf.union(0, 1)
	uf.union(1, 2)
	uf.union(3, 4)

	if uf.find(0) != uf.find(2) {
		t.Errorf("0 and 2 should share a root")
	}
	if uf.find(0) == uf.find(3) {
		t.Errorf("0 and 3 should be in different sets")
	}
	// Union of already-joined nodes is a no-op.
	uf.union(0, 2)
	if uf.find(0) != uf.find(1) {
		t.Errorf("idempotent union broke the set")
	}
}

// TestPhashUnion groups near hashes and leaves distinct ones apart.
func TestPhashUnion(t *testing.T) {
	t.Parallel()
	entries := []phashEntry{
		{idx: 0, phash: 0},
		{idx: 1, phash: 0b11},       // 2 bits from node 0
		{idx: 2, phash: ^uint64(0)}, // far from everything
		{idx: 3, phash: 0b1},        // 1 bit from node 0
	}
	uf := newUnionFind(4)
	matched := phashUnion(entries, 8, uf, nil)

	if uf.find(0) != uf.find(1) || uf.find(0) != uf.find(3) {
		t.Errorf("near hashes were not unioned: roots %d %d %d", uf.find(0), uf.find(1), uf.find(3))
	}
	if uf.find(0) == uf.find(2) {
		t.Errorf("far hash 2 was unioned with 0")
	}
	for _, idx := range []int{0, 1, 3} {
		if !matched[idx] {
			t.Errorf("node %d should be marked pHash-matched", idx)
		}
	}
	if matched[2] {
		t.Errorf("node 2 should not be pHash-matched")
	}
}

// TestPhashUnion_disabled checks that a negative maxDiff links nothing.
func TestPhashUnion_disabled(t *testing.T) {
	t.Parallel()
	entries := []phashEntry{{idx: 0, phash: 0}, {idx: 1, phash: 0}}
	uf := newUnionFind(2)
	matched := phashUnion(entries, -1, uf, nil)
	if uf.find(0) == uf.find(1) {
		t.Errorf("disabled pHash union linked identical hashes")
	}
	if len(matched) != 0 {
		t.Errorf("disabled pHash union reported matches: %v", matched)
	}
}

// TestOrderedPair checks the order-independent key.
func TestOrderedPair(t *testing.T) {
	t.Parallel()
	if orderedPair(3, 1) != orderedPair(1, 3) {
		t.Errorf("orderedPair is not symmetric")
	}
	if orderedPair(1, 3) != [2]int{1, 3} {
		t.Errorf("orderedPair(1,3) = %v, want [1 3]", orderedPair(1, 3))
	}
}

// TestSelectKeeperIndex exercises the resolution → size → age → uid order.
func TestSelectKeeperIndex(t *testing.T) {
	t.Parallel()
	older := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		members []Member
		want    int
	}{
		{
			name: "highest resolution wins",
			members: []Member{
				{UID: "a", FileWidth: 100, FileHeight: 100},
				{UID: "b", FileWidth: 200, FileHeight: 200},
			},
			want: 1,
		},
		{
			name: "file size breaks resolution tie",
			members: []Member{
				{UID: "a", FileWidth: 100, FileHeight: 100, FileSize: 10},
				{UID: "b", FileWidth: 100, FileHeight: 100, FileSize: 99},
			},
			want: 1,
		},
		{
			name: "oldest breaks size tie",
			members: []Member{
				{UID: "a", FileWidth: 1, FileHeight: 1, FileSize: 1, sortTime: newer},
				{UID: "b", FileWidth: 1, FileHeight: 1, FileSize: 1, sortTime: older},
			},
			want: 1,
		},
		{
			name: "uid breaks final tie",
			members: []Member{
				{UID: "b", FileWidth: 1, FileHeight: 1, sortTime: older},
				{UID: "a", FileWidth: 1, FileHeight: 1, sortTime: older},
			},
			want: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := selectKeeperIndex(tt.members); got != tt.want {
				t.Errorf("selectKeeperIndex = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestSortGroups orders by size, then newest keeper, then id.
func TestSortGroups(t *testing.T) {
	t.Parallel()
	older := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	groups := []Group{
		{ID: "small", Members: make([]Member, 2), keeperSortTime: newer},
		{ID: "big", Members: make([]Member, 3), keeperSortTime: older},
		{ID: "small-old", Members: make([]Member, 2), keeperSortTime: older},
	}
	sortGroups(groups)
	wantOrder := []string{"big", "small", "small-old"}
	for i, want := range wantOrder {
		if groups[i].ID != want {
			t.Errorf("group[%d].ID = %q, want %q", i, groups[i].ID, want)
		}
	}
}

// TestMinUID returns the lexicographically smallest uid.
func TestMinUID(t *testing.T) {
	t.Parallel()
	members := []Member{{UID: "ph_c"}, {UID: "ph_a"}, {UID: "ph_b"}}
	if got := minUID(members); got != "ph_a" {
		t.Errorf("minUID = %q, want ph_a", got)
	}
}
