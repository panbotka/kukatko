package system

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

// fakeChartCounter is a ChartCounter returning fixed raw series (or an error) and
// recording the window it was asked for, so both the memoisation and the month
// bound can be observed.
type fakeChartCounter struct {
	charts Charts
	err    error
	calls  int
	since  time.Time
}

// AggregateCharts returns the configured series or error, recording the call.
func (f *fakeChartCounter) AggregateCharts(_ context.Context, since time.Time) (Charts, error) {
	f.calls++
	f.since = since
	return f.charts, f.err
}

// at is a fixed clock reading, mid-month so nothing depends on a month boundary.
func at(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 12, 0, 0, 0, time.UTC)
}

// TestChartsFill_YearGaps verifies the capture-year histogram gets its empty
// years back: a gap that is simply missing would draw 1905 next to 1908 as if
// they were adjacent.
func TestChartsFill_YearGaps(t *testing.T) {
	t.Parallel()

	raw := Charts{PhotosByYear: []YearPhotos{{Year: 1905, Photos: 3}, {Year: 1908, Photos: 1}}}
	got := raw.fill(at(2026, time.August, 10)).PhotosByYear

	want := []YearPhotos{
		{Year: 1905, Photos: 3},
		{Year: 1906, Photos: 0},
		{Year: 1907, Photos: 0},
		{Year: 1908, Photos: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PhotosByYear = %+v, want %+v", got, want)
	}
}

// TestChartsFill_MonthWindow verifies the arrivals chart always covers exactly
// twelve months ending with the current one, whatever the library holds.
func TestChartsFill_MonthWindow(t *testing.T) {
	t.Parallel()

	raw := Charts{AddedByMonth: []MonthPhotos{{Month: "2026-03", Photos: 12}, {Month: "2026-08", Photos: 4}}}
	got := raw.fill(at(2026, time.August, 10)).AddedByMonth

	if len(got) != monthWindow {
		t.Fatalf("AddedByMonth has %d months, want %d", len(got), monthWindow)
	}
	if got[0].Month != "2025-09" || got[monthWindow-1].Month != "2026-08" {
		t.Errorf("window = %s..%s, want 2025-09..2026-08", got[0].Month, got[monthWindow-1].Month)
	}
	byMonth := map[string]int{}
	for _, month := range got {
		byMonth[month.Month] = month.Photos
	}
	if byMonth["2026-03"] != 12 || byMonth["2026-08"] != 4 || byMonth["2026-05"] != 0 {
		t.Errorf("counts = %+v, want March 12 / August 4 / May 0", got)
	}
}

// TestChartsFill_MonthWindowCrossesTheYear verifies the window is built by
// calendar month arithmetic rather than by subtracting days, so a December read
// reaches back into the previous year correctly.
func TestChartsFill_MonthWindowCrossesTheYear(t *testing.T) {
	t.Parallel()

	got := Charts{}.fill(at(2026, time.January, 31)).AddedByMonth
	if got[0].Month != "2025-02" || got[monthWindow-1].Month != "2026-01" {
		t.Errorf("window = %s..%s, want 2025-02..2026-01", got[0].Month, got[monthWindow-1].Month)
	}
}

// TestChartsFill_StorageAccumulates verifies the growth chart carries a running
// total, so the chart never has to accumulate and two readers cannot disagree.
func TestChartsFill_StorageAccumulates(t *testing.T) {
	t.Parallel()

	raw := Charts{StorageByYear: []YearStorage{
		{Year: 2024, Photos: 2, Bytes: 100},
		{Year: 2026, Photos: 3, Bytes: 400},
	}}
	got := raw.fill(at(2026, time.August, 10)).StorageByYear

	want := []YearStorage{
		{Year: 2024, Photos: 2, Bytes: 100, CumulativeBytes: 100},
		{Year: 2025, Photos: 0, Bytes: 0, CumulativeBytes: 100},
		{Year: 2026, Photos: 3, Bytes: 400, CumulativeBytes: 500},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("StorageByYear = %+v, want %+v", got, want)
	}
}

// TestChartsFill_MediaBuckets verifies every bucket is reported in a fixed order,
// so a library with no video says "no video" instead of leaving the reader to
// wonder whether it was measured.
func TestChartsFill_MediaBuckets(t *testing.T) {
	t.Parallel()

	raw := Charts{StorageByMedia: []MediaStorage{{Media: mediaBucketRAW, Photos: 12, Bytes: 900}}}
	got := raw.fill(at(2026, time.August, 10)).StorageByMedia

	want := []MediaStorage{
		{Media: mediaBucketImage},
		{Media: mediaBucketLive},
		{Media: mediaBucketVideo},
		{Media: mediaBucketRAW, Photos: 12, Bytes: 900},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("StorageByMedia = %+v, want %+v", got, want)
	}
}

// TestChartsFill_EmptyLibrary verifies an empty library yields empty — but never
// nil — series and a full, all-zero month window: the page must draw an empty
// library rather than fail on a missing series.
func TestChartsFill_EmptyLibrary(t *testing.T) {
	t.Parallel()

	got := Charts{}.fill(at(2026, time.August, 10))

	if got.PhotosByYear == nil || len(got.PhotosByYear) != 0 {
		t.Errorf("PhotosByYear = %+v, want an empty non-nil slice", got.PhotosByYear)
	}
	if got.TopCameras == nil || len(got.TopCameras) != 0 {
		t.Errorf("TopCameras = %+v, want an empty non-nil slice", got.TopCameras)
	}
	if got.StorageByYear == nil || len(got.StorageByYear) != 0 {
		t.Errorf("StorageByYear = %+v, want an empty non-nil slice", got.StorageByYear)
	}
	if len(got.AddedByMonth) != monthWindow {
		t.Errorf("AddedByMonth has %d months, want a full window of %d", len(got.AddedByMonth), monthWindow)
	}
	if len(got.StorageByMedia) != len(mediaBuckets) {
		t.Errorf("StorageByMedia has %d buckets, want all %d", len(got.StorageByMedia), len(mediaBuckets))
	}
}

// TestCameraName checks the make/model folding: the make is prefixed only when
// the model does not already repeat it, and either field may be missing.
func TestCameraName(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		brand string
		model string
		want  string
	}{
		{"Canon", "Canon EOS 5D Mark III", "Canon EOS 5D Mark III"},
		{"NIKON CORPORATION", "NIKON D750", "NIKON CORPORATION NIKON D750"},
		{"Apple", "iPhone 13", "Apple iPhone 13"},
		{"apple", "Apple iPhone 13", "Apple iPhone 13"},
		{"", "Pixel 7", "Pixel 7"},
		{"Fujifilm", "", "Fujifilm"},
		{" Sony ", " ILCE-7M3 ", "Sony ILCE-7M3"},
	} {
		if got := cameraName(tt.brand, tt.model); got != tt.want {
			t.Errorf("cameraName(%q, %q) = %q, want %q", tt.brand, tt.model, got, tt.want)
		}
	}
}

