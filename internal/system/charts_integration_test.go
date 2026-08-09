//go:build integration

package system_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/system"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case,
// so they do not run in parallel.

// chartPhoto is one row of the chart fixture, spelling out everything the five
// aggregates read: when it was taken, when it arrived, how big it is, what took
// it and what kind of media it is.
type chartPhoto struct {
	uid       string
	fileName  string
	mediaType string
	takenAt   string
	createdAt time.Time
	size      int64
	brand     string
	model     string
	archived  bool
}

// seedChartPhoto inserts one fully specified photo. Unlike seedPhoto it sets the
// columns the charts group by, including created_at — which has a now() default,
// so a fixture that wants a photo to have arrived last year must say so.
func seedChartPhoto(t *testing.T, db *database.DB, p chartPhoto) {
	t.Helper()
	const stmt = `
INSERT INTO photos (uid, file_hash, file_path, file_name, media_type, taken_at, created_at,
                    file_size, camera_make, camera_model, archived_at)
VALUES ($1, $2, $3, $4, $5, $6::timestamptz, $7, $8, $9, $10,
        CASE WHEN $11 THEN now() ELSE NULL END)`
	var takenAt any
	if p.takenAt != "" {
		takenAt = p.takenAt
	}
	_, err := db.Pool().Exec(t.Context(), stmt,
		p.uid, "hash-"+p.uid, "2026/07/"+p.fileName, p.fileName, p.mediaType, takenAt,
		p.createdAt, p.size, p.brand, p.model, p.archived)
	if err != nil {
		t.Fatalf("seed chart photo %s: %v", p.uid, err)
	}
}

// monthsAgo returns an instant in the month n months before now, at midday UTC on
// the 15th so no case can straddle a month boundary or a time zone.
func monthsAgo(n int) time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), 15, 12, 0, 0, 0, time.UTC).AddDate(0, -n, 0)
}

// monthKeyOf formats an instant the way the added-per-month series labels it.
func monthKeyOf(at time.Time) string {
	return at.UTC().Format("2006-01")
}

// seedChartLibrary fills the empty database with a fixture whose every series is
// known by construction:
//
//	c1  1905 image  2 MiB   Canon EOS 5D      added this month
//	c2  1905 image  1 MiB   Canon EOS 5D      added this month
//	c3  1908 image  1 MiB   Apple iPhone 13   added 3 months ago
//	c4  2026 video  8 MiB   (no camera)       added 3 months ago
//	c5  2026 live   4 MiB   Apple iPhone 13   added 13 months ago (outside the window)
//	c6  2026 image  6 MiB   Canon EOS 5D      added 13 months ago, RAW (.cr2)
//	c7  ——   image  1 MiB   (no camera)       no capture time, so no year bar
//	c8  1905 image 99 MiB   Canon EOS 5D      ARCHIVED — the trash counts nowhere
func seedChartLibrary(t *testing.T, db *database.DB) {
	t.Helper()
	const mib = 1024 * 1024
	thisMonth, threeMonthsAgo, longAgo := monthsAgo(0), monthsAgo(3), monthsAgo(13)

	for _, p := range []chartPhoto{
		{uid: "c1", fileName: "c1.jpg", mediaType: "image", takenAt: "1905-06-01T10:00:00Z",
			createdAt: thisMonth, size: 2 * mib, brand: "Canon", model: "Canon EOS 5D"},
		{uid: "c2", fileName: "c2.jpg", mediaType: "image", takenAt: "1905-09-01T10:00:00Z",
			createdAt: thisMonth, size: 1 * mib, brand: "Canon", model: "Canon EOS 5D"},
		{uid: "c3", fileName: "c3.jpg", mediaType: "image", takenAt: "1908-01-01T10:00:00Z",
			createdAt: threeMonthsAgo, size: 1 * mib, brand: "Apple", model: "iPhone 13"},
		{uid: "c4", fileName: "c4.mp4", mediaType: "video", takenAt: "2026-02-01T10:00:00Z",
			createdAt: threeMonthsAgo, size: 8 * mib},
		{uid: "c5", fileName: "c5.heic", mediaType: "live", takenAt: "2026-03-01T10:00:00Z",
			createdAt: longAgo, size: 4 * mib, brand: "Apple", model: "iPhone 13"},
		{uid: "c6", fileName: "c6.CR2", mediaType: "image", takenAt: "2026-04-01T10:00:00Z",
			createdAt: longAgo, size: 6 * mib, brand: "Canon", model: "Canon EOS 5D"},
		{uid: "c7", fileName: "c7.jpg", mediaType: "image", createdAt: thisMonth, size: 1 * mib},
		{uid: "c8", fileName: "c8.jpg", mediaType: "image", takenAt: "1905-01-01T10:00:00Z",
			createdAt: thisMonth, size: 99 * mib, brand: "Canon", model: "Canon EOS 5D", archived: true},
	} {
		seedChartPhoto(t, db, p)
	}
}

