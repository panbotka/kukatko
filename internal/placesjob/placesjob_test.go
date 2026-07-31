package placesjob

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mapy"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
	"github.com/panbotka/kukatko/internal/worker"
)

// fakePhotos is a PhotoStore stub returning canned photos by uid.
type fakePhotos struct {
	byUID map[string]photos.Photo
}

// GetByUID returns the canned photo or photos.ErrPhotoNotFound.
func (f *fakePhotos) GetByUID(_ context.Context, uid string) (photos.Photo, error) {
	p, ok := f.byUID[uid]
	if !ok {
		return photos.Photo{}, photos.ErrPhotoNotFound
	}
	return p, nil
}

// fakePlaces is an in-memory PlaceStore recording saved places and serving a seed.
type fakePlaces struct {
	saved   map[string]places.Place
	missing []string
}

// newFakePlaces returns an empty fake place store.
func newFakePlaces() *fakePlaces {
	return &fakePlaces{saved: map[string]places.Place{}}
}

// GetPlace returns the stored place or places.ErrPlaceNotFound.
func (f *fakePlaces) GetPlace(_ context.Context, photoUID string) (places.Place, error) {
	p, ok := f.saved[photoUID]
	if !ok {
		return places.Place{}, places.ErrPlaceNotFound
	}
	return p, nil
}

// SavePlace records p, stamping a fixed geocoded_at, and returns it.
func (f *fakePlaces) SavePlace(_ context.Context, p places.Place) (places.Place, error) {
	p.GeocodedAt = time.Unix(0, 0)
	f.saved[p.PhotoUID] = p
	return p, nil
}

// ListPhotosMissingPlaces returns the canned missing-uid list.
func (f *fakePlaces) ListPhotosMissingPlaces(_ context.Context, _ int) ([]string, error) {
	return f.missing, nil
}

// fakeGeocoder is a Geocoder stub returning a canned result or error and counting
// calls.
type fakeGeocoder struct {
	result *mapy.GeocodeResult
	err    error
	calls  int
}

// ReverseGeocode records the call and returns the canned outcome.
func (f *fakeGeocoder) ReverseGeocode(_ context.Context, _, _ float64) (*mapy.GeocodeResult, error) {
	f.calls++
	return f.result, f.err
}

// fakeEnqueuer records the uids it was asked to enqueue.
type fakeEnqueuer struct {
	uids []string
	err  error
}

// EnqueuePlaces records photoUID and returns the preset error.
func (f *fakeEnqueuer) EnqueuePlaces(_ context.Context, photoUID string) error {
	if f.err != nil {
		return f.err
	}
	f.uids = append(f.uids, photoUID)
	return nil
}

// denyLimiter is a RateLimiter that always denies, to exercise the local-throttle
// deferral.
type denyLimiter struct{}

// Allow always denies.
func (denyLimiter) Allow() bool { return false }

// fakeBudget is a CreditBudget stub with a canned verdict, counting reservations
// and refunds so a test can assert the credit accounting.
type fakeBudget struct {
	deny       bool
	retryAfter time.Duration
	reserved   int
	refunded   int
}

// Reserve records the call and returns the canned verdict.
func (f *fakeBudget) Reserve() (time.Duration, bool) {
	if f.deny {
		return f.retryAfter, false
	}
	f.reserved++
	return 0, true
}

// Refund records that a reserved credit was handed back.
func (f *fakeBudget) Refund() { f.refunded++ }

// Snapshot reports the reservations made so far against a nominal limit.
func (f *fakeBudget) Snapshot() BudgetSnapshot {
	return BudgetSnapshot{Enabled: true, Limit: 10, Spent: f.reserved - f.refunded}
}

// fakeMeter counts the credits the service reports as spent.
type fakeMeter struct{ spent int }

// GeocodeCreditSpent records one spent credit.
func (f *fakeMeter) GeocodeCreditSpent() { f.spent++ }

// c<geo> builds a typical Czech regionalStructure with the most specific entry
// first and the country last, matching mapy.com's ordering.
func czGeo() *mapy.GeocodeResult {
	return &mapy.GeocodeResult{
		Name: "Pražský hrad",
		RegionalStructure: []mapy.RegionalItem{
			{Name: "Pražský hrad", Type: "regional.address"},
			{Name: "Hradčany", Type: "regional.municipality_part"},
			{Name: "Praha", Type: "regional.municipality"},
			{Name: "Hlavní město Praha", Type: "regional.region"},
			{Name: "Česko", Type: "regional.country"},
		},
	}
}

