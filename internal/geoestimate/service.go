package geoestimate

import (
	"context"
	"fmt"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
)

// DefaultWindow is the default half-width of the time window a photo's
// neighbours are drawn from. Six hours either side keeps a photo inside one
// outing rather than one calendar day: a day that starts in Brno and ends in
// Vienna is exactly the case where a same-day estimate would be wrong, and the
// narrower window either finds coherent neighbours or finds none.
const DefaultWindow = 6 * time.Hour

// DefaultRadiusMeters is the default coherence radius. Five kilometres is about
// the size of an outing that can honestly be called "the same place" — a town, a
// valley, a stretch of coast — and it is well below the distance at which a
// wrong pin would land in the wrong city and corrupt the places hierarchy.
const DefaultRadiusMeters = 5000.0

// Store is the catalogue access the estimator needs. It is satisfied by
// photos.Store.
type Store interface {
	// ListLocationCandidates returns the photos eligible for estimation, capped at
	// limit (zero or less means all).
	ListLocationCandidates(ctx context.Context, limit int) ([]photos.LocationCandidate, error)
	// ListLocatedNeighbours returns the measured positions captured in [from, to].
	ListLocatedNeighbours(ctx context.Context, from, to time.Time) ([]photos.LocatedPoint, error)
	// SetEstimatedLocation writes an estimate onto a photo that still has none,
	// reporting whether the row was written.
	SetEstimatedLocation(ctx context.Context, uid string, lat, lng float64) (bool, error)
}

// Enqueuer schedules `places` jobs so a newly estimated location is reverse
// geocoded into the places hierarchy. It is satisfied by jobs.Enqueuer.
type Enqueuer interface {
	// EnqueuePlaces schedules reverse geocoding for photoUID, treating an existing
	// active job as a no-op.
	EnqueuePlaces(ctx context.Context, photoUID string) error
}

// Config bundles the dependencies and tuning of New. Store is required;
// Enqueuer is optional (without one, estimated locations are stored but not
// geocoded, which is how the service degrades when no mapy.com key is set).
type Config struct {
	// Store reads candidates and neighbours and writes estimates.
	Store Store
	// Enqueuer schedules the reverse-geocode job for each new estimate. May be nil.
	Enqueuer Enqueuer
	// Window is the half-width of the neighbour time window. Non-positive means
	// DefaultWindow.
	Window time.Duration
	// RadiusMeters is the coherence radius. Non-positive means
	// DefaultRadiusMeters.
	RadiusMeters float64
}

// Service estimates missing locations from photos taken nearby in time.
type Service struct {
	store    Store
	enqueuer Enqueuer
	window   time.Duration
	radiusM  float64
}

// New returns a Service from cfg, substituting the package defaults for a
// non-positive window or radius.
func New(cfg Config) *Service {
	s := &Service{
		store:    cfg.Store,
		enqueuer: cfg.Enqueuer,
		window:   cfg.Window,
		radiusM:  cfg.RadiusMeters,
	}
	if s.window <= 0 {
		s.window = DefaultWindow
	}
	if s.radiusM <= 0 {
		s.radiusM = DefaultRadiusMeters
	}
	return s
}

// BackfillLocations estimates a location for every eligible photo and returns
// how many it filled in. Photos whose neighbours are missing or disagree are
// skipped silently — refusing is the normal outcome, not an error.
//
// It is safe to re-run: an estimated photo stops being a candidate, and a photo
// whose estimate the user cleared carries a 'manual' tombstone that keeps it out
// of the candidate set for good. Re-running is also the only resume mechanism —
// there is no cursor, because the candidate set shrinks as the work is done, so
// a run that dies halfway simply leaves a smaller job for the next one. On
// failure the count of what was already committed is returned alongside the
// error.
func (s *Service) BackfillLocations(ctx context.Context) (int, error) {
	candidates, err := s.store.ListLocationCandidates(ctx, 0)
	if err != nil {
		return 0, fmt.Errorf("geoestimate: listing candidates: %w", err)
	}
	estimated := 0
	for _, c := range candidates {
		ok, err := s.estimateOne(ctx, c)
		if err != nil {
			return estimated, err
		}
		if ok {
			estimated++
		}
	}
	return estimated, nil
}

// estimateOne estimates and stores the location of a single candidate,
// reporting whether it wrote one. A candidate with no neighbours, or with
// neighbours that disagree, yields (false, nil): there is nothing to say about
// that photo, which is a normal result and not a failure.
func (s *Service) estimateOne(ctx context.Context, c photos.LocationCandidate) (bool, error) {
	neighbours, err := s.store.ListLocatedNeighbours(ctx, c.TakenAt.Add(-s.window), c.TakenAt.Add(s.window))
	if err != nil {
		return false, fmt.Errorf("geoestimate: listing neighbours of %s: %w", c.UID, err)
	}
	point, ok := Estimate(toPoints(neighbours), s.radiusM)
	if !ok {
		return false, nil
	}
	written, err := s.store.SetEstimatedLocation(ctx, c.UID, point.Lat, point.Lng)
	if err != nil {
		return false, fmt.Errorf("geoestimate: storing estimate for %s: %w", c.UID, err)
	}
	if !written {
		// The photo gained a location or a decision while we were working it out.
		// That location is measured or chosen and ours is a guess, so it loses.
		return false, nil
	}
	if err := s.enqueueGeocode(ctx, c.UID); err != nil {
		return false, err
	}
	return true, nil
}

// enqueueGeocode schedules reverse geocoding for a freshly estimated photo, so
// the places hierarchy picks the new location up. It is a no-op without an
// Enqueuer.
//
// The estimate is already committed by the time this runs, which is deliberate:
// the `places` job re-reads the photo's coordinates and skips a geocode whose
// cached place already matches, so enqueuing after the write is what makes the
// job see the new location rather than the old one. Enqueuing is free — the
// metered mapy.com call happens in the worker, behind the existing
// maps.geocode_rate_per_sec throttle, so a large backfill drip-feeds the
// geocoder instead of blasting it.
func (s *Service) enqueueGeocode(ctx context.Context, uid string) error {
	if s.enqueuer == nil {
		return nil
	}
	if err := s.enqueuer.EnqueuePlaces(ctx, uid); err != nil {
		return fmt.Errorf("geoestimate: enqueuing places for %s: %w", uid, err)
	}
	return nil
}

// toPoints converts catalogue rows to the plain points the estimator works on.
func toPoints(rows []photos.LocatedPoint) []Point {
	out := make([]Point, len(rows))
	for i, r := range rows {
		out[i] = Point{Lat: r.Lat, Lng: r.Lng}
	}
	return out
}
