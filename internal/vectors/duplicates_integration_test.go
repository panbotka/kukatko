//go:build integration

package vectors_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/vectors"
)

// TestFindDuplicatePairs links photos within the cosine threshold and excludes
// distant ones, never pairing a photo with itself.
func TestFindDuplicatePairs(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()

	a := makePhoto(t, photoStore, "dup-a")
	b := makePhoto(t, photoStore, "dup-b")
	far := makePhoto(t, photoStore, "dup-far")

	// a and b are almost identical (tiny cosine distance); far is orthogonal.
	saveEmbedding(t, store, a, imageVec(map[int]float32{0: 1, 1: 0.01}))
	saveEmbedding(t, store, b, imageVec(map[int]float32{0: 1, 1: 0.02}))
	saveEmbedding(t, store, far, imageVec(map[int]float32{1: 1}))

	pairs, err := store.FindDuplicatePairs(ctx, 8, 0.05)
	if err != nil {
		t.Fatalf("FindDuplicatePairs: %v", err)
	}

	if !hasPair(pairs, a, b) {
		t.Errorf("expected a<->b pair, got %+v", pairs)
	}
	for _, p := range pairs {
		if p.A == p.B {
			t.Errorf("self-pair returned: %+v", p)
		}
		if p.A == far || p.B == far {
			t.Errorf("distant photo %s should not be paired: %+v", far, p)
		}
	}
}

// TestFindDuplicatePairs_disabled returns no pairs for a non-positive threshold.
func TestFindDuplicatePairs_disabled(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()
	a := makePhoto(t, photoStore, "dis-a")
	b := makePhoto(t, photoStore, "dis-b")
	saveEmbedding(t, store, a, imageVec(map[int]float32{0: 1}))
	saveEmbedding(t, store, b, imageVec(map[int]float32{0: 1}))

	pairs, err := store.FindDuplicatePairs(ctx, 8, 0)
	if err != nil {
		t.Fatalf("FindDuplicatePairs: %v", err)
	}
	if len(pairs) != 0 {
		t.Errorf("got %d pairs, want 0 for disabled threshold", len(pairs))
	}
}

// hasPair reports whether pairs contains the undirected pair {x, y}.
func hasPair(pairs []vectors.DuplicatePair, x, y string) bool {
	for _, p := range pairs {
		if (p.A == x && p.B == y) || (p.A == y && p.B == x) {
			return true
		}
	}
	return false
}
