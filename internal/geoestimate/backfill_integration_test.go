//go:build integration

package geoestimate_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/geoestimate"
	"github.com/panbotka/kukatko/internal/photos"
)

// Positions used by the seeded fixtures. The two Prague points are ~2 km apart
// (coherent inside the default 5 km radius); Vienna is ~250 km away and turns
// any set it joins incoherent.
var (
	pragueA = point{lat: 50.0900, lng: 14.4000}
	pragueB = point{lat: 50.0870, lng: 14.4210}
	vienna  = point{lat: 48.2082, lng: 16.3738}
)

// point is a test fixture position.
type point struct {
	lat, lng float64
}

// day is the capture date the fixtures share; anchor is midday on it.
var anchor = time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

// fixture describes one photo to seed.
type fixture struct {
	// name becomes the file name and makes failures readable.
	name string
	// at is the capture time; the zero value means no date at all.
	at time.Time
	// loc is the photo's position; nil means no coordinates.
	loc *point
	// source is the photo's location_source.
	source string
	// dateEstimated marks taken_at itself a guess.
	dateEstimated bool
	// scan marks a digitised print.
	scan bool
}

// backfillEnv bundles the store under test and its counters.
type backfillEnv struct {
	store *photos.Store
	svc   *geoestimate.Service
	// enq records the uids handed to the geocode enqueuer.
	enq *recordingEnqueuer
}

// recordingEnqueuer records the photos scheduled for reverse geocoding.
type recordingEnqueuer struct {
	uids []string
}

// EnqueuePlaces records uid.
func (r *recordingEnqueuer) EnqueuePlaces(_ context.Context, uid string) error {
	r.uids = append(r.uids, uid)
	return nil
}

// newBackfillEnv truncates the test database and returns a service wired to it.
func newBackfillEnv(t *testing.T) *backfillEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	store := photos.NewStore(db.Pool())
	enq := &recordingEnqueuer{}
	return &backfillEnv{
		store: store,
		enq:   enq,
		svc: geoestimate.New(geoestimate.Config{
			Store:        store,
			Enqueuer:     enq,
			Window:       6 * time.Hour,
			RadiusMeters: 5000,
		}),
	}
}

// seed inserts a photo per f and returns it. Only the columns this feature reads
// are meaningful; the rest are filler that satisfies the NOT NULL constraints.
func (e *backfillEnv) seed(t *testing.T, f fixture) photos.Photo {
	t.Helper()
	p := photos.Photo{
		FileHash:         fmt.Sprintf("%x", sha256.Sum256([]byte(f.name))),
		FilePath:         "2024/06/" + f.name + ".jpg",
		FileName:         f.name + ".jpg",
		FileSize:         1024,
		FileMime:         "image/jpeg",
		FileWidth:        64,
		FileHeight:       48,
		MediaType:        photos.MediaImage,
		Title:            f.name,
		LocationSource:   f.source,
		TakenAtEstimated: f.dateEstimated,
		Scan:             f.scan,
	}
	if !f.at.IsZero() {
		at := f.at
		p.TakenAt = &at
	}
	if f.loc != nil {
		lat, lng := f.loc.lat, f.loc.lng
		p.Lat, p.Lng = &lat, &lng
	}
	created, err := e.store.Create(t.Context(), p)
	if err != nil {
		t.Fatalf("seeding %s: %v", f.name, err)
	}
	return created
}

// get reloads a photo.
func (e *backfillEnv) get(t *testing.T, uid string) photos.Photo {
	t.Helper()
	p, err := e.store.GetByUID(t.Context(), uid)
	if err != nil {
		t.Fatalf("GetByUID(%s): %v", uid, err)
	}
	return p
}

// run executes the backfill and returns how many photos it estimated.
func (e *backfillEnv) run(t *testing.T) int {
	t.Helper()
	n, err := e.svc.BackfillLocations(t.Context())
	if err != nil {
		t.Fatalf("BackfillLocations: %v", err)
	}
	return n
}