// TestChartsCache_MemoisesWithinTTL verifies a second read inside the TTL is
// served from the cache and does not re-run the five aggregates.
func TestChartsCache_MemoisesWithinTTL(t *testing.T) {
	t.Parallel()

	now := at(2026, time.August, 10)
	counter := &fakeChartCounter{charts: Charts{PhotosByYear: []YearPhotos{{Year: 2026, Photos: 5}}}}
	cache := newChartsCache(counter, time.Hour, func() time.Time { return now })

	first, err := cache.get(t.Context())
	if err != nil {
		t.Fatalf("first charts: %v", err)
	}
	now = now.Add(30 * time.Minute)
	second, err := cache.get(t.Context())
	if err != nil {
		t.Fatalf("second charts: %v", err)
	}
	if !reflect.DeepEqual(second, first) {
		t.Errorf("second charts = %+v, want the memoised %+v", second, first)
	}
	if counter.calls != 1 {
		t.Errorf("aggregator called %d times, want 1 (memoised)", counter.calls)
	}
}

// TestChartsCache_AsksForTheMonthWindow verifies the store is bounded to the
// months the chart actually draws, rather than being asked for the whole history
// and filtered afterwards.
func TestChartsCache_AsksForTheMonthWindow(t *testing.T) {
	t.Parallel()

	counter := &fakeChartCounter{}
	cache := newChartsCache(counter, time.Hour, func() time.Time { return at(2026, time.August, 10) })
	if _, err := cache.get(t.Context()); err != nil {
		t.Fatalf("charts: %v", err)
	}

	want := time.Date(2025, time.September, 1, 0, 0, 0, 0, time.UTC)
	if !counter.since.Equal(want) {
		t.Errorf("since = %s, want %s", counter.since, want)
	}
}

