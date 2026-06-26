package facematch

import (
	"testing"

	"github.com/panbotka/kukatko/internal/vectors"
)

// cand builds a face candidate with the given subject, photo, distance and width.
func cand(photoUID, subjectUID, name string, distance, width float64) vectors.FaceCandidate {
	c := vectors.FaceCandidate{
		PhotoUID:    photoUID,
		Distance:    distance,
		BBox:        [4]float64{0.1, 0.1, width, 0.3},
		SubjectName: name,
	}
	if subjectUID != "" {
		c.SubjectUID = &subjectUID
	}
	return c
}

// TestAggregateSuggestions_filtering checks every filtering rule and the ranking.
func TestAggregateSuggestions_filtering(t *testing.T) {
	t.Parallel()

	candidates := []vectors.FaceCandidate{
		cand("p2", "su_alice", "Alice", 0.10, 0.3),   // closest Alice
		cand("p3", "su_alice", "Alice", 0.30, 0.3),   // farther Alice (averages in)
		cand("p4", "su_bob", "Bob", 0.20, 0.3),       // Bob
		cand("p1", "su_eve", "Eve", 0.05, 0.3),       // self photo → skipped
		cand("p5", "", "", 0.05, 0.3),                // unassigned → skipped
		cand("p6", "su_carol", "Carol", 0.05, 0.005), // too small → skipped
		cand("p7", "su_dan", "Dan", 0.05, 0.3),       // excluded subject → skipped
	}
	exclude := map[string]bool{"su_dan": true}

	got := aggregateSuggestions(candidates, "p1", exclude, 0.02, 5)

	if len(got) != 2 {
		t.Fatalf("got %d suggestions, want 2: %+v", len(got), got)
	}
	// Alice avg distance = (0.10+0.30)/2 = 0.20, Bob = 0.20 — tie on confidence,
	// broken by ascending distance (equal) then subject uid (su_alice < su_bob).
	if got[0].SubjectUID != "su_alice" {
		t.Errorf("got[0] = %+v, want su_alice first", got[0])
	}
	if got[0].Distance != 0.20 {
		t.Errorf("Alice distance = %v, want 0.20 (averaged)", got[0].Distance)
	}
	if got[0].Confidence != 0.80 {
		t.Errorf("Alice confidence = %v, want 0.80", got[0].Confidence)
	}
	if got[1].SubjectUID != "su_bob" {
		t.Errorf("got[1] = %+v, want su_bob", got[1])
	}
}

// TestAggregateSuggestions_limit checks the result is truncated and ordered by
// confidence (closest subject first).
func TestAggregateSuggestions_limit(t *testing.T) {
	t.Parallel()

	candidates := []vectors.FaceCandidate{
		cand("p2", "su_a", "A", 0.40, 0.3),
		cand("p3", "su_b", "B", 0.10, 0.3),
		cand("p4", "su_c", "C", 0.20, 0.3),
	}
	got := aggregateSuggestions(candidates, "p1", nil, 0, 2)
	if len(got) != 2 {
		t.Fatalf("got %d suggestions, want 2", len(got))
	}
	if got[0].SubjectUID != "su_b" || got[1].SubjectUID != "su_c" {
		t.Errorf("order = %s,%s, want su_b,su_c (nearest first)", got[0].SubjectUID, got[1].SubjectUID)
	}
}

// TestAggregateSuggestions_confidenceFloor checks a far neighbour (distance > 1)
// clamps confidence at zero rather than going negative.
func TestAggregateSuggestions_confidenceFloor(t *testing.T) {
	t.Parallel()

	got := aggregateSuggestions(
		[]vectors.FaceCandidate{cand("p2", "su_a", "A", 1.4, 0.3)}, "p1", nil, 0, 5,
	)
	if len(got) != 1 {
		t.Fatalf("got %d suggestions, want 1", len(got))
	}
	if got[0].Confidence != 0 {
		t.Errorf("confidence = %v, want 0 (clamped)", got[0].Confidence)
	}
}

// TestAggregateSuggestions_emptyInput checks no candidates yields an empty,
// non-nil-friendly slice.
func TestAggregateSuggestions_emptyInput(t *testing.T) {
	t.Parallel()
	if got := aggregateSuggestions(nil, "p1", nil, 0.02, 5); len(got) != 0 {
		t.Errorf("got %d suggestions, want 0", len(got))
	}
}