// TestParsePlace verifies the regionalStructure → place-field mapping, including
// the bare-type fallback and a missing-name fallback.
func TestParsePlace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                            string
		result                          *mapy.GeocodeResult
		country, region, city, placeNam string
	}{
		{
			name:    "full czech hierarchy",
			result:  czGeo(),
			country: "Česko", region: "Hlavní město Praha", city: "Praha", placeNam: "Pražský hrad",
		},
		{
			name: "bare types without regional prefix",
			result: &mapy.GeocodeResult{
				Name: "Brno",
				RegionalStructure: []mapy.RegionalItem{
					{Name: "Brno", Type: "municipality"},
					{Name: "Jihomoravský kraj", Type: "region"},
					{Name: "Česko", Type: "country"},
				},
			},
			country: "Česko", region: "Jihomoravský kraj", city: "Brno", placeNam: "Brno",
		},
		{
			name: "empty name falls back to most specific entry",
			result: &mapy.GeocodeResult{
				Name: "",
				RegionalStructure: []mapy.RegionalItem{
					{Name: "Some Street", Type: "regional.street"},
					{Name: "Wien", Type: "regional.municipality"},
				},
			},
			country: "", region: "", city: "Wien", placeNam: "Some Street",
		},
		{
			name:    "no regional structure",
			result:  &mapy.GeocodeResult{Name: "Nowhere"},
			country: "", region: "", city: "", placeNam: "Nowhere",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			country, region, city, name := parsePlace(tt.result)
			if country != tt.country || region != tt.region || city != tt.city || name != tt.placeNam {
				t.Errorf("parsePlace = (%q,%q,%q,%q), want (%q,%q,%q,%q)",
					country, region, city, name, tt.country, tt.region, tt.city, tt.placeNam)
			}
		})
	}
}

// newService wires a Service over the given collaborators with the default
// (always-allow) limiter unless overridden.
func newService(p PhotoStore, pl PlaceStore, g Geocoder, e Enqueuer, lim RateLimiter) *Service {
	return New(Config{Photos: p, Places: pl, Geocoder: g, Enqueuer: e, Limiter: lim})
}

// TestGeocode_storesParsedPlace verifies a geotagged photo is geocoded and the
// parsed place (with the source coordinates) is cached.
func TestGeocode_storesParsedPlace(t *testing.T) {
	t.Parallel()

	photo := photos.Photo{UID: "ph1", Lat: new(50.09), Lng: new(14.40)}
	pho := &fakePhotos{byUID: map[string]photos.Photo{"ph1": photo}}
	pl := newFakePlaces()
	geo := &fakeGeocoder{result: czGeo()}
	svc := newService(pho, pl, geo, &fakeEnqueuer{}, nil)

	if err := svc.Geocode(context.Background(), "ph1"); err != nil {
		t.Fatalf("Geocode: %v", err)
	}
	got := pl.saved["ph1"]
	if got.Country != "Česko" || got.City != "Praha" || got.PlaceName != "Pražský hrad" {
		t.Errorf("stored place = %+v", got)
	}
	if got.Lat == nil || *got.Lat != 50.09 || got.Lng == nil || *got.Lng != 14.40 {
		t.Errorf("stored coords = (%v,%v), want (50.09,14.40)", got.Lat, got.Lng)
	}
	if geo.calls != 1 {
		t.Errorf("geocoder calls = %d, want 1", geo.calls)
	}
}

// TestGeocode_idempotentSameCoords verifies a photo already geocoded for its
// current coordinates is skipped without calling the geocoder.
func TestGeocode_idempotentSameCoords(t *testing.T) {
	t.Parallel()

	photo := photos.Photo{UID: "ph1", Lat: new(50.0), Lng: new(14.0)}
	pho := &fakePhotos{byUID: map[string]photos.Photo{"ph1": photo}}
	pl := newFakePlaces()
	pl.saved["ph1"] = places.Place{PhotoUID: "ph1", City: "Praha", Lat: new(50.0), Lng: new(14.0)}
	geo := &fakeGeocoder{result: czGeo()}
	svc := newService(pho, pl, geo, &fakeEnqueuer{}, nil)

	if err := svc.Geocode(context.Background(), "ph1"); err != nil {
		t.Fatalf("Geocode: %v", err)
	}
	if geo.calls != 0 {
		t.Errorf("geocoder calls = %d, want 0 (idempotent skip)", geo.calls)
	}
}

