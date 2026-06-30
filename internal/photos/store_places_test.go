package photos

import (
	"testing"
)

// TestAssemblePlaces verifies the flat (country, city, count) rows fold into the
// nested hierarchy with correct per-country totals, that the unknown-city group
// contributes to the country total without producing a city entry, and that
// countries and cities are sorted by count descending then name.
func TestAssemblePlaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []placeCell
		want []CountryPlaces
	}{
		{
			name: "empty input yields empty slice",
			in:   nil,
			want: []CountryPlaces{},
		},
		{
			name: "country total folds the unknown-city group without a city entry",
			in: []placeCell{
				{country: "Czechia", city: "Praha", count: 2},
				{country: "Czechia", city: "", count: 3},
			},
			want: []CountryPlaces{
				{Country: "Czechia", Count: 5, Cities: []CityCount{{City: "Praha", Count: 2}}},
			},
		},
		{
			name: "countries sorted by count desc then name; cities likewise",
			in: []placeCell{
				{country: "Czechia", city: "Brno", count: 1},
				{country: "Czechia", city: "Praha", count: 4},
				{country: "Austria", city: "Wien", count: 4},
				{country: "Brazil", city: "Rio", count: 4},
			},
			want: []CountryPlaces{
				// Czechia (5) first by count; Austria and Brazil tie at 4, name breaks tie.
				{Country: "Czechia", Count: 5, Cities: []CityCount{
					{City: "Praha", Count: 4}, {City: "Brno", Count: 1},
				}},
				{Country: "Austria", Count: 4, Cities: []CityCount{{City: "Wien", Count: 4}}},
				{Country: "Brazil", Count: 4, Cities: []CityCount{{City: "Rio", Count: 4}}},
			},
		},
		{
			name: "city tie broken by name",
			in: []placeCell{
				{country: "Czechia", city: "Brno", count: 2},
				{country: "Czechia", city: "Adamov", count: 2},
			},
			want: []CountryPlaces{
				{Country: "Czechia", Count: 4, Cities: []CityCount{
					{City: "Adamov", Count: 2}, {City: "Brno", Count: 2},
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := assemblePlaces(tt.in)
			if !equalPlaces(got, tt.want) {
				t.Errorf("assemblePlaces(%v) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

// equalPlaces reports whether two place hierarchies are identical in country and
// city order, counts and names.
func equalPlaces(a, b []CountryPlaces) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Country != b[i].Country || a[i].Count != b[i].Count {
			return false
		}
		if len(a[i].Cities) != len(b[i].Cities) {
			return false
		}
		for j := range a[i].Cities {
			if a[i].Cities[j] != b[i].Cities[j] {
				return false
			}
		}
	}
	return true
}
