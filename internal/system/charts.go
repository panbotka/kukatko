package system

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// errNoChartCounter is returned by LibraryCharts when the service was built
// without a chart aggregator, so a mis-wired instance answers with an error
// instead of panicking on a nil interface.
var errNoChartCounter = errors.New("system: no library chart aggregator configured")

// defaultChartsTTL is how long a chart aggregation is memoised before the next
// request recomputes it. It is ten times the library counts' TTL on purpose: the
// counts are what an import is watched with and must move within seconds, while
// a per-year histogram of a library spanning a century does not visibly change in
// five minutes — and its aggregates are the more expensive of the two.
const defaultChartsTTL = 5 * time.Minute

// monthWindow is how many months back the "added to the library" chart reaches,
// the current (partial) month included.
const monthWindow = 12

// topCameras is how many cameras the camera chart names. Beyond ten the bars are
// too short to compare and the tail is a long list of one-off phones.
const topCameras = 10

// Media buckets of StorageByMedia. The first three are the catalogue's own
// media_type values; RAW is not one of them — it is a file format, recognised by
// extension — so a RAW is reported as its own bucket and left out of `image`,
// which is what makes the three shares add up to the library.
const (
	mediaBucketImage = "image"
	mediaBucketLive  = "live"
	mediaBucketVideo = "video"
	mediaBucketRAW   = "raw"
)

// mediaBuckets is the fixed order StorageByMedia reports its buckets in, so the
// chart's bars never reshuffle between two reads and a bucket the library happens
// not to hold is a visible zero rather than a missing row.
var mediaBuckets = []string{mediaBucketImage, mediaBucketLive, mediaBucketVideo, mediaBucketRAW}

// YearPhotos is one bar of the capture-year histogram.
type YearPhotos struct {
	// Year is the calendar year (UTC) the photos were taken in.
	Year int `json:"year"`
	// Photos is how many were taken that year.
	Photos int `json:"photos"`
}

// MonthPhotos is one bar of the "added to the library" chart.
type MonthPhotos struct {
	// Month is the calendar month (UTC) in `YYYY-MM` form — a plain string rather
	// than a timestamp, because it is a bucket label, not an instant.
	Month string `json:"month"`
	// Photos is how many photos were added to the library that month.
	Photos int `json:"photos"`
}

// CameraPhotos is one bar of the top-cameras chart.
type CameraPhotos struct {
	// Camera is the camera's display name, the make and model folded into one
	// string ("Canon EOS 5D Mark III") the way a reader would say it.
	Camera string `json:"camera"`
	// Model is the bare camera_model value, which is what the library's `camera`
	// filter matches, so the chart can link a bar to the photos behind it.
	Model string `json:"model"`
	// Photos is how many photos that camera took.
	Photos int `json:"photos"`
}

// MediaStorage is one slice of the storage-by-media breakdown.
type MediaStorage struct {
	// Media is the bucket: image, live, video or raw.
	Media string `json:"media"`
	// Photos is how many catalogue rows fall in the bucket.
	Photos int `json:"photos"`
	// Bytes is the total size of their originals.
	Bytes int64 `json:"bytes"`
}

// YearStorage is one bar of the library-growth chart: what a year of importing
// added, and how big the library stood at the end of it.
type YearStorage struct {
	// Year is the calendar year (UTC) the photos were added to the library in —
	// when they were imported, not when they were taken.
	Year int `json:"year"`
	// Photos is how many were added that year.
	Photos int `json:"photos"`
	// Bytes is what they added to the library's size.
	Bytes int64 `json:"bytes"`
	// CumulativeBytes is the library's size at the end of that year, i.e. this
	// year's bytes plus every earlier year's. Derived, so the chart does not have
	// to accumulate and two readers cannot disagree about the running total.
	CumulativeBytes int64 `json:"cumulative_bytes"`
}

// Charts is the chart-shaped half of the library statistics, returned by
// GET /system/stats/charts: the series behind "what does our library look like?"
// — when its photos were taken, when they arrived, what took them and what they
// cost in bytes. The scalar counts live in Library and are fetched separately, so
// the cheap numbers render immediately and are not held up by these aggregates.
//
// Every series covers the **browsable** library — archived photos (the trash) are
// excluded throughout — so a bar a reader clicks leads to exactly the photos it
// counted. Every slice is non-nil, gap-filled and in ascending order, so a
// renderer can walk it as a time axis without reconstructing the missing buckets.
type Charts struct {
	// PhotosByYear is the capture-year histogram, one entry per year from the
	// oldest photo to the newest, years with nothing in them included. Photos with
	// no capture time — or with an implausible one, see photosByYearSQL — have no
	// place on a time axis and are not in it.
	PhotosByYear []YearPhotos `json:"photos_by_year"`
	// AddedByMonth is how many photos arrived in each of the last monthWindow
	// months, always exactly that many entries, oldest first.
	AddedByMonth []MonthPhotos `json:"added_by_month"`
	// TopCameras is the topCameras most-used cameras, most photos first.
	TopCameras []CameraPhotos `json:"top_cameras"`
	// StorageByMedia is the library's size split across the media buckets, always
	// all of them, in mediaBuckets order.
	StorageByMedia []MediaStorage `json:"storage_by_media"`
	// StorageByYear is the library's growth by year of addition, one entry per year
	// from the first import to the latest, with the running total.
	StorageByYear []YearStorage `json:"storage_by_year"`
}

// ChartCounter reads the raw chart aggregates from the catalogue. It is satisfied
// by *Store; an interface so the assembly is unit-testable with a fake and so the
// HTTP layer never talks to the database directly.
type ChartCounter interface {
	// AggregateCharts returns the raw series, with since bounding the
	// added-per-month window. Empty buckets are simply absent from the result and
	// the running totals are not computed: filling the gaps is the service's job,
	// so the store reports only what the database actually counted.
	AggregateCharts(ctx context.Context, since time.Time) (Charts, error)
}

