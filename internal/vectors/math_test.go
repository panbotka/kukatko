package vectors_test

import (
	"math"
	"testing"

	"github.com/panbotka/kukatko/internal/vectors"
)

// TestCentroid checks the mean is L2-normalised and that an empty input yields
// nil.
func TestCentroid(t *testing.T) {
	t.Parallel()

	if got := vectors.Centroid(nil); got != nil {
		t.Errorf("Centroid(nil) = %v, want nil", got)
	}

	got := vectors.Centroid([][]float32{{2, 0}, {0, 2}})
	want := float32(1 / math.Sqrt2)
	if math.Abs(float64(got[0]-want)) > 1e-6 || math.Abs(float64(got[1]-want)) > 1e-6 {
		t.Errorf("Centroid = %v, want [%g %g]", got, want, want)
	}
	if mag := magnitude(got); math.Abs(mag-1) > 1e-6 {
		t.Errorf("Centroid magnitude = %g, want 1", mag)
	}
}

// TestNormalizeZero verifies a zero vector is returned unchanged (no NaNs).
func TestNormalizeZero(t *testing.T) {
	t.Parallel()
	got := vectors.Normalize([]float32{0, 0, 0})
	for _, x := range got {
		if x != 0 {
			t.Fatalf("Normalize(zero) = %v, want all zeros", got)
		}
	}
}

// TestNormalizeUnit verifies a non-zero vector is scaled to unit length.
func TestNormalizeUnit(t *testing.T) {
	t.Parallel()
	got := vectors.Normalize([]float32{3, 4})
	if mag := magnitude(got); math.Abs(mag-1) > 1e-6 {
		t.Errorf("Normalize magnitude = %g, want 1", mag)
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
		{name: "scale invariant", a: []float32{2, 0}, b: []float32{5, 0}, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := vectors.CosineDistance(tt.a, tt.b)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("CosineDistance(%v, %v) = %g, want %g", tt.a, tt.b, got, tt.want)
			}
		})
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