// TestChartsCache_ErrorIsReturnedNotCached verifies a failed aggregation surfaces
// as an error — never as empty series, which would draw as an empty library — and
// that the failure is not memoised, so the next read retries.
func TestChartsCache_ErrorIsReturnedNotCached(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("db down")
	counter := &fakeChartCounter{err: wantErr}
	cache := newChartsCache(counter, time.Hour, nil)

	if _, err := cache.get(t.Context()); !errors.Is(err, wantErr) {
		t.Fatalf("charts error = %v, want %v", err, wantErr)
	}

	counter.err = nil
	counter.charts = Charts{TopCameras: []CameraPhotos{{Camera: "Canon", Photos: 1}}}
	recovered, err := cache.get(t.Context())
	if err != nil {
		t.Fatalf("charts after recovery: %v", err)
	}
	if len(recovered.TopCameras) != 1 || counter.calls != 2 {
		t.Errorf("recovered = %+v after %d calls, want one camera after 2 calls",
			recovered, counter.calls)
	}
}

// TestChartsCache_Defaults verifies the zero-value knobs fall back to the package
// defaults rather than a zero TTL or a nil clock.
func TestChartsCache_Defaults(t *testing.T) {
	t.Parallel()

	cache := newChartsCache(&fakeChartCounter{}, 0, nil)
	if cache.ttl != defaultChartsTTL {
		t.Errorf("ttl = %v, want the default %v", cache.ttl, defaultChartsTTL)
	}
	if cache.now == nil {
		t.Error("clock is nil, want the default time.Now")
	}
}

// TestServiceLibraryCharts_NoCounter verifies a service wired without a chart
// aggregator answers with an error instead of panicking on a nil interface.
func TestServiceLibraryCharts_NoCounter(t *testing.T) {
	t.Parallel()

	svc := New(Config{})
	if _, err := svc.LibraryCharts(t.Context()); !errors.Is(err, errNoChartCounter) {
		t.Errorf("LibraryCharts error = %v, want %v", err, errNoChartCounter)
	}
}

// TestServiceLibraryCharts_Fills verifies the service hands the caller filled
// series rather than the store's raw ones.
func TestServiceLibraryCharts_Fills(t *testing.T) {
	t.Parallel()

	svc := New(Config{
		Charts: &fakeChartCounter{charts: Charts{
			PhotosByYear:  []YearPhotos{{Year: 2024, Photos: 1}, {Year: 2026, Photos: 2}},
			StorageByYear: []YearStorage{{Year: 2026, Photos: 3, Bytes: 90}},
		}},
		Clock: func() time.Time { return at(2026, time.August, 10) },
	})
	got, err := svc.LibraryCharts(t.Context())
	if err != nil {
		t.Fatalf("LibraryCharts: %v", err)
	}
	if len(got.PhotosByYear) != 3 || got.PhotosByYear[1] != (YearPhotos{Year: 2025}) {
		t.Errorf("PhotosByYear = %+v, want the 2025 gap filled in", got.PhotosByYear)
	}
	if len(got.StorageByYear) != 1 || got.StorageByYear[0].CumulativeBytes != 90 {
		t.Errorf("StorageByYear = %+v, want a running total of 90", got.StorageByYear)
	}
}
