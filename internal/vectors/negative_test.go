package vectors_test

import (
	"math"
	"testing"

	"github.com/panbotka/kukatko/internal/vectors"
)

// vec is a small helper building a 3-element vector for the margin-test cases; the
// negative-exemplar rule only needs cosine distances, so a low dimension is enough.
func vec(a, b, c float32) []float32 { return []float32{a, b, c} }

// TestNearestDistance checks the nearest-distance primitive: the minimum over a set
// and the +Inf sentinel for an empty set.
func TestNearestDistance(t *testing.T) {
	t.Parallel()

	if got := vectors.NearestDistance(vec(1, 0, 0), nil); !math.IsInf(got, 1) {
		t.Errorf("NearestDistance over empty set = %v, want +Inf", got)
	}

	// Query aligned with the second exemplar: distance 0 to it, ~1 to the first.
	set := [][]float32{vec(0, 1, 0), vec(1, 0, 0)}
	if got := vectors.NearestDistance(vec(1, 0, 0), set); math.Abs(got) > 1e-6 {
		t.Errorf("NearestDistance = %v, want ~0 (aligned exemplar present)", got)
	}
}

// TestIsNegativeExemplar exercises the rule: the no-rejection no-op, a candidate
// nearer a rejection (dropped), a candidate nearer an accepted exemplar (kept), the
// deterministic tie (kept), and the accepted-empty case.
func TestIsNegativeExemplar(t *testing.T) {
	t.Parallel()

	accepted := [][]float32{vec(1, 0, 0)} // "the subject looks like this"
	rejected := [][]float32{vec(0, 1, 0)} // "the user said no to this"

	tests := []struct {
		name      string
		candidate []float32
		accepted  [][]float32
		rejected  [][]float32
		want      bool
	}{
		{
			name:      "no rejections is a no-op",
			candidate: vec(0, 1, 0), // sits on a rejected direction...
			accepted:  accepted,
			rejected:  nil, // ...but with no rejections it survives
			want:      false,
		},
		{
			name:      "nearer a rejected exemplar is dropped",
			candidate: vec(0.2, 1, 0), // closer to rejected (0,1,0) than accepted (1,0,0)
			accepted:  accepted,
			rejected:  rejected,
			want:      true,
		},
		{
			name:      "nearer an accepted exemplar survives",
			candidate: vec(1, 0.2, 0), // closer to accepted (1,0,0) than rejected (0,1,0)
			accepted:  accepted,
			rejected:  rejected,
			want:      false,
		},
		{
			name:      "an exact tie survives (deterministic, strictly-closer drops)",
			candidate: vec(1, 1, 0), // equidistant from (1,0,0) and (0,1,0)
			accepted:  accepted,
			rejected:  rejected,
			want:      false,
		},
		{
			name:      "no accepted exemplars but a rejection drops a nearby candidate",
			candidate: vec(0.2, 1, 0),
			accepted:  nil,
			rejected:  rejected,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := vectors.IsNegativeExemplar(tt.candidate, tt.accepted, tt.rejected)
			if got != tt.want {
				t.Errorf("IsNegativeExemplar(%v) = %v, want %v", tt.candidate, got, tt.want)
			}
		})
	}
}
