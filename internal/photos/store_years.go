package photos

import (
	"context"
	"fmt"
	"strings"
)

// YearBucket is one calendar year of the photo catalog together with the number
// of photos captured in it. It backs the library's year facet, where each year is
// offered with its count so the reader sees how much a year holds before
// selecting it.
type YearBucket struct {
	Year  int `json:"year"`
	Count int `json:"count"`
}

// Years is the year histogram of the photo catalog returned by YearBuckets: the
// years that actually hold photos, newest first, plus the overall Total. Total
// counts every matching photo — including those with an unknown capture time
// (NULL taken_at), which belong to no year — so it may exceed the sum of the
// bucket counts.
type Years struct {
	Years []YearBucket `json:"years"`
	Total int          `json:"total"`
}

// yearsSQL groups the matching photos into calendar-year buckets over their
// capture time, newest year first. Photos with an unknown capture time (NULL
// taken_at) carry no year, so they are excluded here; they still contribute to
// the Years' Total, which Count computes over the same filters. The %s
// placeholder receives the shared List/Count WHERE filters (already AND-prefixed)
// so the histogram matches List/Count exactly.
const yearsSQL = `SELECT date_part('year', taken_at)::int AS year,
       count(*)::int AS cnt
FROM photos
WHERE taken_at IS NOT NULL%s
GROUP BY year
ORDER BY year DESC`

// YearBuckets returns the calendar years that hold photos matching params, newest
// first, each with its photo count. It reuses the shared buildWhere filters, so a
// bucket's count is exactly the number of photos List would return for the same
// filters plus that year; params' sort, order and pagination are ignored because
// the histogram is always grouped by year. The returned Total is Count over the
// same filters and includes photos with an unknown capture time, which belong to
// no bucket. The slice is empty (not nil) when nothing matches.
func (s *Store) YearBuckets(ctx context.Context, params ListParams) (Years, error) {
	query, args := buildYearsQuery(params)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return Years{}, fmt.Errorf("photos: querying years: %w", err)
	}
	defer rows.Close()

	buckets := make([]YearBucket, 0)
	for rows.Next() {
		var b YearBucket
		if scanErr := rows.Scan(&b.Year, &b.Count); scanErr != nil {
			return Years{}, fmt.Errorf("photos: scanning year bucket: %w", scanErr)
		}
		buckets = append(buckets, b)
	}
	if err := rows.Err(); err != nil {
		return Years{}, fmt.Errorf("photos: iterating year buckets: %w", err)
	}

	total, err := s.Count(ctx, params)
	if err != nil {
		return Years{}, err
	}
	return Years{Years: buckets, Total: total}, nil
}

// buildYearsQuery assembles the parameterised year-bucket aggregation for
// YearBuckets, reusing List's WHERE filters (so the histogram stays in step with
// List/Count) and adding the taken_at IS NOT NULL guard. Ordering and grouping
// are fixed by the query, never taken from params.
func buildYearsQuery(params ListParams) (string, []any) {
	where, args := buildWhere(params)
	var filter string
	if len(where) > 0 {
		filter = " AND " + strings.Join(where, " AND ")
	}
	return fmt.Sprintf(yearsSQL, filter), args
}