// chartsOf aggregates the fixture through the service, which is how the endpoint
// reads it: raw series from the store, gaps filled, running totals derived.
func chartsOf(t *testing.T, db *database.DB) system.Charts {
	t.Helper()
	svc := system.New(system.Config{Charts: system.NewStore(db.Pool())})
	charts, err := svc.LibraryCharts(t.Context())
	if err != nil {
		t.Fatalf("LibraryCharts: %v", err)
	}
	return charts
}

// TestLibraryCharts_PhotosByYear asserts the capture-year histogram: only live
// photos, only those with a capture time, and every empty year in between made
// explicit so the axis stays linear.
func TestLibraryCharts_PhotosByYear(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	seedChartLibrary(t, db)

	got := chartsOf(t, db).PhotosByYear

	if len(got) != 2026-1905+1 {
		t.Fatalf("PhotosByYear has %d bars, want one per year from 1905 to 2026", len(got))
	}
	byYear := map[int]int{}
	for _, bar := range got {
		byYear[bar.Year] = bar.Photos
	}
	// 1905 holds two live photos; the third is archived and must not be counted.
	if byYear[1905] != 2 || byYear[1908] != 1 || byYear[2026] != 3 {
		t.Errorf("years = %+v, want 1905:2 / 1908:1 / 2026:3", byYear)
	}
	if byYear[1906] != 0 || byYear[1907] != 0 {
		t.Errorf("years = %+v, want the empty years present and zero", byYear)
	}
}

// TestLibraryCharts_PhotosByYear_IgnoresImplausibleDates verifies the axis guard:
// one misparsed capture date must not stretch the histogram across two millennia
// of empty bars, because the service fills every year in between.
func TestLibraryCharts_PhotosByYear_IgnoresImplausibleDates(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	seedChartLibrary(t, db)
	seedChartPhoto(t, db, chartPhoto{uid: "c9", fileName: "c9.jpg", mediaType: "image",
		takenAt: "0001-01-01T00:00:00Z", createdAt: monthsAgo(0), size: 1024})
	seedChartPhoto(t, db, chartPhoto{uid: "c10", fileName: "c10.jpg", mediaType: "image",
		takenAt: "2099-01-01T00:00:00Z", createdAt: monthsAgo(0), size: 1024})

	got := chartsOf(t, db).PhotosByYear

	if got[0].Year != 1905 || got[len(got)-1].Year != 2026 {
		t.Errorf("axis spans %d..%d, want the plausible 1905..2026",
			got[0].Year, got[len(got)-1].Year)
	}
}

// TestLibraryCharts_AddedByMonth asserts the arrivals window: exactly twelve
// months ending with the current one, with anything older left outside it.
func TestLibraryCharts_AddedByMonth(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	seedChartLibrary(t, db)

	got := chartsOf(t, db).AddedByMonth

	if len(got) != 12 {
		t.Fatalf("AddedByMonth has %d months, want 12", len(got))
	}
	if got[11].Month != monthKeyOf(monthsAgo(0)) {
		t.Errorf("last month = %s, want the current one %s", got[11].Month, monthKeyOf(monthsAgo(0)))
	}
	byMonth := map[string]int{}
	for _, bar := range got {
		byMonth[bar.Month] = bar.Photos
	}
	// c1, c2 and c7 arrived this month; the archived c8 did too and must not count.
	if byMonth[monthKeyOf(monthsAgo(0))] != 3 {
		t.Errorf("this month = %d, want 3 (the archived arrival excluded)", byMonth[monthKeyOf(monthsAgo(0))])
	}
	if byMonth[monthKeyOf(monthsAgo(3))] != 2 {
		t.Errorf("three months ago = %d, want 2", byMonth[monthKeyOf(monthsAgo(3))])
	}
	if _, inWindow := byMonth[monthKeyOf(monthsAgo(13))]; inWindow {
		t.Error("a 13-month-old arrival is inside the 12-month window")
	}
}

