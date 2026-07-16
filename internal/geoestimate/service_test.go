package geoestimate

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
)

// noon is the capture time every fake candidate carries.
var noon = time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

// fakeStore is a Store stub over in-memory canned data. It records the windows
// it was asked for and the estimates written to it.
type fakeStore struct {
	candidates []photos.LocationCandidate
	neighbours []photos.LocatedPoint

	// written collects each SetEstimatedLocation call in order.
	written []writtenEstimate
	// lost makes SetEstimatedLocation report the row as already decided, which is
	// how a photo that gained a location mid-run is simulated.
	lost bool
	// windows records the [from, to] of every neighbour query.
	windows []window

	listErr  error
	writeErr error
}

// writtenEstimate is one recorded SetEstimatedLocation call.
type writtenEstimate struct {
	uid      string
	lat, lng float64
}

// window is one recorded neighbour-query time range.
type window struct {
	from, to time.Time
}

// ListLocationCandidates returns the canned candidates or the canned error.
func (f *fakeStore) ListLocationCandidates(_ context.Context, _ int) ([]photos.LocationCandidate, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.candidates, nil
}

// ListLocatedNeighbours records the window and returns the canned neighbours.
func (f *fakeStore) ListLocatedNeighbours(_ context.Context, from, to time.Time) ([]photos.LocatedPoint, error) {
	f.windows = append(f.windows, window{from: from, to: to})
	return f.neighbours, nil
}

// SetEstimatedLocation records the write and reports whether it landed.
func (f *fakeStore) SetEstimatedLocation(_ context.Context, uid string, lat, lng float64) (bool, error) {
	if f.writeErr != nil {
		return false, f.writeErr
	}
	f.written = append(f.written, writtenEstimate{uid: uid, lat: lat, lng: lng})
	return !f.lost, nil
}

// fakeEnqueuer is an Enqueuer stub recording the uids it was handed.
type fakeEnqueuer struct {
	uids []string
	err  error
}

// EnqueuePlaces records the uid and returns the canned error.
func (f *fakeEnqueuer) EnqueuePlaces(_ context.Context, uid string) error {
	if f.err != nil {
		return f.err
	}
	f.uids = append(f.uids, uid)
	return nil
}

// candidate returns a candidate with the given uid captured at noon.
func candidate(uid string) photos.LocationCandidate {
	return photos.LocationCandidate{UID: uid, TakenAt: noon}
}

// located turns points into catalogue rows.
func located(points ...Point) []photos.LocatedPoint {
	out := make([]photos.LocatedPoint, len(points))
	for i, p := range points {
		out[i] = photos.LocatedPoint{Lat: p.Lat, Lng: p.Lng}
	}
	return out
}

func TestBackfillLocations_estimatesFromCoherentNeighbours(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		candidates: []photos.LocationCandidate{candidate("p1")},
		neighbours: located(pragueCastle, pragueOldTown),
	}
	enq := &fakeEnqueuer{}
	svc := New(Config{Store: store, Enqueuer: enq})

	got, err := svc.BackfillLocations(context.Background())
	if err != nil {
		t.Fatalf("BackfillLocations returned error: %v", err)
	}
	if got != 1 {
		t.Errorf("BackfillLocations estimated %d photos, want 1", got)
	}
	if len(store.written) != 1 || store.written[0].uid != "p1" {
		t.Fatalf("wrote %v, want one estimate for p1", store.written)
	}
	wantLat := (pragueCastle.Lat + pragueOldTown.Lat) / 2
	if math.Abs(store.written[0].lat-wantLat) > 1e-9 {
		t.Errorf("wrote lat %v, want the neighbours' centroid %v", store.written[0].lat, wantLat)
	}
	// The places hierarchy only picks the new location up if the geocode is
	// scheduled, so the enqueue is part of the contract, not a side effect.
	if len(enq.uids) != 1 || enq.uids[0] != "p1" {
		t.Errorf("enqueued %v, want a places job for p1", enq.uids)
	}
}

func TestBackfillLocations_producesNothingWhenNeighboursDisagree(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		candidates: []photos.LocationCandidate{candidate("p1")},
		neighbours: located(pragueCastle, vienna),
	}
	enq := &fakeEnqueuer{}
	svc := New(Config{Store: store, Enqueuer: enq})

	got, err := svc.BackfillLocations(context.Background())
	if err != nil {
		t.Fatalf("BackfillLocations returned error: %v", err)
	}
	if got != 0 {
		t.Errorf("BackfillLocations estimated %d photos, want 0", got)
	}
	if len(store.written) != 0 {
		t.Errorf("wrote %v, want nothing written for an incoherent day", store.written)
	}
	if len(enq.uids) != 0 {
		t.Errorf("enqueued %v, want no geocode for a photo that got no location", enq.uids)
	}
}

func TestBackfillLocations_producesNothingWithoutNeighbours(t *testing.T) {
	t.Parallel()

	store := &fakeStore{candidates: []photos.LocationCandidate{candidate("p1")}}
	svc := New(Config{Store: store})

	got, err := svc.BackfillLocations(context.Background())
	if err != nil {
		t.Fatalf("BackfillLocations returned error: %v", err)
	}
	if got != 0 || len(store.written) != 0 {
		t.Errorf("BackfillLocations estimated %d and wrote %v, want 0 and nothing", got, store.written)
	}
}

