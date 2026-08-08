package photos

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// CityCount is one city's photo count within a country in the place hierarchy,
// with the photo that stands for it.
type CityCount struct {
	City  string `json:"city"`
	Count int    `json:"count"`
	// CoverUID is the city's newest visible photo, the thumbnail the browse list
	// draws for it. Empty only in the theoretical case of a city with no photo
	// left to show, which the aggregation cannot produce (a row exists because a
	// photo put it there), so it is omitted rather than sent as "".
	CoverUID string `json:"cover_uid,omitempty"`
}

// CountryPlaces is one country in the place hierarchy: its total photo count, the
// photo that stands for it and the breakdown by city. Count includes photos whose
// city is unknown (so it may exceed the sum of the city counts), and Cities is
// never nil (empty when no city is known for the country).
type CountryPlaces struct {
	Country string `json:"country"`
	Count   int    `json:"count"`
	// CoverUID is the country's newest visible photo, taken from whichever of its
	// cities holds it — including the unknown-city group, which is as much a part
	// of the country as any named town.
	CoverUID string      `json:"cover_uid,omitempty"`
	Cities   []CityCount `json:"cities"`
}

// placeCell is one (country, city, count) row from the place aggregation query,
// carrying the group's cover photo and that photo's capture time so a country's
// cover can be chosen across its cities without a second query.
type placeCell struct {
	country string
	city    string
	count   int
	// cover is the group's newest photo; coverTakenAt is its capture time, nil
	// when no photo in the group has one.
	cover        string
	coverTakenAt *time.Time
}

// aggregatePlacesSQL groups the cached places of non-archived photos by country
// and city. Rows whose country is empty (a photo without geocoded place data, or
// a no-GPS "processed" marker) are excluded, so only photos with real place data
// contribute. The %s placeholder receives a country filter only when one is
// requested. A country's empty-city group still contributes to that country's
// total, which the caller folds into Count.
//
// This query builds its own WHERE clause instead of going through
// buildListQuery, so the library-visibility predicates have to be repeated by
// hand: hidden_from_library alongside the archive and stack gates. Places is a
// browse of the library, and a count that includes photos no grid will show is a
// count that lies.
//
// Each group also yields the photo that stands for it: the newest of the group's
// photos, with an unknown capture time sorted last and uid breaking ties, so the
// same place returns the same cover on every request. It is picked with the same
// array_agg subscript the album index uses — one more aggregate over the pass the
// count already makes — and never with a correlated "ORDER BY … LIMIT 1", which
// on this library walks the global capture order once per group (see
// docs/PERF.md § "The album index"). MAX(p.taken_at) is that cover's capture time
// by construction: the aggregate sorts NULLS LAST, so the first element is the
// row holding the maximum.
const aggregatePlacesSQL = `
SELECT pp.country, pp.city, count(*),
       (array_agg(p.uid ORDER BY p.taken_at DESC NULLS LAST, p.uid))[1] AS cover_uid,
       MAX(p.taken_at) AS cover_taken_at
FROM photo_places pp
JOIN photos p ON p.uid = pp.photo_uid
WHERE p.archived_at IS NULL
  AND (p.stack_uid IS NULL OR p.stack_primary)
  AND NOT p.hidden_from_library
  AND pp.country <> ''%s
GROUP BY pp.country, pp.city`

// AggregatePlaces returns the place hierarchy — each country with its photo
// count, its cover photo and per-city breakdown — aggregated over non-archived
// photos that have place data. Countries are sorted by count descending then
// name, and each country's cities likewise. When country is non-empty the result
// is scoped to that one country (drilling into its cities only); an unknown
// country yields an empty slice. Photos without place data are excluded entirely.
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
		if err := rows.Scan(&c.country, &c.city, &c.count, &c.cover, &c.coverTakenAt); err != nil {
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
// city group) and listing only its known cities. Each level keeps a cover photo:
// a city takes its own group's, a country the newest across all of its groups —
// the unknown-city one included, since a photo placed only by country is still a
// photo of that country. Countries are ordered by count descending then name, and
// each country's cities likewise. The input order does not matter; the result is
// fully sorted.
func assemblePlaces(rows []placeCell) []CountryPlaces {
	out := make([]CountryPlaces, 0)
	index := make(map[string]int)
	// The capture time behind each country's current cover, so a later group can
	// be compared against it without re-reading the rows.
	coverAt := make(map[string]*time.Time)
	for _, r := range rows {
		i, ok := index[r.country]
		if !ok {
			i = len(out)
			index[r.country] = i
			out = append(out, CountryPlaces{Country: r.country, Cities: []CityCount{}})
		}
		out[i].Count += r.count
		if newerCover(r.cover, r.coverTakenAt, out[i].CoverUID, coverAt[r.country]) {
			out[i].CoverUID = r.cover
			coverAt[r.country] = r.coverTakenAt
		}
		if r.city != "" {
			out[i].Cities = append(out[i].Cities,
				CityCount{City: r.city, Count: r.count, CoverUID: r.cover})
		}
	}
	for i := range out {
		sortCities(out[i].Cities)
	}
	sortCountries(out)
	return out
}

// newerCover reports whether the candidate cover should replace the incumbent:
// the newer capture time wins, an unknown one loses to any known one, and equal
// (or equally unknown) times fall back to the lower uid. That last tie-break is
// what makes the choice independent of the order the rows arrived in, so the same
// country returns the same cover on every request. An empty incumbent is always
// replaced; an empty candidate never wins.
func newerCover(candidate string, candidateAt *time.Time, incumbent string, incumbentAt *time.Time) bool {
	if candidate == "" {
		return false
	}
	if incumbent == "" {
		return true
	}
	switch {
	case candidateAt == nil && incumbentAt == nil:
		return candidate < incumbent
	case candidateAt == nil:
		return false
	case incumbentAt == nil:
		return true
	case candidateAt.Equal(*incumbentAt):
		return candidate < incumbent
	default:
		return candidateAt.After(*incumbentAt)
	}
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
