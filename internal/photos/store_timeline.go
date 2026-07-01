package photos

import (
	"context"
	"fmt"
	"strings"
)

// TimelineBucket is one month-granularity date bucket of the photo timeline: the
// number of photos captured in that calendar month (Count) and the number of
// photos that sort before this bucket in the default newest-first grid order
// (Cumulative). Because the buckets are ordered newest-first and month ranges do
// not overlap, Cumulative is the scroll index of the bucket's first photo in the
// grid, which lets a date scrubber jump to a month.
type TimelineBucket struct {
	Year       int `json:"year"`
	Month      int `json:"month"`
	Count      int `json:"count"`
	Cumulative int `json:"cumulative"`
}

// Timeline is the date histogram of the photo library returned by
// TimelineBuckets: the month buckets in newest-first order plus the overall
// Total. Total counts every matching photo — including those with an unknown
// capture time (NULL taken_at), which sort last in the grid and belong to no
// bucket — so it may exceed the sum of the bucket counts.
type Timeline struct {
	Buckets []TimelineBucket `json:"buckets"`
	Total   int              `json:"total"`
}

// timelineSQL groups the matching photos into month buckets over their capture
// time, newest month first, mirroring the default grid order (taken_at DESC).
// Photos with an unknown capture time (NULL taken_at) sort last in the grid and
// carry no year/month, so they are excluded here; they still contribute to the
// Timeline's Total, which Count computes over the same filters. The %s
// placeholder receives the shared List/Count WHERE filters (already AND-prefixed)
// so the histogram matches List/Count exactly.
const timelineSQL = `SELECT date_part('year', taken_at)::int AS year,
       date_part('month', taken_at)::int AS month,
       count(*)::int AS cnt
FROM photos
WHERE taken_at IS NOT NULL%s
GROUP BY year, month
ORDER BY year DESC, month DESC`

// TimelineBuckets returns the month-granularity date histogram of the photos
// matching params, ordered newest-first (by taken_at, the default grid order),
// with each bucket's Cumulative set to the number of photos that sort before it.
// It reuses the shared buildWhere filters, so the buckets match exactly what List
// would return in the same order for the same filters; params' sort, order and
// pagination are ignored because the histogram is always grouped by date. The
// returned Total is Count over the same filters and includes photos with an
// unknown capture time, which belong to no bucket.
func (s *Store) TimelineBuckets(ctx context.Context, params ListParams) (Timeline, error) {
	query, args := buildTimelineQuery(params)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return Timeline{}, fmt.Errorf("photos: querying timeline: %w", err)
	}
	defer rows.Close()

	buckets := make([]TimelineBucket, 0)
	for rows.Next() {
		var b TimelineBucket
		if scanErr := rows.Scan(&b.Year, &b.Month, &b.Count); scanErr != nil {
			return Timeline{}, fmt.Errorf("photos: scanning timeline bucket: %w", scanErr)
		}
		buckets = append(buckets, b)
	}
	if err := rows.Err(); err != nil {
		return Timeline{}, fmt.Errorf("photos: iterating timeline buckets: %w", err)
	}
	accumulate(buckets)

	total, err := s.Count(ctx, params)
	if err != nil {
		return Timeline{}, err
	}
	return Timeline{Buckets: buckets, Total: total}, nil
}

// buildTimelineQuery assembles the parameterised month-bucket aggregation for
// TimelineBuckets, reusing List's WHERE filters (so the histogram stays in step
// with List/Count) and adding the taken_at IS NOT NULL guard. Ordering and
// grouping are fixed by the query, never taken from params.
func buildTimelineQuery(params ListParams) (string, []any) {
	where, args := buildWhere(params)
	var filter string
	if len(where) > 0 {
		filter = " AND " + strings.Join(where, " AND ")
	}
	return fmt.Sprintf(timelineSQL, filter), args
}

// accumulate fills each bucket's Cumulative with the running total of the counts
// of the buckets before it. Because the buckets are in newest-first grid order and
// month ranges do not overlap, that running total is the scroll index of the
// bucket's first photo in the grid.
func accumulate(buckets []TimelineBucket) {
	running := 0
	for i := range buckets {
		buckets[i].Cumulative = running
		running += buckets[i].Count
	}
}
