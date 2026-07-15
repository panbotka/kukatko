package photos

import (
	"context"
	"fmt"
	"sort"
)

// CityCount is one city's photo count within a country in the place hierarchy.
type CityCount struct {
	City  string `json:"city"`
	Count int    `json:"count"`
}

// CountryPlaces is one country in the place hierarchy: its total photo count and
// the breakdown by city. Count includes photos whose city is unknown (so it may
// exceed the sum of the city counts), and Cities is never nil (empty when no
// city is known for the country).
type CountryPlaces struct {
	Country string      `json:"country"`
	Count   int         `json:"count"`
	Cities  []CityCount `json:"cities"`
}

// placeCell is one (country, city, count) row from the place aggregation query.
type placeCell struct {
	country string
	city    string
	count   int
}

// aggregatePlacesSQL groups the cached places of non-archived photos by country
// and city. Rows whose country is empty (a photo without geocoded place data, or
// a no-GPS "processed" marker) are excluded, so only photos with real place data
// contribute. The %s placeholder receives a country filter only when one is
// requested. A country's empty-city group still contributes to that country's
// total, which the caller folds into Count.
const aggregatePlacesSQL = `
SELECT pp.country, pp.city, count(*)
FROM photo_places pp
JOIN photos p ON p.uid = pp.photo_uid
WHERE p.archived_at IS NULL
  AND (p.stack_uid IS NULL OR p.stack_primary)
  AND pp.country <> ''%s
GROUP BY pp.country, pp.city`

// AggregatePlaces returns the place hierarchy — each country with its photo count
// and per-city breakdown — aggregated over non-archived photos that have place
// data. Countries are sorted by count descending then name, and each country's
// cities likewise. When country is non-empty the result is scoped to that one
// country (drilling into its cities only); an unknown country yields an empty
// slice. Photos without place data are excluded entirely.
func (s *Store) AggregatePlaces(ctx context.Context, country string) ([]CountryPlaces, error) {
	query := fmt.Sprintf(aggregatePlacesSQL, "")
	var args []any
	if country != "" {
		query = fmt.Sprintf(aggregatePlacesSQL, "\n  AND pp.country = $1")
		args = []any{country}
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("photos: aggregating places: %w", err)
	}
	defer rows.Close()

	var cells []placeCell
	for rows.Next() {
		var c placeCell
		if err := rows.Scan(&c.country, &c.city, &c.count); err != nil {
			return nil, fmt.Errorf("photos: scanning place aggregate: %w", err)
		}
		cells = append(cells, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photos: iterating place aggregates: %w", err)
	}
	return assemblePlaces(cells), nil
}

// assemblePlaces folds the flat (country, city, count) rows into the nested
// country/city hierarchy, summing each country's total (including its unknown-
// city group) and listing only its known cities. Countries are ordered by count
// descending then name, and each country's cities likewise. The input order does
// not matter; the result is fully sorted.
func assemblePlaces(rows []placeCell) []CountryPlaces {
	out := make([]CountryPlaces, 0)
	index := make(map[string]int)
	for _, r := range rows {
		i, ok := index[r.country]
		if !ok {
			i = len(out)
			index[r.country] = i
			out = append(out, CountryPlaces{Country: r.country, Cities: []CityCount{}})
		}
		out[i].Count += r.count
		if r.city != "" {
			out[i].Cities = append(out[i].Cities, CityCount{City: r.city, Count: r.count})
		}
	}
	for i := range out {
		sortCities(out[i].Cities)
	}
	sortCountries(out)
	return out
}

// sortCountries orders countries by photo count descending, breaking ties by
// country name ascending so the ordering is stable and deterministic.
func sortCountries(countries []CountryPlaces) {
	sort.Slice(countries, func(i, j int) bool {
		if countries[i].Count != countries[j].Count {
			return countries[i].Count > countries[j].Count
		}
		return countries[i].Country < countries[j].Country
	})
}

// sortCities orders cities by photo count descending, breaking ties by city name
// ascending so the ordering is stable and deterministic.
func sortCities(cities []CityCount) {
	sort.Slice(cities, func(i, j int) bool {
		if cities[i].Count != cities[j].Count {
			return cities[i].Count > cities[j].Count
		}
		return cities[i].City < cities[j].City
	})
}
