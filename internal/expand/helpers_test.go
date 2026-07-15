package expand

import (
	"reflect"
	"testing"
)

// TestComputeMinMatchCount checks the vote rule scales with the source-set size and
// the threshold and stays clamped to 1..min(5, sourceCount).
func TestComputeMinMatchCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		sourceCount   int
		threshold     float64
		baseThreshold float64
		want          int
	}{
		{"zero sources is zero", 0, 0.30, 0.30, 0},
		{"single source clamps to one", 1, 0.30, 0.30, 1},
		{"single source never exceeds count", 1, 2.0, 0.30, 1},
		{"four sources at baseline is one", 4, 0.30, 0.30, 1},
		{"eight sources at baseline is one", 8, 0.30, 0.30, 1},
		{"nine sources at baseline is two", 9, 0.30, 0.30, 2},
		{"sixteen sources at baseline is two", 16, 0.30, 0.30, 2},
		{"thirty-six sources at baseline is three", 36, 0.30, 0.30, 3},
		{"large set clamps to five", 400, 0.30, 0.30, 5},
		{"looser threshold raises the count", 9, 0.60, 0.30, 3},
		{"tighter threshold lowers the count", 36, 0.15, 0.30, 2},
		{"three sources cannot demand more than three", 3, 2.0, 0.30, 3},
		{"zero base threshold falls back to ratio one", 9, 0.30, 0, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := computeMinMatchCount(tt.sourceCount, tt.threshold, tt.baseThreshold); got != tt.want {
				t.Errorf("computeMinMatchCount(%d, %v, %v) = %d, want %d",
					tt.sourceCount, tt.threshold, tt.baseThreshold, got, tt.want)
			}
		})
	}
}

// TestSampleSource checks the deterministic source-set sampling: a collection within
// the cap is used whole (uncapped), and a larger one is spread across an even stride.
func TestSampleSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		members    []string
		cap        int
		want       []string
		wantCapped bool
	}{
		{"non-positive cap keeps all", []string{"a", "b", "c"}, 0, []string{"a", "b", "c"}, false},
		{"within cap keeps all", []string{"a", "b"}, 5, []string{"a", "b"}, false},
		{"at cap keeps all", []string{"a", "b", "c"}, 3, []string{"a", "b", "c"}, false},
		{"over cap strides evenly", []string{"a", "b", "c", "d", "e"}, 2, []string{"a", "c"}, true},
		{"over cap strides across a big set", []string{"a", "b", "c", "d", "e", "f"}, 3, []string{"a", "c", "e"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, capped := sampleSource(tt.members, tt.cap)
			if capped != tt.wantCapped {
				t.Errorf("sampleSource capped = %v, want %v", capped, tt.wantCapped)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("sampleSource = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSampleSourceIsDeterministic checks the same over-cap input yields the same
// sample every time, so results are reproducible.
func TestSampleSourceIsDeterministic(t *testing.T) {
	t.Parallel()
	members := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	first, _ := sampleSource(members, 4)
	for range 5 {
		got, _ := sampleSource(members, 4)
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("sampleSource non-deterministic: %v vs %v", got, first)
		}
	}
}

// TestResolveLimit checks the default-and-clamp behaviour of a request limit.
func TestResolveLimit(t *testing.T) {
	t.Parallel()
	svc := &Service{limit: 50, maxLimit: 200}
	tests := []struct {
		name      string
		requested int
		want      int
	}{
		{"zero uses the default", 0, 50},
		{"negative uses the default", -3, 50},
		{"in-range passes through", 120, 120},
		{"over the cap clamps", 500, 200},
		{"at the cap passes", 200, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := svc.resolveLimit(tt.requested); got != tt.want {
				t.Errorf("resolveLimit(%d) = %d, want %d", tt.requested, got, tt.want)
			}
		})
	}
}

// TestNewPanicsOnNilStore checks New treats a missing required store as a wiring bug.
func TestNewPanicsOnNilStore(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("New with nil stores did not panic")
		}
	}()
	_ = New(Config{})
}
