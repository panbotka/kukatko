package cluster

import (
	"math"
	"reflect"
	"testing"
)

// TestConnectedComponents covers singletons, chains, disjoint groups and the
// deterministic ordering of the result.
func TestConnectedComponents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		n     int
		edges [][2]int
		want  [][]int
	}{
		{
			name: "all singletons",
			n:    3,
			want: [][]int{{0}, {1}, {2}},
		},
		{
			name:  "single pair plus singleton",
			n:     3,
			edges: [][2]int{{0, 1}},
			want:  [][]int{{0, 1}, {2}},
		},
		{
			name:  "transitive chain merges",
			n:     4,
			edges: [][2]int{{0, 1}, {1, 2}},
			want:  [][]int{{0, 1, 2}, {3}},
		},
		{
			name:  "two disjoint groups",
			n:     4,
			edges: [][2]int{{0, 1}, {2, 3}},
			want:  [][]int{{0, 1}, {2, 3}},
		},
		{
			name:  "duplicate and reversed edges are idempotent",
			n:     3,
			edges: [][2]int{{0, 1}, {1, 0}, {0, 1}},
			want:  [][]int{{0, 1}, {2}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := connectedComponents(tt.n, tt.edges)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("connectedComponents(%d, %v) = %v, want %v", tt.n, tt.edges, got, tt.want)
			}
		})
	}
}

// TestCentroid checks the mean is L2-normalised and that an empty input yields
// nil.
func TestCentroid(t *testing.T) {
	t.Parallel()

	if got := centroid(nil); got != nil {
		t.Errorf("centroid(nil) = %v, want nil", got)
	}

	got := centroid([][]float32{{2, 0}, {0, 2}})
	want := float32(1 / math.Sqrt2)
	if math.Abs(float64(got[0]-want)) > 1e-6 || math.Abs(float64(got[1]-want)) > 1e-6 {
		t.Errorf("centroid = %v, want [%g %g]", got, want, want)
	}
	if mag := magnitude(got); math.Abs(mag-1) > 1e-6 {
		t.Errorf("centroid magnitude = %g, want 1", mag)
	}
}

// TestNormalizeZero verifies a zero vector is returned unchanged (no NaNs).
func TestNormalizeZero(t *testing.T) {
	t.Parallel()
	got := normalize([]float32{0, 0, 0})
	for _, x := range got {
		if x != 0 {
			t.Fatalf("normalize(zero) = %v, want all zeros", got)
		}
	}
}

// TestCosineDistance covers identical, orthogonal, opposite and zero vectors.
func TestCosineDistance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b []float32
		want float64
	}{
		{name: "identical", a: []float32{1, 0}, b: []float32{1, 0}, want: 0},
		{name: "orthogonal", a: []float32{1, 0}, b: []float32{0, 1}, want: 1},
		{name: "opposite", a: []float32{1, 0}, b: []float32{-1, 0}, want: 2},
		{name: "zero operand", a: []float32{0, 0}, b: []float32{1, 0}, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := cosineDistance(tt.a, tt.b)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("cosineDistance(%v, %v) = %g, want %g", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestNearestToCentroid checks the closest face to the centroid wins.
func TestNearestToCentroid(t *testing.T) {
	t.Parallel()
	faces := []Face{
		{Vector: []float32{0, 1}},
		{Vector: []float32{1, 0.1}},
		{Vector: []float32{-1, 0}},
	}
	if got := nearestToCentroid([]float32{1, 0}, faces); got != 1 {
		t.Errorf("nearestToCentroid = %d, want 1", got)
	}
}

// magnitude returns the L2 norm of v, a test helper for normalisation checks.
func magnitude(v []float32) float64 {
	var sumSq float64
	for _, x := range v {
		sumSq += float64(x) * float64(x)
	}
	return math.Sqrt(sumSq)
}
