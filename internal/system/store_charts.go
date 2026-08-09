package system

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/panbotka/kukatko/internal/imgconvert"
)

// Every chart query below shares three decisions, so they are stated once here
// rather than repeated five times.
//
// They all count the **browsable** library (archived_at IS NULL). The trash is a
// handful of rows a reader has already thrown away; leaving it in would make a
// bar disagree with the library view it links to.
//
// They all bucket time in **UTC** (`AT TIME ZONE 'UTC'`), which is how the rest
// of the app reads a capture date — the library's period filter sends UTC day
// bounds — so the buckets do not shift with the database server's time zone.
//
// None of them is a filtered subquery over an index-covered minority: they are
// whole-library aggregates, which is what a histogram is. What keeps them cheap
// is that each is a single grouped pass with no join and no sort beyond the
// grouping, that the only one with a range predicate keeps it sargable (see
// addedByMonthSQL), and that the assembled result is memoised for
// defaultChartsTTL.

// photosByYearSQL counts the live library by the calendar year its photos were
// taken in. Photos with no capture time have no place on a time axis and are left
// out; the counts card is where their absence shows up.
//
// The two bounds are a sanity guard on the axis, not a filter on the library: the
// service fills the empty years between the first and the last, so a single row
// whose capture date was misparsed as year 0001 (or as a century from now — a
// camera with a dead clock battery) would stretch the histogram to two thousand
// bars. 1826 is the year of the first photograph, so nothing real is lost.
const photosByYearSQL = `
SELECT extract(YEAR FROM taken_at AT TIME ZONE 'UTC')::int AS year,
       count(*)::int                                       AS photos
FROM photos
WHERE archived_at IS NULL
  AND taken_at >= '1826-01-01T00:00:00Z'::timestamptz
  AND taken_at < now() + interval '1 year'
GROUP BY year
ORDER BY year`

// addedByMonthSQL counts the live library by the month each photo was added,
// bounded to the window the caller asks for. The bound is on the bare created_at
// so it stays sargable on idx_photos_live_created_at; only the bucket label is
// computed per row.
const addedByMonthSQL = `
SELECT to_char(date_trunc('month', created_at AT TIME ZONE 'UTC'), 'YYYY-MM') AS month,
       count(*)::int                                                          AS photos
FROM photos
WHERE archived_at IS NULL AND created_at >= $1
GROUP BY month
ORDER BY month`

// topCamerasSQL ranks the cameras behind the live library. Make and model are
// grouped separately and folded into a display name in Go, because the model
// alone is what the library's camera filter matches. The tiebreakers make the
// ranking stable: two cameras with the same count must not swap places between
// two reads of the same library.
const topCamerasSQL = `
SELECT camera_make, camera_model, count(*)::int AS photos
FROM photos
WHERE archived_at IS NULL AND (camera_make <> '' OR camera_model <> '')
GROUP BY camera_make, camera_model
ORDER BY photos DESC, camera_make, camera_model
LIMIT $1`

// storageByMediaSQL splits the live library's bytes across the media buckets.
// RAW is not a media_type — it is a file format — so it is recognised by the
// original's extension and carved out of the images, which keeps the buckets
// disjoint and adding up to the whole library. The extension list is passed in
// (imgconvert.RAWExtensions) rather than spelled out here, so there is one
// definition of "this is a RAW" in the codebase.
const storageByMediaSQL = `
SELECT CASE
           WHEN media_type = 'video'                                     THEN 'video'
           WHEN lower(substring(file_name FROM '\.([^.]+)$')) = ANY($1)  THEN 'raw'
           WHEN media_type = 'live'                                      THEN 'live'
           ELSE 'image'
       END                              AS media,
       count(*)::int                    AS photos,
       coalesce(sum(file_size), 0)::bigint AS bytes
FROM photos
WHERE archived_at IS NULL
GROUP BY media`

// storageByYearSQL measures what each year of importing added to the library.
// It groups on created_at (when the photo arrived), not taken_at (when it was
// shot): this is the growth of a collection, not of a life.
const storageByYearSQL = `
SELECT extract(YEAR FROM created_at AT TIME ZONE 'UTC')::int AS year,
       count(*)::int                                         AS photos,
       coalesce(sum(file_size), 0)::bigint                    AS bytes
FROM photos
WHERE archived_at IS NULL
GROUP BY year
ORDER BY year`