// TestGeocode_recomputesOnCoordinateChange verifies a stored place with stale
// coordinates is re-geocoded.
func TestGeocode_recomputesOnCoordinateChange(t *testing.T) {
	t.Parallel()

	photo := photos.Photo{UID: "ph1", Lat: new(50.0), Lng: new(14.0)}
	pho := &fakePhotos{byUID: map[string]photos.Photo{"ph1": photo}}
	pl := newFakePlaces()
	pl.saved["ph1"] = places.Place{PhotoUID: "ph1", City: "Old", Lat: new(49.0), Lng: new(13.0)}
	geo := &fakeGeocoder{result: czGeo()}
	svc := newService(pho, pl, geo, &fakeEnqueuer{}, nil)

	if err := svc.Geocode(context.Background(), "ph1"); err != nil {
		t.Fatalf("Geocode: %v", err)
	}
	if geo.calls != 1 {
		t.Errorf("geocoder calls = %d, want 1 (re-geocode)", geo.calls)
	}
	if pl.saved["ph1"].City != "Praha" {
		t.Errorf("place not updated: %+v", pl.saved["ph1"])
	}
}

// TestGeocode_noGPSRecordsProcessed verifies a photo without coordinates is
// recorded as processed (empty marker, nil coords) and the geocoder is not called.
func TestGeocode_noGPSRecordsProcessed(t *testing.T) {
	t.Parallel()

	photo := photos.Photo{UID: "ph1"} // no Lat/Lng
	pho := &fakePhotos{byUID: map[string]photos.Photo{"ph1": photo}}
	pl := newFakePlaces()
	geo := &fakeGeocoder{result: czGeo()}
	svc := newService(pho, pl, geo, &fakeEnqueuer{}, nil)

	if err := svc.Geocode(context.Background(), "ph1"); err != nil {
		t.Fatalf("Geocode: %v", err)
	}
	got, ok := pl.saved["ph1"]
	if !ok {
		t.Fatal("no place recorded for GPS-less photo")
	}
	if got.PlaceName != "" || got.Lat != nil || got.Lng != nil {
		t.Errorf("expected empty processed marker, got %+v", got)
	}
	if geo.calls != 0 {
		t.Errorf("geocoder calls = %d, want 0", geo.calls)
	}

	// A second run must skip it (recorded as processed, coordinates unchanged).
	if err := svc.Geocode(context.Background(), "ph1"); err != nil {
		t.Fatalf("Geocode (2nd): %v", err)
	}
	if geo.calls != 0 {
		t.Errorf("geocoder calls after 2nd run = %d, want 0", geo.calls)
	}
}

// TestGeocode_notFoundRecordsProcessed verifies a coordinate with no mapy.com
// match is recorded as processed at those coordinates so it is not retried.
func TestGeocode_notFoundRecordsProcessed(t *testing.T) {
	t.Parallel()

	photo := photos.Photo{UID: "ph1", Lat: new(0.0), Lng: new(0.0)}
	pho := &fakePhotos{byUID: map[string]photos.Photo{"ph1": photo}}
	pl := newFakePlaces()
	geo := &fakeGeocoder{err: mapy.ErrNotFound}
	svc := newService(pho, pl, geo, &fakeEnqueuer{}, nil)

	if err := svc.Geocode(context.Background(), "ph1"); err != nil {
		t.Fatalf("Geocode: %v", err)
	}
	got, ok := pl.saved["ph1"]
	if !ok {
		t.Fatal("no place recorded for unmatched coordinate")
	}
	if got.PlaceName != "" || got.Lat == nil || *got.Lat != 0.0 {
		t.Errorf("expected empty marker at source coords, got %+v", got)
	}
}

