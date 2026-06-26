package facematch

import (
	"math"
	"testing"
)

// TestIoU covers the overlap, no-overlap, containment and degenerate cases for the
// normalised-box Intersection-over-Union.
func TestIoU(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b [4]float64
		want float64
	}{
		{
			name: "identical boxes",
			a:    [4]float64{0.1, 0.1, 0.4, 0.4},
			b:    [4]float64{0.1, 0.1, 0.4, 0.4},
			want: 1,
		},
		{
			name: "disjoint boxes",
			a:    [4]float64{0, 0, 0.2, 0.2},
			b:    [4]float64{0.5, 0.5, 0.2, 0.2},
			want: 0,
		},
		{
			name: "edge-touching boxes do not overlap",
			a:    [4]float64{0, 0, 0.2, 0.2},
			b:    [4]float64{0.2, 0, 0.2, 0.2},
			want: 0,
		},
		{
			name: "half overlap",
			// Two unit-area-0.04 boxes sharing exactly half their width.
			a:    [4]float64{0, 0, 0.2, 0.2},
			b:    [4]float64{0.1, 0, 0.2, 0.2},
			want: 1.0 / 3.0, // inter 0.02, union 0.06
		},
		{
			name: "contained box",
			// b fully inside a: IoU = areaB/areaA = 0.04/0.16 = 0.25.
			a:    [4]float64{0, 0, 0.4, 0.4},
			b:    [4]float64{0.1, 0.1, 0.2, 0.2},
			want: 0.25,
		},
		{
			name: "zero-area query box",
			a:    [4]float64{0.1, 0.1, 0, 0},
			b:    [4]float64{0.1, 0.1, 0.4, 0.4},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IoU(tt.a, tt.b)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("IoU(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestIoU_symmetric checks that IoU does not depend on argument order.
func TestIoU_symmetric(t *testing.T) {
	t.Parallel()
	a := [4]float64{0.05, 0.1, 0.3, 0.25}
	b := [4]float64{0.2, 0.15, 0.3, 0.3}
	if ab, ba := IoU(a, b), IoU(b, a); math.Abs(ab-ba) > 1e-9 {
		t.Errorf("IoU not symmetric: IoU(a,b)=%v IoU(b,a)=%v", ab, ba)
	}
}