func TestBackfillLocations_noCandidatesIsANoOp(t *testing.T) {
	t.Parallel()

	store := &fakeStore{neighbours: located(pragueCastle)}
	svc := New(Config{Store: store})

	got, err := svc.BackfillLocations(context.Background())
	if err != nil {
		t.Fatalf("BackfillLocations returned error: %v", err)
	}
	if got != 0 {
		t.Errorf("BackfillLocations estimated %d photos, want 0", got)
	}
	if len(store.windows) != 0 {
		t.Errorf("queried neighbours %v times, want none without candidates", len(store.windows))
	}
}

// TestBackfillLocations_dropsTheEstimateWhenTheRowWasDecided covers the race the
// store's guarded UPDATE exists for: the photo gained a location between being
// listed and being written, so the estimate must lose and no geocode may be
// spent on it.
func TestBackfillLocations_dropsTheEstimateWhenTheRowWasDecided(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		candidates: []photos.LocationCandidate{candidate("p1")},
		neighbours: located(pragueCastle),
		lost:       true,
	}
	enq := &fakeEnqueuer{}
	svc := New(Config{Store: store, Enqueuer: enq})

	got, err := svc.BackfillLocations(context.Background())
	if err != nil {
		t.Fatalf("BackfillLocations returned error: %v", err)
	}
	if got != 0 {
		t.Errorf("BackfillLocations counted %d, want 0 for a row it did not write", got)
	}
	if len(enq.uids) != 0 {
		t.Errorf("enqueued %v, want no geocode for an estimate that was dropped", enq.uids)
	}
}

// TestBackfillLocations_windowIsCentredOnTheCandidate checks the neighbour query
// asks for ±window around the capture time, since the window is the feature's
// main knob.
func TestBackfillLocations_windowIsCentredOnTheCandidate(t *testing.T) {
	t.Parallel()

	store := &fakeStore{candidates: []photos.LocationCandidate{candidate("p1")}}
	svc := New(Config{Store: store, Window: 2 * time.Hour})

	if _, err := svc.BackfillLocations(context.Background()); err != nil {
		t.Fatalf("BackfillLocations returned error: %v", err)
	}
	if len(store.windows) != 1 {
		t.Fatalf("queried %d windows, want 1", len(store.windows))
	}
	wantFrom, wantTo := noon.Add(-2*time.Hour), noon.Add(2*time.Hour)
	if !store.windows[0].from.Equal(wantFrom) || !store.windows[0].to.Equal(wantTo) {
		t.Errorf("queried [%v, %v], want [%v, %v]",
			store.windows[0].from, store.windows[0].to, wantFrom, wantTo)
	}
}

func TestNew_defaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		cfg        Config
		wantWindow time.Duration
		wantRadius float64
	}{
		{
			name:       "a zero config takes the package defaults",
			cfg:        Config{},
			wantWindow: DefaultWindow,
			wantRadius: DefaultRadiusMeters,
		},
		{
			name:       "a negative window and radius fall back too",
			cfg:        Config{Window: -time.Hour, RadiusMeters: -1},
			wantWindow: DefaultWindow,
			wantRadius: DefaultRadiusMeters,
		},
		{
			name:       "explicit values win",
			cfg:        Config{Window: time.Hour, RadiusMeters: 250},
			wantWindow: time.Hour,
			wantRadius: 250,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := New(tt.cfg)
			if svc.window != tt.wantWindow {
				t.Errorf("window = %v, want %v", svc.window, tt.wantWindow)
			}
			if svc.radiusM != tt.wantRadius {
				t.Errorf("radiusM = %v, want %v", svc.radiusM, tt.wantRadius)
			}
		})
	}
}

func TestBackfillLocations_reportsErrors(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("boom")

	t.Run("a listing failure aborts the run", func(t *testing.T) {
		t.Parallel()
		svc := New(Config{Store: &fakeStore{listErr: errBoom}})
		if _, err := svc.BackfillLocations(context.Background()); !errors.Is(err, errBoom) {
			t.Errorf("BackfillLocations error = %v, want it to wrap %v", err, errBoom)
		}
	})

	t.Run("a write failure returns what was already committed", func(t *testing.T) {
		t.Parallel()
		store := &fakeStore{
			candidates: []photos.LocationCandidate{candidate("p1")},
			neighbours: located(pragueCastle),
			writeErr:   errBoom,
		}
		got, err := New(Config{Store: store}).BackfillLocations(context.Background())
		if !errors.Is(err, errBoom) {
			t.Errorf("BackfillLocations error = %v, want it to wrap %v", err, errBoom)
		}
		if got != 0 {
			t.Errorf("BackfillLocations counted %d, want 0 committed before the failure", got)
		}
	})

	t.Run("an estimate survives without an enqueuer", func(t *testing.T) {
		t.Parallel()
		store := &fakeStore{
			candidates: []photos.LocationCandidate{candidate("p1")},
			neighbours: located(pragueCastle),
		}
		got, err := New(Config{Store: store}).BackfillLocations(context.Background())
		if err != nil {
			t.Fatalf("BackfillLocations returned error: %v", err)
		}
		if got != 1 {
			t.Errorf("BackfillLocations estimated %d, want 1 with no geocoder wired", got)
		}
	})
}
