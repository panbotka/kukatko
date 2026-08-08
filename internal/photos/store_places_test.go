package photos

import (
	"testing"
	"time"
)

// placeTime is a helper for a fixed capture time in the cover-selection cases.
func placeTime(day int) *time.Time {
	t := time.Date(2026, time.May, day, 12, 0, 0, 0, time.UTC)
	return &t
}

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
		{
			name: "each city keeps its own cover, the country takes the newest",
			in: []placeCell{
				{country: "Czechia", city: "Brno", count: 2, cover: "old", coverTakenAt: placeTime(1)},
				{country: "Czechia", city: "Praha", count: 5, cover: "new", coverTakenAt: placeTime(9)},
			},
			want: []CountryPlaces{
				{Country: "Czechia", Count: 7, CoverUID: "new", Cities: []CityCount{
					{City: "Praha", Count: 5, CoverUID: "new"},
					{City: "Brno", Count: 2, CoverUID: "old"},
				}},
			},
		},
		{
			name: "a photo placed only by country can be the country cover",
			in: []placeCell{
				{country: "Czechia", city: "Brno", count: 9, cover: "city", coverTakenAt: placeTime(1)},
				{country: "Czechia", city: "", count: 1, cover: "loose", coverTakenAt: placeTime(4)},
			},
			want: []CountryPlaces{
				{Country: "Czechia", Count: 10, CoverUID: "loose", Cities: []CityCount{
					{City: "Brno", Count: 9, CoverUID: "city"},
				}},
			},
		},
		{
			name: "an unknown capture time loses to a known one, then uid breaks the tie",
			in: []placeCell{
				{country: "Czechia", city: "Zlin", count: 1, cover: "undated"},
				{country: "Czechia", city: "Brno", count: 1, cover: "dated", coverTakenAt: placeTime(2)},
				{country: "Austria", city: "Wien", count: 1, cover: "bbb"},
				{country: "Austria", city: "Linz", count: 1, cover: "aaa"},
			},
			want: []CountryPlaces{
				{Country: "Austria", Count: 2, CoverUID: "aaa", Cities: []CityCount{
					{City: "Linz", Count: 1, CoverUID: "aaa"},
					{City: "Wien", Count: 1, CoverUID: "bbb"},
				}},
				{Country: "Czechia", Count: 2, CoverUID: "dated", Cities: []CityCount{
					{City: "Brno", Count: 1, CoverUID: "dated"},
					{City: "Zlin", Count: 1, CoverUID: "undated"},
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

// TestAssemblePlacesCoverIsOrderIndependent verifies the country cover does not
// depend on the order the aggregation rows arrive in: Postgres makes no promise
// about group order, so a cover chosen from the first row seen would drift
// between requests.
func TestAssemblePlacesCoverIsOrderIndependent(t *testing.T) {
	t.Parallel()

	forward := []placeCell{
		{country: "Czechia", city: "Brno", count: 1, cover: "a", coverTakenAt: placeTime(3)},
		{country: "Czechia", city: "Praha", count: 1, cover: "b", coverTakenAt: placeTime(3)},
		{country: "Czechia", city: "Zlin", count: 1, cover: "c"},
	}
	reversed := []placeCell{forward[2], forward[1], forward[0]}

	got := assemblePlaces(forward)
	back := assemblePlaces(reversed)
	if len(got) != 1 || len(back) != 1 {
		t.Fatalf("assemblePlaces = %+v / %+v, want one country each", got, back)
	}
	if got[0].CoverUID != back[0].CoverUID {
		t.Errorf("cover depends on row order: %q vs %q", got[0].CoverUID, back[0].CoverUID)
	}
	if got[0].CoverUID != "a" {
		t.Errorf("cover = %q, want the lower uid of the two newest", got[0].CoverUID)
	}
}

// equalPlaces reports whether two place hierarchies are identical in country and
// city order, counts, names and covers.
func equalPlaces(a, b []CountryPlaces) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Country != b[i].Country || a[i].Count != b[i].Count {
			return false
		}
		if a[i].CoverUID != b[i].CoverUID {
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
