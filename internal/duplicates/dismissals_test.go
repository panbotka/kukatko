package duplicates

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// fakeFeedback serves a fixed set of dismissed pairs (or an error).
type fakeFeedback struct {
	pairs []feedback.DuplicateDismissalKey
	err   error
}

// DismissedDuplicatePairs returns the canned pairs or error.
func (f fakeFeedback) DismissedDuplicatePairs(
	_ context.Context,
) ([]feedback.DuplicateDismissalKey, error) {
	return f.pairs, f.err
}

// dismissal builds a dismissed pair for the two uids.
func dismissal(a, b string) feedback.DuplicateDismissalKey {
	return feedback.DuplicateDismissalKey{PhotoUID: a, OtherUID: b}
}

// TestFindGroups_dismissedPairDropped checks that dismissing the only edge of a
// two-photo group makes the group disappear: with no edge left both photos are
// singletons, and singletons are not duplicates. This is the whole point of
// persisting the decision — the scan re-runs from scratch, so a dismissal that the
// scan did not read back would offer the same pair forever.
func TestFindGroups_dismissedPairDropped(t *testing.T) {
	t.Parallel()
	hashes := []photos.Phash{
		{PhotoUID: "ph_a", Phash: 0},
		{PhotoUID: "ph_b", Phash: 0b11}, // 2 bits from a -> would be a duplicate
	}
	photoCat := catalogue(
		makePhoto("ph_a", 100, 100, 10, baseTime),
		makePhoto("ph_b", 200, 200, 40, baseTime),
	)
	svc := New(Config{
		Photos:       photoCat,
		Phashes:      fakePhashes{hashes: hashes},
		Feedback:     fakeFeedback{pairs: []feedback.DuplicateDismissalKey{dismissal("ph_a", "ph_b")}},
		PhashMaxDiff: 8,
	})

	res, err := svc.FindGroups(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("FindGroups: %v", err)
	}
	if res.Total != 0 || len(res.Groups) != 0 {
		t.Fatalf("got %d groups (total %d), want 0 — the dismissed pair came back", len(res.Groups), res.Total)
	}
}

// TestFindGroups_dismissalIsUnordered checks a dismissal stored in either argument
// order suppresses the same edge. The pair is unordered, so a caller naming the
// photos the other way round must not resurrect the group.
func TestFindGroups_dismissalIsUnordered(t *testing.T) {
	t.Parallel()
	hashes := []photos.Phash{
		{PhotoUID: "ph_a", Phash: 0},
		{PhotoUID: "ph_b", Phash: 0b11},
	}
	photoCat := catalogue(
		makePhoto("ph_a", 100, 100, 10, baseTime),
		makePhoto("ph_b", 200, 200, 40, baseTime),
	)
	svc := New(Config{
		Photos:  photoCat,
		Phashes: fakePhashes{hashes: hashes},
		// Reversed relative to the uids' lexicographic order.
		Feedback:     fakeFeedback{pairs: []feedback.DuplicateDismissalKey{dismissal("ph_b", "ph_a")}},
		PhashMaxDiff: 8,
	})

	res, err := svc.FindGroups(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("FindGroups: %v", err)
	}
	if res.Total != 0 {
		t.Fatalf("got %d groups, want 0 — a reversed dismissal did not suppress the edge", res.Total)
	}
}

// TestFindGroups_dismissedEmbeddingPairDropped checks the dismissal suppresses an
// embedding edge too, not just a pHash one. Both linking steps must honour it, or
// dismissing a pair the pHash pass missed would do nothing.
func TestFindGroups_dismissedEmbeddingPairDropped(t *testing.T) {
	t.Parallel()
	hashes := []photos.Phash{
		{PhotoUID: "ph_a", Phash: 0},
		{PhotoUID: "ph_b", Phash: -1}, // far apart: only the embedding could link them
	}
	photoCat := catalogue(
		makePhoto("ph_a", 100, 100, 10, baseTime),
		makePhoto("ph_b", 200, 200, 40, baseTime),
	)
	embeds := fakeEmbeddings{pairs: []vectors.DuplicatePair{{A: "ph_a", B: "ph_b", Distance: 0.01}}}
	svc := New(Config{
		Photos:           photoCat,
		Phashes:          fakePhashes{hashes: hashes},
		Embeddings:       embeds,
		Feedback:         fakeFeedback{pairs: []feedback.DuplicateDismissalKey{dismissal("ph_a", "ph_b")}},
		PhashMaxDiff:     2,
		EmbeddingMaxDist: 0.2,
	})

	res, err := svc.FindGroups(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("FindGroups: %v", err)
	}
	if res.Total != 0 {
		t.Fatalf("got %d groups, want 0 — the dismissed embedding edge was still drawn", res.Total)
	}
}