// TestGeocode_offlineDefers verifies an unavailable/rate-limited upstream defers
// the job via worker.RetryAfter (no attempt burned) without writing a place.
func TestGeocode_offlineDefers(t *testing.T) {
	t.Parallel()

	for _, upstreamErr := range []error{mapy.ErrUnavailable, mapy.ErrRateLimited} {
		photo := photos.Photo{UID: "ph1", Lat: new(50.0), Lng: new(14.0)}
		pho := &fakePhotos{byUID: map[string]photos.Photo{"ph1": photo}}
		pl := newFakePlaces()
		geo := &fakeGeocoder{err: upstreamErr}
		svc := newService(pho, pl, geo, &fakeEnqueuer{}, nil)

		err := svc.Geocode(context.Background(), "ph1")
		var ra *worker.RetryAfterError
		if !errors.As(err, &ra) {
			t.Fatalf("err for %v = %v, want RetryAfterError", upstreamErr, err)
		}
		if ra.Delay != DefaultOfflineRetryDelay {
			t.Errorf("delay = %s, want %s", ra.Delay, DefaultOfflineRetryDelay)
		}
		if _, ok := pl.saved["ph1"]; ok {
			t.Error("place written despite offline upstream")
		}
	}
}

// TestGeocode_localRateLimitDefers verifies an empty local limiter defers the job
// without calling the geocoder or writing a place.
func TestGeocode_localRateLimitDefers(t *testing.T) {
	t.Parallel()

	photo := photos.Photo{UID: "ph1", Lat: new(50.0), Lng: new(14.0)}
	pho := &fakePhotos{byUID: map[string]photos.Photo{"ph1": photo}}
	pl := newFakePlaces()
	geo := &fakeGeocoder{result: czGeo()}
	svc := newService(pho, pl, geo, &fakeEnqueuer{}, denyLimiter{})

	err := svc.Geocode(context.Background(), "ph1")
	var ra *worker.RetryAfterError
	if !errors.As(err, &ra) {
		t.Fatalf("err = %v, want RetryAfterError", err)
	}
	if ra.Delay != DefaultRateLimitDelay {
		t.Errorf("delay = %s, want %s", ra.Delay, DefaultRateLimitDelay)
	}
	if geo.calls != 0 {
		t.Errorf("geocoder calls = %d, want 0 (throttled before call)", geo.calls)
	}
}

// geotagged is a photo with coordinates, the input of every budget test below.
func geotagged() photos.Photo {
	return photos.Photo{UID: "ph1", Lat: new(50.0), Lng: new(14.0)}
}

// TestGeocode_budgetExhaustedDefers verifies an exhausted credit budget defers
// the job (it must not fail and must not burn a retry attempt), that no credit
// is spent — neither a mapy.com call nor a place write happens — and that the
// deferral waits for the budget to refill rather than retrying in a tight loop.
func TestGeocode_budgetExhaustedDefers(t *testing.T) {
	t.Parallel()

	pho := &fakePhotos{byUID: map[string]photos.Photo{"ph1": geotagged()}}
	pl := newFakePlaces()
	geo := &fakeGeocoder{result: czGeo()}
	meter := &fakeMeter{}
	budget := &fakeBudget{deny: true, retryAfter: 4 * time.Hour}
	svc := New(Config{
		Photos: pho, Places: pl, Geocoder: geo, Enqueuer: &fakeEnqueuer{},
		Budget: budget, Meter: meter,
	})

	err := svc.Geocode(context.Background(), "ph1")
	var ra *worker.RetryAfterError
	if !errors.As(err, &ra) {
		t.Fatalf("err = %v, want RetryAfterError (deferred, not failed)", err)
	}
	if ra.Delay != 4*time.Hour {
		t.Errorf("delay = %s, want %s (until the budget refills)", ra.Delay, 4*time.Hour)
	}
	if ra.Delay <= DefaultRateLimitDelay {
		t.Errorf("delay = %s, want longer than the rate-limit deferral %s so the queue cannot spin",
			ra.Delay, DefaultRateLimitDelay)
	}
	if geo.calls != 0 {
		t.Errorf("geocoder calls = %d, want 0 (no credit spent on an empty budget)", geo.calls)
	}
	if meter.spent != 0 {
		t.Errorf("metered spend = %d, want 0", meter.spent)
	}
	if _, ok := pl.saved["ph1"]; ok {
		t.Error("place written despite the budget being exhausted")
	}
}

