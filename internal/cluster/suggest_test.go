package cluster

import (
	"math"
	"testing"

	"github.com/panbotka/kukatko/internal/vectors"
)

// TestBestSubjectSuggestion covers the no-named-candidate case, picking the
// closest subject by average distance, and ignoring unassigned candidates.
func TestBestSubjectSuggestion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		candidates []vectors.FaceCandidate
		wantOK     bool
		wantUID    string
	}{
		{
			name:   "no candidates",
			wantOK: false,
		},
		{
			name: "only unassigned candidates",
			candidates: []vectors.FaceCandidate{
				{Distance: 0.1, SubjectUID: nil},
				{Distance: 0.2, SubjectUID: new("")},
			},
			wantOK: false,
		},
		{
			name: "closest subject wins",
			candidates: []vectors.FaceCandidate{
				{Distance: 0.4, SubjectUID: new("su-far"), SubjectName: "Far"},
				{Distance: 0.1, SubjectUID: new("su-near"), SubjectName: "Near"},
			},
			wantOK:  true,
			wantUID: "su-near",
		},
		{
			name: "averages multiple hits per subject",
			candidates: []vectors.FaceCandidate{
				{Distance: 0.5, SubjectUID: new("su-a"), SubjectName: "A"},
				{Distance: 0.5, SubjectUID: new("su-a"), SubjectName: "A"},
				{Distance: 0.45, SubjectUID: new("su-b"), SubjectName: "B"},
			},
			wantOK:  true,
			wantUID: "su-b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := bestSubjectSuggestion(tt.candidates)
			if ok != tt.wantOK {
				t.Fatalf("bestSubjectSuggestion ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got.SubjectUID != tt.wantUID {
				t.Errorf("bestSubjectSuggestion uid = %q, want %q", got.SubjectUID, tt.wantUID)
			}
		})
	}
}

// TestBestSubjectSuggestionConfidence checks confidence is 1 - distance, clamped
// to a non-negative value.
func TestBestSubjectSuggestionConfidence(t *testing.T) {
	t.Parallel()

	got, ok := bestSubjectSuggestion([]vectors.FaceCandidate{
		{Distance: 0.25, SubjectUID: new("su-x"), SubjectName: "X"},
	})
	if !ok {
		t.Fatal("expected a suggestion")
	}
	if math.Abs(got.Confidence-0.75) > 1e-9 {
		t.Errorf("confidence = %g, want 0.75", got.Confidence)
	}

	clamped, ok := bestSubjectSuggestion([]vectors.FaceCandidate{
		{Distance: 1.5, SubjectUID: new("su-y"), SubjectName: "Y"},
	})
	if !ok || clamped.Confidence != 0 {
		t.Errorf("confidence = %g, want 0 (clamped)", clamped.Confidence)
	}
}