// TestBackfill_fillsAndMarksTheEstimate is the happy path: a photo with no GPS
// between two nearby located photos gets their location, marked as an estimate,
// and is scheduled for reverse geocoding so the places hierarchy picks it up.
func TestBackfill_fillsAndMarksTheEstimate(t *testing.T) {
	env := newBackfillEnv(t)
	env.seed(t, fixture{name: "located-a", at: anchor.Add(-time.Hour), loc: &pragueA, source: photos.LocationSourceExif})
	env.seed(t, fixture{name: "located-b", at: anchor.Add(time.Hour), loc: &pragueB, source: photos.LocationSourceExif})
	target := env.seed(t, fixture{name: "no-gps", at: anchor})

	if got := env.run(t); got != 1 {
		t.Fatalf("backfill estimated %d photos, want 1", got)
	}

	got := env.get(t, target.UID)
	if got.Lat == nil || got.Lng == nil {
		t.Fatalf("photo still has no location: %+v", got)
	}
	// The centroid of the two neighbours, not one of them.
	wantLat, wantLng := (pragueA.lat+pragueB.lat)/2, (pragueA.lng+pragueB.lng)/2
	if diff := *got.Lat - wantLat; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("lat = %v, want the neighbours' centroid %v", *got.Lat, wantLat)
	}
	if diff := *got.Lng - wantLng; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("lng = %v, want the neighbours' centroid %v", *got.Lng, wantLng)
	}
	// The whole point: it is stored marked, so nothing downstream can mistake it
	// for a measured coordinate.
	if got.LocationSource != photos.LocationSourceEstimate {
		t.Errorf("location_source = %q, want %q", got.LocationSource, photos.LocationSourceEstimate)
	}
	if len(env.enq.uids) != 1 || env.enq.uids[0] != target.UID {
		t.Errorf("enqueued %v, want a places job for %s", env.enq.uids, target.UID)
	}
}

// TestBackfill_isIdempotent checks a second run neither re-estimates nor
// re-enqueues: an estimated photo has stopped being a candidate.
func TestBackfill_isIdempotent(t *testing.T) {
	env := newBackfillEnv(t)
	env.seed(t, fixture{name: "located", at: anchor, loc: &pragueA, source: photos.LocationSourceExif})
	target := env.seed(t, fixture{name: "no-gps", at: anchor})

	if got := env.run(t); got != 1 {
		t.Fatalf("first run estimated %d, want 1", got)
	}
	first := env.get(t, target.UID)

	if got := env.run(t); got != 0 {
		t.Errorf("second run estimated %d, want 0 — the work was already done", got)
	}
	if len(env.enq.uids) != 1 {
		t.Errorf("enqueued %v, want the geocode scheduled once, not once per run", env.enq.uids)
	}
	second := env.get(t, target.UID)
	if *second.Lat != *first.Lat || *second.Lng != *first.Lng {
		t.Errorf("the estimate moved between runs: %v,%v then %v,%v",
			*first.Lat, *first.Lng, *second.Lat, *second.Lng)
	}
}

// TestBackfill_neverReEstimatesAClearedLocation is the rule that makes the
// feature liveable: clearing an estimate is a decision, and re-adding it on every
// nightly pass would be maddening. The clear is simulated exactly as photoapi
// performs it — coordinates gone, source stamped "manual".
func TestBackfill_neverReEstimatesAClearedLocation(t *testing.T) {
	env := newBackfillEnv(t)
	env.seed(t, fixture{name: "located", at: anchor, loc: &pragueA, source: photos.LocationSourceExif})
	target := env.seed(t, fixture{name: "no-gps", at: anchor})

	if got := env.run(t); got != 1 {
		t.Fatalf("first run estimated %d, want 1", got)
	}

	// The user throws the estimate away.
	cleared := env.get(t, target.UID)
	update := metadataFrom(cleared)
	update.Lat, update.Lng = nil, nil
	update.LocationSource = photos.LocationSourceManual
	if _, err := env.store.UpdateMetadata(t.Context(), target.UID, update); err != nil {
		t.Fatalf("clearing the estimate: %v", err)
	}

	if got := env.run(t); got != 0 {
		t.Errorf("re-run estimated %d, want 0 — a cleared estimate must stay cleared", got)
	}
	after := env.get(t, target.UID)
	if after.Lat != nil || after.Lng != nil {
		t.Errorf("the cleared location came back: %v, %v", after.Lat, after.Lng)
	}
}

// TestBackfill_neverTouchesAManualLocation checks the estimator cannot overwrite
// a location the user set, even when the day's neighbours disagree with it.
func TestBackfill_neverTouchesAManualLocation(t *testing.T) {
	env := newBackfillEnv(t)
	env.seed(t, fixture{name: "located", at: anchor, loc: &pragueA, source: photos.LocationSourceExif})
	manual := env.seed(t, fixture{name: "user-placed", at: anchor, loc: &vienna, source: photos.LocationSourceManual})

	if got := env.run(t); got != 0 {
		t.Errorf("backfill estimated %d, want 0 — nothing was eligible", got)
	}
	got := env.get(t, manual.UID)
	if *got.Lat != vienna.lat || *got.Lng != vienna.lng {
		t.Errorf("the user's location moved to %v,%v", *got.Lat, *got.Lng)
	}
	if got.LocationSource != photos.LocationSourceManual {
		t.Errorf("location_source = %q, want it left %q", got.LocationSource, photos.LocationSourceManual)
	}
}