// TestGeocode_rateLimitRefundsCredit verifies a credit reserved for a call the
// rate limiter then blocks is handed back, so throttling never burns budget.
func TestGeocode_rateLimitRefundsCredit(t *testing.T) {
	t.Parallel()

	pho := &fakePhotos{byUID: map[string]photos.Photo{"ph1": geotagged()}}
	geo := &fakeGeocoder{result: czGeo()}
	budget := &fakeBudget{}
	meter := &fakeMeter{}
	svc := New(Config{
		Photos: pho, Places: newFakePlaces(), Geocoder: geo, Enqueuer: &fakeEnqueuer{},
		Limiter: denyLimiter{}, Budget: budget, Meter: meter,
	})

	err := svc.Geocode(context.Background(), "ph1")
	var ra *worker.RetryAfterError
	if !errors.As(err, &ra) {
		t.Fatalf("err = %v, want RetryAfterError", err)
	}
	if ra.Delay != DefaultRateLimitDelay {
		t.Errorf("delay = %s, want the rate-limit deferral %s", ra.Delay, DefaultRateLimitDelay)
	}
	if budget.reserved != 1 || budget.refunded != 1 {
		t.Errorf("budget reserved/refunded = %d/%d, want 1/1", budget.reserved, budget.refunded)
	}
	if meter.spent != 0 {
		t.Errorf("metered spend = %d, want 0 (the call never happened)", meter.spent)
	}
}

// TestGeocode_meterCountsActualSpend verifies the credit counter reflects what
// was really spent: an answered lookup (match or no match) and a failure the
// geocoder itself returned cost a credit, while a call mapy.com never performed
// (offline, upstream throttled) costs nothing and returns the credit.
func TestGeocode_meterCountsActualSpend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		geocodeErr error
		wantSpent  int
		wantRefund int
	}{
		{name: "match", wantSpent: 1},
		{name: "no match", geocodeErr: mapy.ErrNotFound, wantSpent: 1},
		{name: "upstream error", geocodeErr: errors.New("boom"), wantSpent: 1},
		{name: "offline", geocodeErr: mapy.ErrUnavailable, wantRefund: 1},
		{name: "upstream rate limited", geocodeErr: mapy.ErrRateLimited, wantRefund: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pho := &fakePhotos{byUID: map[string]photos.Photo{"ph1": geotagged()}}
			geo := &fakeGeocoder{result: czGeo(), err: tt.geocodeErr}
			budget := &fakeBudget{}
			meter := &fakeMeter{}
			svc := New(Config{
				Photos: pho, Places: newFakePlaces(), Geocoder: geo, Enqueuer: &fakeEnqueuer{},
				Budget: budget, Meter: meter,
			})

			_ = svc.Geocode(context.Background(), "ph1")

			if meter.spent != tt.wantSpent {
				t.Errorf("metered spend = %d, want %d", meter.spent, tt.wantSpent)
			}
			if budget.reserved != 1 {
				t.Errorf("budget reserved = %d, want 1", budget.reserved)
			}
			if budget.refunded != tt.wantRefund {
				t.Errorf("budget refunded = %d, want %d", budget.refunded, tt.wantRefund)
			}
		})
	}
}

// TestGeocode_skippedWorkCostsNoCredit verifies the paths that never reach
// mapy.com — an already-current place and a photo without GPS — neither reserve
// nor spend a credit.
func TestGeocode_skippedWorkCostsNoCredit(t *testing.T) {
	t.Parallel()

	pl := newFakePlaces()
	pl.saved["ph1"] = places.Place{PhotoUID: "ph1", City: "Praha", Lat: new(50.0), Lng: new(14.0)}
	pho := &fakePhotos{byUID: map[string]photos.Photo{
		"ph1": geotagged(),
		"ph2": {UID: "ph2"}, // no GPS
	}}
	budget := &fakeBudget{}
	meter := &fakeMeter{}
	svc := New(Config{
		Photos: pho, Places: pl, Geocoder: &fakeGeocoder{result: czGeo()}, Enqueuer: &fakeEnqueuer{},
		Budget: budget, Meter: meter,
	})

	for _, uid := range []string{"ph1", "ph2"} {
		if err := svc.Geocode(context.Background(), uid); err != nil {
			t.Fatalf("Geocode(%s): %v", uid, err)
		}
	}
	if budget.reserved != 0 || meter.spent != 0 {
		t.Errorf("reserved/spent = %d/%d, want 0/0 for work that never geocodes",
			budget.reserved, meter.spent)
	}
}

