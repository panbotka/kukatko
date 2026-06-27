package cluster

import (
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
