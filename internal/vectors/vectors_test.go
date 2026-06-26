package vectors

import (
	"slices"
	"testing"
)

// TestHalfVecRoundTrip checks that ToHalfVec and FromHalfVec preserve the slice.
func TestHalfVecRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		vec  []float32
	}{
		{name: "empty", vec: []float32{}},
		{name: "single", vec: []float32{1.5}},
		{name: "several", vec: []float32{0, -1, 2.25, 3.5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := FromHalfVec(ToHalfVec(tt.vec))
			if !slices.Equal(got, tt.vec) {
				t.Errorf("round trip = %v, want %v", got, tt.vec)
			}
		})
	}
}

// TestNormalizeLimit checks clamping into [1, maxLimit] with a default for
// non-positive inputs.
func TestNormalizeLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{name: "zero uses default", limit: 0, want: defaultLimit},
		{name: "negative uses default", limit: -5, want: defaultLimit},
		{name: "in range kept", limit: 10, want: 10},
		{name: "at max kept", limit: maxLimit, want: maxLimit},
		{name: "over max clamped", limit: maxLimit + 1, want: maxLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeLimit(tt.limit); got != tt.want {
				t.Errorf("normalizeLimit(%d) = %d, want %d", tt.limit, got, tt.want)
			}
		})
	}
}

// TestNormalizeMaxDistance checks that a non-positive distance disables the
// filter while a positive one is passed through.
func TestNormalizeMaxDistance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   float64
		want float64
	}{
		{name: "zero disables", in: 0, want: noDistanceLimit},
		{name: "negative disables", in: -1, want: noDistanceLimit},
		{name: "positive kept", in: 0.25, want: 0.25},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeMaxDistance(tt.in); got != tt.want {
				t.Errorf("normalizeMaxDistance(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