// newChartsCache returns the memoised chart aggregates over counter, recomputed
// at most once per TTL. A non-positive ttl defaults to defaultChartsTTL and a nil
// now defaults to time.Now.
//
// The clock is the same one the cache ages its entries with, so the month window
// and the memoisation cannot drift apart: the twelve months a cached snapshot
// covers are the twelve months that were current when it was computed.
func newChartsCache(counter ChartCounter, ttl time.Duration, now func() time.Time) *snapshotCache[Charts] {
	if now == nil {
		now = time.Now
	}
	return newSnapshotCache(func(ctx context.Context) (Charts, error) {
		if counter == nil {
			return Charts{}, errNoChartCounter
		}
		at := now()
		raw, err := counter.AggregateCharts(ctx, monthWindowStart(at))
		if err != nil {
			return Charts{}, fmt.Errorf("aggregating library charts: %w", err)
		}
		return raw.fill(at), nil
	}, ttl, defaultChartsTTL, now)
}

// fill turns the raw aggregates into series a chart can render straight from:
// the two year axes get their empty years back so the time axis stays linear, the
// month window is padded to its full length whatever the library holds, every
// media bucket is present, and the growth chart gets its running total. An empty
// library yields empty (but non-nil) series and a full, all-zero month window.
func (c Charts) fill(now time.Time) Charts {
	return Charts{
		PhotosByYear: fillYears(c.PhotosByYear,
			func(y YearPhotos) int { return y.Year },
			func(year int) YearPhotos { return YearPhotos{Year: year} }),
		AddedByMonth:   fillMonths(c.AddedByMonth, now),
		TopCameras:     nonNilSlice(c.TopCameras),
		StorageByMedia: fillMediaBuckets(c.StorageByMedia),
		StorageByYear: accumulate(fillYears(c.StorageByYear,
			func(y YearStorage) int { return y.Year },
			func(year int) YearStorage { return YearStorage{Year: year} })),
	}
}

// fillYears returns rows sorted by year with every year between the first and the
// last present, inserting zero(year) for the ones the database had nothing for. A
// histogram whose gaps are simply missing draws a lie — 1950 next to 1975 reads
// as two adjacent years — so the gaps are made explicit here rather than left to
// each renderer. The rows are assumed to already be in ascending order (the
// queries order by year); an empty input yields an empty, non-nil slice.
func fillYears[T any](rows []T, yearOf func(T) int, zero func(int) T) []T {
	if len(rows) == 0 {
		return []T{}
	}
	first, last := yearOf(rows[0]), yearOf(rows[len(rows)-1])
	byYear := make(map[int]T, len(rows))
	for _, row := range rows {
		byYear[yearOf(row)] = row
	}
	out := make([]T, 0, last-first+1)
	for year := first; year <= last; year++ {
		row, ok := byYear[year]
		if !ok {
			row = zero(year)
		}
		out = append(out, row)
	}
	return out
}

// fillMonths returns exactly monthWindow months ending with the one now falls in,
// oldest first, taking each month's count from rows and zero for the months that
// have none. The window is fixed rather than derived from the data so a quiet
// month reads as a quiet month instead of vanishing from the axis.
func fillMonths(rows []MonthPhotos, now time.Time) []MonthPhotos {
	byMonth := make(map[string]int, len(rows))
	for _, row := range rows {
		byMonth[row.Month] = row.Photos
	}
	start := monthWindowStart(now)
	out := make([]MonthPhotos, 0, monthWindow)
	for offset := range monthWindow {
		month := monthKey(start.AddDate(0, offset, 0))
		out = append(out, MonthPhotos{Month: month, Photos: byMonth[month]})
	}
	return out
}

// monthWindowStart returns the first instant of the oldest month the added-per-
// month chart covers, in UTC. UTC throughout — here and in the SQL — so the
// buckets do not shift with the database server's time zone.
func monthWindowStart(now time.Time) time.Time {
	at := now.UTC()
	return time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -(monthWindow - 1), 0)
}

// monthKey formats an instant as the `YYYY-MM` bucket label used by MonthPhotos,
// matching what the SQL produces.
func monthKey(at time.Time) string {
	return at.UTC().Format("2006-01")
}

// fillMediaBuckets returns every media bucket in mediaBuckets order, taking the
// measured ones from rows and reporting the rest as zero. A library with no video
// in it should say "no video", not leave the reader wondering whether the bucket
// was measured at all.
func fillMediaBuckets(rows []MediaStorage) []MediaStorage {
	measured := make(map[string]MediaStorage, len(rows))
	for _, row := range rows {
		measured[row.Media] = row
	}
	out := make([]MediaStorage, 0, len(mediaBuckets))
	for _, bucket := range mediaBuckets {
		row, ok := measured[bucket]
		if !ok {
			row = MediaStorage{Media: bucket}
		}
		out = append(out, row)
	}
	return out
}

// accumulate fills in each year's running total, in place over the passed slice.
// The rows must be in ascending year order with no gaps, which is what fillYears
// guarantees.
func accumulate(years []YearStorage) []YearStorage {
	var total int64
	for i := range years {
		total += years[i].Bytes
		years[i].CumulativeBytes = total
	}
	return years
}

// nonNilSlice returns rows, or an empty slice when it is nil, so the JSON body
// carries `[]` rather than `null` and a client can iterate it unconditionally.
func nonNilSlice[T any](rows []T) []T {
	if rows == nil {
		return []T{}
	}
	return rows
}
