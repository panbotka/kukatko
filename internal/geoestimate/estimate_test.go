package geoestimate

import (
	"math"
	"testing"
)

// Reference positions used across the tests. Prague and Vienna are ~250 km
// apart, the distance the "no honest estimate" rule exists for.
var (
	pragueCastle   = Point{Lat: 50.0900, Lng: 14.4000}
	pragueOldTown  = Point{Lat: 50.0870, Lng: 14.4210}
	pragueVysehrad = Point{Lat: 50.0640, Lng: 14.4190}
	vienna         = Point{Lat: 48.2082, Lng: 16.3738}
	sydney         = Point{Lat: -33.8688, Lng: 151.2093}
)

func TestEstimate_coherentNeighbours(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		neighbours []Point
		radius     float64
		want       Point
	}{
		{
			name:       "a single neighbour is its own estimate",
			neighbours: []Point{pragueCastle},
			radius:     5000,
			want:       pragueCastle,
		},
		{
			name:       "identical neighbours collapse to the same point",
			neighbours: []Point{pragueCastle, pragueCastle, pragueCastle},
			radius:     5000,
			want:       pragueCastle,
		},
		{
			name:       "a tight cluster yields its centroid",
			neighbours: []Point{pragueCastle, pragueOldTown, pragueVysehrad},
			radius:     5000,
			want: Point{
				Lat: (pragueCastle.Lat + pragueOldTown.Lat + pragueVysehrad.Lat) / 3,
				Lng: (pragueCastle.Lng + pragueOldTown.Lng + pragueVysehrad.Lng) / 3,
			},
		},
		{
			name:       "the southern hemisphere is not special",
			neighbours: []Point{sydney},
			radius:     5000,
			want:       sydney,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := Estimate(tt.neighbours, tt.radius)
			if !ok {
				t.Fatalf("Estimate(%v, %v) refused, want an estimate", tt.neighbours, tt.radius)
			}
			if math.Abs(got.Lat-tt.want.Lat) > 1e-9 || math.Abs(got.Lng-tt.want.Lng) > 1e-9 {
				t.Errorf("Estimate(%v, %v) = %v, want %v", tt.neighbours, tt.radius, got, tt.want)
			}
		})
	}
}

func TestEstimate_refusesIncoherentNeighbours(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		neighbours []Point
		radius     float64
	}{
		{
			name:       "no neighbours at all",
			neighbours: nil,
			radius:     5000,
		},
		{
			name:       "an empty, non-nil set",
			neighbours: []Point{},
			radius:     5000,
		},
		{
			// The case the whole feature is judged on: a day spanning two cities has
			// no honest answer, and the midpoint somewhere in South Moravia — where
			// nobody took a photo — is the worst possible one.
			name:       "a day spanning Prague and Vienna",
			neighbours: []Point{pragueCastle, vienna},
			radius:     5000,
		},
		{
			// A majority does not rescue an incoherent set: three photos in Prague
			// and one in Vienna still means the day moved, and "most of these agree"
			// is a weaker claim than the unmarked-looking pin would be making.
			name:       "one distant outlier among a tight cluster",
			neighbours: []Point{pragueCastle, pragueOldTown, pragueVysehrad, vienna},
			radius:     5000,
		},
		{
			// The same tight cluster that succeeds above, refused purely because the
			// configured radius is tighter than it is — the radius is the knob, and
			// it bites.
			name:       "a cluster wider than the configured radius",
			neighbours: []Point{pragueCastle, pragueOldTown, pragueVysehrad},
			radius:     100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := Estimate(tt.neighbours, tt.radius)
			if ok {
				t.Errorf("Estimate(%v, %v) = %v, want no estimate", tt.neighbours, tt.radius, got)
			}
			if got != (Point{}) {
				t.Errorf("Estimate(%v, %v) returned %v alongside ok=false, want the zero Point",
					tt.neighbours, tt.radius, got)
			}
		})
	}
}

// TestEstimate_radiusBoundary pins the inclusive edge of the coherence test: a
// neighbour exactly at the radius is coherent, one just past it is not.
func TestEstimate_radiusBoundary(t *testing.T) {
	t.Parallel()

	// Two points 1 km apart have a centroid 500 m from each.
	const oneKmInDegreesLat = 1000.0 / 111320.0
	a := Point{Lat: 50, Lng: 14}
	b := Point{Lat: 50 + oneKmInDegreesLat, Lng: 14}
	half := DistanceMeters(a, b) / 2

	if _, ok := Estimate([]Point{a, b}, half*1.001); !ok {
		t.Errorf("Estimate refused a set inside the radius, want an estimate")
	}
	if _, ok := Estimate([]Point{a, b}, half*0.999); ok {
		t.Errorf("Estimate accepted a set outside the radius, want no estimate")
	}
}

func TestDistanceMeters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		a, b      Point
		want      float64
		tolerance float64
	}{
		{
			name: "a point is zero from itself",
			a:    pragueCastle, b: pragueCastle,
			want: 0, tolerance: 1e-6,
		},
		{
			name: "Prague to Vienna is about 250 km",
			a:    pragueCastle, b: vienna,
			want: 250000, tolerance: 10000,
		},
		{
			name: "one degree of latitude is about 111 km",
			a:    Point{Lat: 0, Lng: 0}, b: Point{Lat: 1, Lng: 0},
			want: 111195, tolerance: 100,
		},
		{
			name: "the distance is symmetric",
			a:    vienna, b: pragueCastle,
			want: 250000, tolerance: 10000,
		},
		{
			name: "antipodes are half the circumference apart",
			a:    Point{Lat: 0, Lng: 0}, b: Point{Lat: 0, Lng: 180},
			want: math.Pi * earthRadiusMeters, tolerance: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := DistanceMeters(tt.a, tt.b)
			if math.Abs(got-tt.want) > tt.tolerance {
				t.Errorf("DistanceMeters(%v, %v) = %v, want %v (±%v)", tt.a, tt.b, got, tt.want, tt.tolerance)
			}
		})
	}
}