// TestFindGroups_dismissedPairKeepsLargerGroup checks that dismissing one edge of a
// three-photo group only removes that edge: the group survives on its remaining
// ones. Dismissing "A is not B" is not a claim about C, so silently dropping the
// whole group would throw away a decision the user never made.
func TestFindGroups_dismissedPairKeepsLargerGroup(t *testing.T) {
	t.Parallel()
	// All three are within 2 bits of each other, so every edge exists.
	hashes := []photos.Phash{
		{PhotoUID: "ph_a", Phash: 0},
		{PhotoUID: "ph_b", Phash: 0b1},
		{PhotoUID: "ph_c", Phash: 0b11},
	}
	photoCat := catalogue(
		makePhoto("ph_a", 100, 100, 10, baseTime),
		makePhoto("ph_b", 200, 200, 40, baseTime),
		makePhoto("ph_c", 150, 150, 20, baseTime),
	)
	svc := New(Config{
		Photos:       photoCat,
		Phashes:      fakePhashes{hashes: hashes},
		Feedback:     fakeFeedback{pairs: []feedback.DuplicateDismissalKey{dismissal("ph_a", "ph_b")}},
		PhashMaxDiff: 8,
	})

	res, err := svc.FindGroups(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("FindGroups: %v", err)
	}
	if res.Total != 1 {
		t.Fatalf("got %d groups, want 1 — a-c and b-c still link the three", res.Total)
	}
	if n := len(res.Groups[0].Members); n != 3 {
		t.Fatalf("group has %d members, want 3", n)
	}
}

// TestFindGroups_dismissalOfUnknownPhotoIgnored checks a dismissal naming a uid the
// scan does not know (an archived or purged photo) is skipped rather than
// mis-suppressing another pair.
func TestFindGroups_dismissalOfUnknownPhotoIgnored(t *testing.T) {
	t.Parallel()
	hashes := []photos.Phash{
		{PhotoUID: "ph_a", Phash: 0},
		{PhotoUID: "ph_b", Phash: 0b11},
	}
	photoCat := catalogue(
		makePhoto("ph_a", 100, 100, 10, baseTime),
		makePhoto("ph_b", 200, 200, 40, baseTime),
	)
	svc := New(Config{
		Photos:       photoCat,
		Phashes:      fakePhashes{hashes: hashes},
		Feedback:     fakeFeedback{pairs: []feedback.DuplicateDismissalKey{dismissal("ph_a", "ph_gone")}},
		PhashMaxDiff: 8,
	})

	res, err := svc.FindGroups(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("FindGroups: %v", err)
	}
	if res.Total != 1 {
		t.Fatalf("got %d groups, want 1 — an unrelated dismissal suppressed a real pair", res.Total)
	}
}

// TestFindGroups_feedbackError propagates a failing dismissal lookup rather than
// silently scanning without the exclusions, which would re-offer settled pairs.
func TestFindGroups_feedbackError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("boom")
	svc := New(Config{
		Photos:       catalogue(),
		Phashes:      fakePhashes{hashes: []photos.Phash{{PhotoUID: "ph_a", Phash: 0}}},
		Feedback:     fakeFeedback{err: sentinel},
		PhashMaxDiff: 8,
	})

	if _, err := svc.FindGroups(context.Background(), 0, 0); !errors.Is(err, sentinel) {
		t.Fatalf("FindGroups error = %v, want it to wrap %v", err, sentinel)
	}
}