// TestBackfill_skipsWhatItCannotJudge covers the photos that are ineligible or
// unanswerable in one table: each seeds its own scenario and expects no estimate.
func TestBackfill_skipsWhatItCannotJudge(t *testing.T) {
	tests := []struct {
		name string
		// target is the photo that must be left alone.
		target fixture
		// neighbours are seeded alongside it.
		neighbours []fixture
	}{
		{
			// An estimated date makes an estimated location a guess about a guess.
			name:       "a photo whose date is itself an estimate",
			target:     fixture{name: "circa-1950", at: anchor, dateEstimated: true},
			neighbours: []fixture{{name: "located", at: anchor, loc: &pragueA, source: photos.LocationSourceExif}},
		},
		{
			// A print's capture date says nothing about where the scanner was.
			name:       "a scan",
			target:     fixture{name: "scanned", at: anchor, scan: true},
			neighbours: []fixture{{name: "located", at: anchor, loc: &pragueA, source: photos.LocationSourceExif}},
		},
		{
			name:       "a photo with no date at all",
			target:     fixture{name: "undated"},
			neighbours: []fixture{{name: "located", at: anchor, loc: &pragueA, source: photos.LocationSourceExif}},
		},
		{
			// The headline rule: a day spanning two cities has no honest answer.
			name:   "a day spanning Prague and Vienna",
			target: fixture{name: "no-gps", at: anchor},
			neighbours: []fixture{
				{name: "in-prague", at: anchor.Add(-time.Hour), loc: &pragueA, source: photos.LocationSourceExif},
				{name: "in-vienna", at: anchor.Add(time.Hour), loc: &vienna, source: photos.LocationSourceExif},
			},
		},
		{
			name:       "no located photos anywhere near in time",
			target:     fixture{name: "no-gps", at: anchor},
			neighbours: []fixture{{name: "far-off", at: anchor.Add(72 * time.Hour), loc: &pragueA, source: photos.LocationSourceExif}},
		},
		{
			// Located photos exist that day, but only estimated ones — a guess must
			// not seed another guess.
			name:       "neighbours that are themselves estimates",
			target:     fixture{name: "no-gps", at: anchor},
			neighbours: []fixture{{name: "guessed", at: anchor, loc: &pragueA, source: photos.LocationSourceEstimate}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newBackfillEnv(t)
			for _, n := range tt.neighbours {
				env.seed(t, n)
			}
			target := env.seed(t, tt.target)

			if got := env.run(t); got != 0 {
				t.Errorf("backfill estimated %d photos, want 0", got)
			}
			after := env.get(t, target.UID)
			if after.Lat != nil || after.Lng != nil {
				t.Errorf("photo was given a location %v,%v, want none", after.Lat, after.Lng)
			}
			if after.LocationSource != tt.target.source {
				t.Errorf("location_source = %q, want it left %q", after.LocationSource, tt.target.source)
			}
			if len(env.enq.uids) != 0 {
				t.Errorf("enqueued %v, want no geocode spent on a photo with no location", env.enq.uids)
			}
		})
	}
}

// TestBackfill_windowExcludesTheNextDay checks the ±window bound is real: a
// located photo 8 hours away is outside the 6-hour window and cannot lend its
// location.
func TestBackfill_windowExcludesTheNextDay(t *testing.T) {
	env := newBackfillEnv(t)
	env.seed(t, fixture{name: "too-late", at: anchor.Add(8 * time.Hour), loc: &pragueA, source: photos.LocationSourceExif})
	target := env.seed(t, fixture{name: "no-gps", at: anchor})

	if got := env.run(t); got != 0 {
		t.Errorf("backfill estimated %d, want 0 — the only located photo is out of the window", got)
	}
	if after := env.get(t, target.UID); after.Lat != nil {
		t.Errorf("photo was given a location %v, want none", *after.Lat)
	}
}

// metadataFrom builds the full MetadataUpdate for a photo, mirroring what
// photoapi's merge produces for an untouched row, so a test can overlay just the
// fields it means to change.
func metadataFrom(p photos.Photo) photos.MetadataUpdate {
	return photos.MetadataUpdate{
		Title:            p.Title,
		Description:      p.Description,
		Notes:            p.Notes,
		AiNote:           p.AiNote,
		Subject:          p.Subject,
		Keywords:         p.Keywords,
		Artist:           p.Artist,
		Copyright:        p.Copyright,
		License:          p.License,
		Scan:             p.Scan,
		TakenAt:          p.TakenAt,
		TakenAtSource:    p.TakenAtSource,
		TakenAtEstimated: p.TakenAtEstimated,
		TakenAtNote:      p.TakenAtNote,
		Lat:              p.Lat,
		Lng:              p.Lng,
		Altitude:         p.Altitude,
		LocationSource:   p.LocationSource,
		Private:          p.Private,
	}
}