// TestGeocode_budgetDrainsThenDefers verifies the end-to-end shape of a run
// against a real WindowBudget: the first Limit photos are geocoded, the rest are
// deferred until the window refills, and the readout reflects the spend.
func TestGeocode_budgetDrainsThenDefers(t *testing.T) {
	t.Parallel()

	const limit = 3
	byUID := map[string]photos.Photo{}
	for i := range 6 {
		uid := "ph" + string(rune('1'+i))
		byUID[uid] = photos.Photo{UID: uid, Lat: new(50.0 + float64(i)), Lng: new(14.0)}
	}
	geo := &fakeGeocoder{result: czGeo()}
	meter := &fakeMeter{}
	svc := New(Config{
		Photos: &fakePhotos{byUID: byUID}, Places: newFakePlaces(), Geocoder: geo,
		Enqueuer: &fakeEnqueuer{}, Meter: meter,
		Budget: NewWindowBudget(BudgetConfig{Limit: limit, Window: 24 * time.Hour}),
	})

	deferred := 0
	for i := range 6 {
		err := svc.Geocode(context.Background(), "ph"+string(rune('1'+i)))
		var ra *worker.RetryAfterError
		switch {
		case err == nil:
		case errors.As(err, &ra):
			deferred++
		default:
			t.Fatalf("Geocode #%d: unexpected error %v", i+1, err)
		}
	}

	if geo.calls != limit || meter.spent != limit {
		t.Errorf("geocoder calls/metered spend = %d/%d, want %d/%d", geo.calls, meter.spent, limit, limit)
	}
	if deferred != 6-limit {
		t.Errorf("deferred = %d, want %d", deferred, 6-limit)
	}
	snapshot := svc.BudgetSnapshot()
	if snapshot.Spent != limit || snapshot.Remaining != 0 {
		t.Errorf("snapshot = %+v, want %d spent and none remaining", snapshot, limit)
	}
}

// TestGeocode_missingPhotoErrors verifies a missing photo is a hard error so the
// job dead-letters rather than looping.
func TestGeocode_missingPhotoErrors(t *testing.T) {
	t.Parallel()

	svc := newService(&fakePhotos{byUID: map[string]photos.Photo{}}, newFakePlaces(),
		&fakeGeocoder{}, &fakeEnqueuer{}, nil)

	if err := svc.Geocode(context.Background(), "nope"); err == nil {
		t.Fatal("Geocode of missing photo = nil, want error")
	}
}

// TestHandle_payload verifies the worker entrypoint decodes the photo uid and
// rejects an empty/malformed payload as a permanent error.
func TestHandle_payload(t *testing.T) {
	t.Parallel()

	photo := photos.Photo{UID: "ph1", Lat: new(50.0), Lng: new(14.0)}
	pho := &fakePhotos{byUID: map[string]photos.Photo{"ph1": photo}}
	svc := newService(pho, newFakePlaces(), &fakeGeocoder{result: czGeo()}, &fakeEnqueuer{}, nil)

	payload, _ := json.Marshal(map[string]string{"photo_uid": "ph1"})
	if err := svc.Handle(context.Background(), jobs.Job{Payload: payload}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	empty, _ := json.Marshal(map[string]string{})
	if err := svc.Handle(context.Background(), jobs.Job{Payload: empty}); !errors.Is(err, ErrMissingPhotoUID) {
		t.Errorf("Handle(empty) = %v, want ErrMissingPhotoUID", err)
	}
	if err := svc.Handle(context.Background(), jobs.Job{Payload: []byte("not json")}); err == nil {
		t.Error("Handle(bad json) = nil, want error")
	}
}

// TestBackfillPlaces verifies the backfill enqueues one job per missing photo and
// returns the count.
func TestBackfillPlaces(t *testing.T) {
	t.Parallel()

	pl := newFakePlaces()
	pl.missing = []string{"ph1", "ph2", "ph3"}
	enq := &fakeEnqueuer{}
	svc := newService(&fakePhotos{}, pl, &fakeGeocoder{}, enq, nil)

	n, err := svc.BackfillPlaces(context.Background())
	if err != nil {
		t.Fatalf("BackfillPlaces: %v", err)
	}
	if n != 3 || len(enq.uids) != 3 {
		t.Errorf("enqueued = %d (uids %v), want 3", n, enq.uids)
	}
}

// TestNew_panicsOnMissingCollaborator verifies New rejects an incomplete wiring.
func TestNew_panicsOnMissingCollaborator(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("New with nil collaborators did not panic")
		}
	}()
	_ = New(Config{})
}
