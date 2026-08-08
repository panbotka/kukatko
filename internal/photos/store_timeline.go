package photos

import (
	"context"
	"fmt"
	"strings"
)

// TimelineBucket is one month-granularity date bucket of the photo timeline: the
// number of photos captured in that calendar month (Count) and the number of
// photos that sort before this bucket in the grid order the params ask for
// (Cumulative). Because the buckets follow that same order and month ranges do
// not overlap, Cumulative is the scroll index of the bucket's first photo in the
// grid, which lets a date scrubber jump to a month.
type TimelineBucket struct {
	Year       int `json:"year"`
	Month      int `json:"month"`
	Count      int `json:"count"`
	Cumulative int `json:"cumulative"`
}

// Timeline is the date histogram of the photo library returned by
// TimelineBuckets: the month buckets in grid order (newest-first by default,
// oldest-first for an ascending sort) plus the overall Total. Total counts every
// matching photo — under the default sort including those with an unknown
// capture time (NULL taken_at), which sort last in the grid and belong to no
// bucket — so it may exceed the sum of the bucket counts. Under the chronology
// sort every photo belongs to a bucket and the two agree.
type Timeline struct {
	Buckets []TimelineBucket `json:"buckets"`
	Total   int              `json:"total"`
}

// timelineSQL groups the matching photos into month buckets over the date the
// grid orders by, mirroring that order. Placeholder %[1]s receives the date
// expression (timelineDateExpr), %[2]s the shared List/Count WHERE filters
// (already AND-prefixed) so the histogram matches List/Count exactly, and %[3]s
// the direction (timelineDirection). Photos the date expression leaves NULL sort
// last in the grid and carry no year/month, so they are excluded here; they still
// contribute to the Timeline's Total, which Count computes over the same filters.
const timelineSQL = `SELECT date_part('year', %[1]s)::int AS year,
       date_part('month', %[1]s)::int AS month,
       count(*)::int AS cnt
FROM photos
WHERE %[1]s IS NOT NULL%[2]s
GROUP BY year, month
ORDER BY year %[3]s, month %[3]s`

// TimelineBuckets returns the month-granularity date histogram of the photos
// matching params, in the same order List would return them, with each bucket's
// Cumulative set to the number of photos that sort before it. It reuses the
// shared buildWhere filters, so the buckets match exactly what List would return
// for the same filters; params' pagination is ignored, and of the sort only what
// decides the date axis is read — the direction (Order) and whether the upload
// time stands in for a missing capture time (SortByChronology). Every other sort
// key is grouped by capture time, newest first, as before: a scrubber over a
// histogram is a date scrubber whatever the grid beside it is sorted by. The
// returned Total is Count over the same filters and includes photos with an
// unknown capture time, which belong to no bucket unless the chronology sort
// gives them one.
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
// with List/Count) and adding the guard that keeps rows without a date out of the
// grouping. The grouping is always by month; the date it groups on and the
// direction it walks come from params, so the buckets stay in step with the grid.
func buildTimelineQuery(params ListParams) (string, []any) {
	where, args := buildWhere(params)
	var filter string
	if len(where) > 0 {
		filter = " AND " + strings.Join(where, " AND ")
	}
	return fmt.Sprintf(timelineSQL, timelineDateExpr(params), filter, timelineDirection(params)), args
}

// timelineDateExpr returns the date the histogram groups by, matching the column
// orderClause sorts the grid on: the capture time, or — under SortByChronology,
// where the upload time stands in for a photo that never had one — the same
// COALESCE. That is what keeps Cumulative an exact grid index for an album, whose
// undated photos are interleaved by upload time rather than pushed to the end.
func timelineDateExpr(params ListParams) string {
	if params.Sort == SortByChronology {
		return "COALESCE(taken_at, created_at)"
	}
	return "taken_at"
}

// timelineDirection returns the bucket order matching the grid's, so the first
// bucket is the first month the reader meets: newest-first by default, oldest-
// first for an ascending sort (an album's default presentation).
func timelineDirection(params ListParams) string {
	if params.Order == OrderAsc {
		return "ASC"
	}
	return "DESC"
}

// accumulate fills each bucket's Cumulative with the running total of the counts
// of the buckets before it. Because the buckets are in grid order and month
// ranges do not overlap, that running total is the scroll index of the bucket's
// first photo in the grid.
func accumulate(buckets []TimelineBucket) {
	running := 0
	for i := range buckets {
		buckets[i].Cumulative = running
		running += buckets[i].Count
	}
}