// AggregateCharts returns the raw chart series in five grouped queries, one per
// chart, with since bounding the added-per-month window. Gaps are left unfilled
// and the running totals uncomputed — Charts.fill does that — so this method
// reports only what the database actually counted.
//
// The five queries are issued in sequence rather than batched: they are
// independent, none of them feeds the next, and the whole result is memoised for
// minutes, so five round trips every few minutes is not a cost worth trading
// clarity for.
func (s *Store) AggregateCharts(ctx context.Context, since time.Time) (Charts, error) {
	var charts Charts
	var err error

	charts.PhotosByYear, err = collect(ctx, s, "photos by year", photosByYearSQL,
		func(row *YearPhotos) []any { return []any{&row.Year, &row.Photos} })
	if err != nil {
		return Charts{}, err
	}
	charts.AddedByMonth, err = collect(ctx, s, "photos added by month", addedByMonthSQL,
		func(row *MonthPhotos) []any { return []any{&row.Month, &row.Photos} }, since)
	if err != nil {
		return Charts{}, err
	}
	charts.TopCameras, err = s.rankCameras(ctx)
	if err != nil {
		return Charts{}, err
	}
	charts.StorageByMedia, err = collect(ctx, s, "storage by media type", storageByMediaSQL,
		func(row *MediaStorage) []any { return []any{&row.Media, &row.Photos, &row.Bytes} },
		imgconvert.RAWExtensions())
	if err != nil {
		return Charts{}, err
	}
	charts.StorageByYear, err = collect(ctx, s, "storage by year", storageByYearSQL,
		func(row *YearStorage) []any { return []any{&row.Year, &row.Photos, &row.Bytes} })
	if err != nil {
		return Charts{}, err
	}
	return charts, nil
}

// rankCameras reads the most-used cameras, most photos first. It is the one
// series the database does not hand over in its final shape — make and model are
// grouped separately and folded into a display name here — so it collects into a
// private row type and maps it, rather than scanning straight into the result.
func (s *Store) rankCameras(ctx context.Context) ([]CameraPhotos, error) {
	type cameraRow struct {
		brand  string
		model  string
		photos int
	}
	rows, err := collect(ctx, s, "cameras", topCamerasSQL,
		func(row *cameraRow) []any { return []any{&row.brand, &row.model, &row.photos} }, topCameras)
	if err != nil {
		return nil, err
	}
	out := make([]CameraPhotos, 0, len(rows))
	for _, row := range rows {
		out = append(out, CameraPhotos{
			Camera: cameraName(row.brand, row.model),
			Model:  row.model,
			Photos: row.photos,
		})
	}
	return out, nil
}

// collect runs one chart query and scans every row into a fresh T, with dest
// naming the fields to scan into in column order. It exists so each series is one
// statement and one field list rather than a fifth copy of the same
// query/scan/iterate loop; what names the series in an error is `what`.
func collect[T any](
	ctx context.Context, s *Store, what, sql string, dest func(*T) []any, args ...any,
) ([]T, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("system: querying %s: %w", what, err)
	}
	defer rows.Close()

	out := []T{}
	for rows.Next() {
		var row T
		if err := rows.Scan(dest(&row)...); err != nil {
			return nil, fmt.Errorf("system: scanning %s row: %w", what, err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("system: iterating %s rows: %w", what, err)
	}
	return out, nil
}

// cameraName folds a camera's make and model into the one name a reader would
// say. Most cameras already repeat the make in the model ("Canon" + "Canon EOS
// 5D"), so the make is prefixed only when the model does not already start with
// it; either field may be empty, and both being empty is filtered out in SQL.
func cameraName(brand, model string) string {
	brand, model = strings.TrimSpace(brand), strings.TrimSpace(model)
	switch {
	case model == "":
		return brand
	case brand == "":
		return model
	case strings.HasPrefix(strings.ToLower(model), strings.ToLower(brand)):
		return model
	default:
		return brand + " " + model
	}
}