// TestLibraryCharts_TopCameras asserts the ranking, the make/model folding and
// that the model rides along so a bar can link to the photos behind it.
func TestLibraryCharts_TopCameras(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	seedChartLibrary(t, db)

	got := chartsOf(t, db).TopCameras

	// Only cameras: the two photos with no camera at all are not a bar named "".
	want := []system.CameraPhotos{
		{Camera: "Canon EOS 5D", Model: "Canon EOS 5D", Photos: 3},
		{Camera: "Apple iPhone 13", Model: "iPhone 13", Photos: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TopCameras = %+v, want %+v", got, want)
	}
}

// TestLibraryCharts_Storage asserts both storage series: the media split — with
// RAW recognised by extension and carved out of the images — and the growth by
// year of addition with its running total.
func TestLibraryCharts_Storage(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	seedChartLibrary(t, db)

	const mib = 1024 * 1024
	charts := chartsOf(t, db)

	// c1 + c2 + c3 + c7 are plain images (5 MiB), c5 is live (4 MiB), c4 is video
	// (8 MiB) and c6 is a .CR2, so RAW (6 MiB) — case-insensitively, and it is not
	// counted among the images. The archived 99 MiB is in none of them.
	want := []system.MediaStorage{
		{Media: "image", Photos: 4, Bytes: 5 * mib},
		{Media: "live", Photos: 1, Bytes: 4 * mib},
		{Media: "video", Photos: 1, Bytes: 8 * mib},
		{Media: "raw", Photos: 1, Bytes: 6 * mib},
	}
	if !reflect.DeepEqual(charts.StorageByMedia, want) {
		t.Errorf("StorageByMedia = %+v, want %+v", charts.StorageByMedia, want)
	}

	var total int64
	for _, year := range charts.StorageByYear {
		total += year.Bytes
		if year.CumulativeBytes != total {
			t.Errorf("year %d cumulative = %d, want the running total %d",
				year.Year, year.CumulativeBytes, total)
		}
	}
	if total != 23*mib {
		t.Errorf("total bytes by year = %d, want %d (the trash excluded)", total, 23*mib)
	}
}

// TestLibraryCharts_EmptyLibrary verifies a freshly truncated database still
// renders: empty (never nil) series, a full twelve-month window of zeroes and
// every media bucket present. An instance before its first import must show an
// empty dashboard, not an error.
func TestLibraryCharts_EmptyLibrary(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	got := chartsOf(t, db)

	if len(got.PhotosByYear) != 0 || got.PhotosByYear == nil {
		t.Errorf("PhotosByYear = %+v, want an empty non-nil slice", got.PhotosByYear)
	}
	if len(got.TopCameras) != 0 || got.TopCameras == nil {
		t.Errorf("TopCameras = %+v, want an empty non-nil slice", got.TopCameras)
	}
	if len(got.StorageByYear) != 0 || got.StorageByYear == nil {
		t.Errorf("StorageByYear = %+v, want an empty non-nil slice", got.StorageByYear)
	}
	if len(got.AddedByMonth) != 12 {
		t.Fatalf("AddedByMonth has %d months, want a full window of 12", len(got.AddedByMonth))
	}
	for _, month := range got.AddedByMonth {
		if month.Photos != 0 {
			t.Errorf("month %s = %d, want 0", month.Month, month.Photos)
		}
	}
	for _, bucket := range got.StorageByMedia {
		if bucket.Photos != 0 || bucket.Bytes != 0 {
			t.Errorf("bucket %s = %+v, want zeroes", bucket.Media, bucket)
		}
	}
}

// TestAggregateCharts_RawSeriesAreUnfilled verifies the store reports only what
// the database counted: no gap years, no running totals, no padded month window.
// Filling is the service's job, so a caller reading the store directly cannot
// mistake a derived value for a measured one.
func TestAggregateCharts_RawSeriesAreUnfilled(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	seedChartLibrary(t, db)

	since := time.Now().UTC().AddDate(-1, 0, 0)
	got, err := system.NewStore(db.Pool()).AggregateCharts(t.Context(), since)
	if err != nil {
		t.Fatalf("AggregateCharts: %v", err)
	}

	// Three years hold photos; the gap years between them are the service's doing.
	if len(got.PhotosByYear) != 3 {
		t.Errorf("PhotosByYear = %+v, want only the three years that hold photos", got.PhotosByYear)
	}
	for _, year := range got.StorageByYear {
		if year.CumulativeBytes != 0 {
			t.Errorf("year %d = %+v, want the running total left at zero", year.Year, year)
		}
	}
}
