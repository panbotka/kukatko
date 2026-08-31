// Package placesjob wires reverse geocoding into Kukátko's background job system.
//
// Its centrepiece is the `places` job handler: given a photo uid it loads the
// photo and, when it carries GPS coordinates, asks the server-side mapy.com
// client to reverse-geocode them into a country / region / city / place-name
// hierarchy, which it caches in the photo_places side table so the library can
// later be browsed and filtered by location without re-hitting the rate-limited
// geocoder.
//
// The handler is idempotent — a photo whose place is already cached for its
// current coordinates is skipped without calling mapy.com (a coordinate change
// re-geocodes) — and degrades gracefully: when mapy.com is unreachable or rate
// limited, or the job's own credit-protecting limiter is empty, it returns a
// worker.RetryAfter so the job is requeued without burning a retry attempt
// (mirroring the embedding job's offline handling). A photo without GPS, or one
// the geocoder has no match for, is recorded as processed so it is never retried
// forever.
//
// Because every geocode costs a mapy.com credit, the job is bounded twice: the
// RateLimiter caps how fast credits are spent, and the CreditBudget (see
// budget.go) caps how many are spent per period. An exhausted budget defers the
// job until the budget refills — a long sleep, not a retry loop — so a
// full-library import spreads its credit spend over days instead of draining the
// quota in one pass. What it does spend is counted through a CreditMeter, so the
// spend is visible while it happens.
//
// Every collaborator — the photo store, the place cache, the geocoder, the rate
// limiter, the budget and the meter — is an interface so the Service unit-tests
// with fakes and no network or database.
package placesjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mapy"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
	"github.com/panbotka/kukatko/internal/worker"
)

const (
	// DefaultOfflineRetryDelay is how long a `places` job waits before becoming
	// runnable again after mapy.com was found unavailable or rate limited.
	DefaultOfflineRetryDelay = 5 * time.Minute
	// DefaultRateLimitDelay is how long a `places` job waits when the job's own
	// credit-protecting limiter has no token to spare; processing slowly is
	// acceptable, so the job simply tries again shortly without burning a retry.
	DefaultRateLimitDelay = time.Minute
)

// ErrMissingPhotoUID indicates a `places` job payload had no photo_uid.
var ErrMissingPhotoUID = errors.New("placesjob: payload missing photo_uid")

// errLocalRateLimited is the cause attached to the deferral the handler returns
// when its own limiter is empty. It is internal control flow, not surfaced.
var errLocalRateLimited = errors.New("placesjob: geocode rate limit reached")

// errBudgetExhausted is the cause attached to the deferral the handler returns
// when the credit budget for the current window is spent. Like
// errLocalRateLimited it is internal control flow, not surfaced.
var errBudgetExhausted = errors.New("placesjob: geocode credit budget exhausted")

// PhotoStore is the subset of photos.Store the service reads.
type PhotoStore interface {
	// GetByUID returns the photo with the given uid, or photos.ErrPhotoNotFound.
	GetByUID(ctx context.Context, uid string) (photos.Photo, error)
}

// PlaceStore is the subset of places.Store the service uses to read and write the
// place cache and to enumerate photos still missing it.
type PlaceStore interface {
	// GetPlace returns a photo's cached place, or places.ErrPlaceNotFound.
	GetPlace(ctx context.Context, photoUID string) (places.Place, error)
	// SavePlace inserts or replaces a photo's place row.
	SavePlace(ctx context.Context, p places.Place) (places.Place, error)
	// ListPhotosMissingPlaces returns uids of non-archived, geotagged photos with
	// no cached place yet (limit <= 0 returns all).
	ListPhotosMissingPlaces(ctx context.Context, limit int) ([]string, error)
}

// Geocoder reverse-geocodes a coordinate into a place. It is the subset of
// mapy.Client the service needs, behind an interface so tests substitute a fake
// without a real key or network.
type Geocoder interface {
	// ReverseGeocode resolves lat/lng to a simplified location, or a classified
	// mapy sentinel error (ErrNotFound, ErrUnavailable, ErrRateLimited, ...).
	ReverseGeocode(ctx context.Context, lat, lng float64) (*mapy.GeocodeResult, error)
}

// Enqueuer schedules `places` jobs for the backfill. It is satisfied by
// jobs.Enqueuer.
type Enqueuer interface {
	// EnqueuePlaces schedules reverse geocoding for photoUID, treating an existing
	// active job as a no-op.
	EnqueuePlaces(ctx context.Context, photoUID string) error
}

// Config bundles the Service's collaborators and tunables. Photos, Places,
// Geocoder and Enqueuer are required; the remaining fields fall back to package
// defaults when left zero (Limiter defaults to an always-allow limiter).
type Config struct {
	// Photos resolves a photo uid to its catalogue record.
	Photos PhotoStore
	// Places reads and writes the place cache and enumerates ungeocoded photos.
	Places PlaceStore
	// Geocoder reverse-geocodes coordinates via mapy.com.
	Geocoder Geocoder
	// Enqueuer schedules backfill jobs.
	Enqueuer Enqueuer
	// Limiter caps how often the job reaches mapy.com (default: always allow).
	Limiter RateLimiter
	// Budget caps how many geocodes may be spent per period, independently of
	// the rate (default: no budget at all).
	Budget CreditBudget
	// Meter counts the credits actually spent (default: no-op).
	Meter CreditMeter
	// OfflineRetryDelay is the deferral applied when mapy.com is unavailable or
	// rate limited (default DefaultOfflineRetryDelay).
	OfflineRetryDelay time.Duration
	// RateLimitDelay is the deferral applied when the local limiter is empty
	// (default DefaultRateLimitDelay).
	RateLimitDelay time.Duration
}

// Service reverse-geocodes photos into the place cache and backfills it.
type Service struct {
	photos         PhotoStore
	places         PlaceStore
	geocoder       Geocoder
	enqueuer       Enqueuer
	limiter        RateLimiter
	budget         CreditBudget
	meter          CreditMeter
	retryDelay     time.Duration
	rateLimitDelay time.Duration
}

// New builds a Service from cfg, applying defaults for the optional tunables. It
// panics if any required collaborator is nil, since none has a sensible default
// and a missing one is a wiring bug that should surface at startup.
func New(cfg Config) *Service {
	if cfg.Photos == nil || cfg.Places == nil || cfg.Geocoder == nil || cfg.Enqueuer == nil {
		panic("placesjob: New requires Photos, Places, Geocoder and Enqueuer")
	}
	limiter := cfg.Limiter
	if limiter == nil {
		limiter = allowAll{}
	}
	budget := cfg.Budget
	if budget == nil {
		budget = unlimitedBudget{}
	}
	meter := cfg.Meter
	if meter == nil {
		meter = noopMeter{}
	}
	retryDelay := cfg.OfflineRetryDelay
	if retryDelay <= 0 {
		retryDelay = DefaultOfflineRetryDelay
	}
	rateLimitDelay := cfg.RateLimitDelay
	if rateLimitDelay <= 0 {
		rateLimitDelay = DefaultRateLimitDelay
	}
	return &Service{
		photos:         cfg.Photos,
		places:         cfg.Places,
		geocoder:       cfg.Geocoder,
		enqueuer:       cfg.Enqueuer,
		limiter:        limiter,
		budget:         budget,
		meter:          meter,
		retryDelay:     retryDelay,
		rateLimitDelay: rateLimitDelay,
	}
}

// BudgetSnapshot reports the current state of the geocode credit budget, so the
// admin status endpoint can show what a running import is spending. It is safe
// to call at any time and never touches mapy.com.
func (s *Service) BudgetSnapshot() BudgetSnapshot { return s.budget.Snapshot() }

// jobPayload is the JSON shape of a `places` job's payload.
type jobPayload struct {
	PhotoUID string `json:"photo_uid"`
	// Force asks for the rebuild rather than the repair: mapy.com is asked again
	// and the answer replaces the cached place, even though the coordinate has
	// already been resolved. The repair path skips such a photo, which is right
	// when the place is merely missing and wrong when the cached one is stale — a
	// geocode taken before the place was renamed, or one recorded from a bad
	// answer. It costs a credit, so nothing schedules it automatically. See
	// jobs.Enqueuer.EnqueuePlacesRebuild.
	Force bool `json:"force"`
}

// Handle is the worker.HandlerFunc for `places` jobs: it decodes the photo uid
// from the job payload and geocodes it. A malformed or empty payload is a
// permanent error (the job dead-letters rather than retrying a payload that can
// never succeed).
func (s *Service) Handle(ctx context.Context, job jobs.Job) error {
	var p jobPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return fmt.Errorf("placesjob: decoding payload: %w", err)
	}
	if p.PhotoUID == "" {
		return ErrMissingPhotoUID
	}
	if p.Force {
		return s.ForceGeocode(ctx, p.PhotoUID)
	}
	return s.Geocode(ctx, p.PhotoUID)
}

// Geocode resolves photoUID's coordinates into the place cache. It is idempotent:
// a photo already geocoded for its current coordinates returns nil without
// calling mapy.com. A photo without GPS is recorded as processed (and never
// retried). When mapy.com is unavailable/rate limited, or the local limiter is
// empty, it returns a worker.RetryAfter so the job is requeued without consuming a
// retry attempt; any other failure is returned as an ordinary (retryable) error.
// A missing photo is returned as an error so the job fails and dead-letters.
func (s *Service) Geocode(ctx context.Context, photoUID string) error {
	return s.geocode(ctx, photoUID, false)
}

// ForceGeocode resolves photoUID's coordinates again and replaces the cached
// place, where Geocode would skip a photo already geocoded for those very
// coordinates. It is to Geocode what thumbjob.ForceRegenerate is to
// thumbjob.Regenerate: the repair fills a gap, the rebuild corrects an answer that
// is wrong rather than absent.
//
// It spends a mapy.com credit every time by definition, which is why nothing
// schedules it automatically — the upload pipeline and the backfill keep the
// skipping path. Everything else is Geocode's behaviour: there is one place row
// per photo, so the new answer replaces the old one; an unreachable or
// rate-limited geocoder, an empty local limiter and an exhausted credit budget all
// still defer without burning a retry attempt; a photo with no GPS is still
// recorded as processed rather than asked about.
func (s *Service) ForceGeocode(ctx context.Context, photoUID string) error {
	return s.geocode(ctx, photoUID, true)
}

// geocode resolves the photo's coordinates into the place cache, skipping a photo
// already geocoded for those coordinates unless force is set. It is the shared
// body of Geocode and ForceGeocode, so the two differ in exactly one thing —
// whether the cached answer is a reason to stop.
func (s *Service) geocode(ctx context.Context, photoUID string, force bool) error {
	photo, err := s.photos.GetByUID(ctx, photoUID)
	if err != nil {
		return fmt.Errorf("placesjob: loading photo %s: %w", photoUID, err)
	}
	if !force {
		current, currentErr := s.alreadyCurrent(ctx, photo)
		if currentErr != nil {
			return currentErr
		}
		if current {
			return nil // already geocoded for these coordinates — idempotent skip
		}
	}
	if photo.Lat == nil || photo.Lng == nil {
		// No GPS: record an empty processed marker so the job never retries it.
		return s.savePlace(ctx, places.Place{PhotoUID: photo.UID})
	}
	return s.geocodeAndStore(ctx, photo)
}

// alreadyCurrent reports whether photo already has a cached place computed from
// its current coordinates, in which case the geocode can be skipped. A photo with
// no place row, or one whose stored coordinates differ from the photo's (a
// coordinate edit), is not current and must be (re-)geocoded.
func (s *Service) alreadyCurrent(ctx context.Context, photo photos.Photo) (bool, error) {
	existing, err := s.places.GetPlace(ctx, photo.UID)
	if errors.Is(err, places.ErrPlaceNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("placesjob: checking existing place for %s: %w", photo.UID, err)
	}
	return sameCoord(existing.Lat, photo.Lat) && sameCoord(existing.Lng, photo.Lng), nil
}

// geocodeAndStore reverse-geocodes the photo's coordinates (after reserving a
// credit and a rate limiter token) and caches the parsed place. An exhausted
// budget, an exhausted limiter and a mapy.com outage all defer the job without
// burning a retry attempt.
func (s *Service) geocodeAndStore(ctx context.Context, photo photos.Photo) error {
	if err := s.reserveCredit(); err != nil {
		return err
	}
	result, err := s.geocoder.ReverseGeocode(ctx, *photo.Lat, *photo.Lng)
	if err != nil {
		return s.classifyGeocodeErr(ctx, photo, err)
	}
	s.meter.GeocodeCreditSpent()
	country, region, city, name := parsePlace(result)
	return s.savePlace(ctx, places.Place{
		PhotoUID:  photo.UID,
		Country:   country,
		Region:    region,
		City:      city,
		PlaceName: name,
		Lat:       photo.Lat,
		Lng:       photo.Lng,
	})
}

// reserveCredit claims what one geocode costs — first a credit from the budget,
// then a token from the rate limiter — and returns a deferral when either is
// exhausted, so the job is requeued instead of failing.
//
// The budget comes first on purpose: an empty budget defers the job until the
// budget actually refills, where the limiter's deferral is a minute, and a job
// that keeps waking into an empty budget every minute would churn the queue for
// the rest of the window. A credit reserved for a call the limiter then blocks
// is handed straight back, since nothing was spent.
func (s *Service) reserveCredit() error {
	retryAfter, ok := s.budget.Reserve()
	if !ok {
		// RetryAfter is our worker control-flow signal, not a foreign error to wrap.
		return worker.RetryAfter(retryAfter, errBudgetExhausted) //nolint:wrapcheck
	}
	if !s.limiter.Allow() {
		s.budget.Refund()
		return worker.RetryAfter(s.rateLimitDelay, errLocalRateLimited) //nolint:wrapcheck
	}
	return nil
}

// classifyGeocodeErr turns a reverse-geocode failure into the right outcome: no
// match is recorded as processed (at these coordinates, so it is not retried
// forever); an unavailable or rate-limited upstream defers the job without
// burning an attempt; anything else is an ordinary retryable error.
//
// It also settles the reserved credit. mapy.com performed no lookup when it was
// unreachable or refused the request, so that credit goes back to the budget;
// every answer it did give — including "no match" — cost a credit and is
// metered.
func (s *Service) classifyGeocodeErr(ctx context.Context, photo photos.Photo, err error) error {
	switch {
	case errors.Is(err, mapy.ErrNotFound):
		s.meter.GeocodeCreditSpent()
		return s.savePlace(ctx, places.Place{PhotoUID: photo.UID, Lat: photo.Lat, Lng: photo.Lng})
	case errors.Is(err, mapy.ErrUnavailable), errors.Is(err, mapy.ErrRateLimited):
		s.budget.Refund()
		// RetryAfter is our worker control-flow signal, not a foreign error to wrap.
		return worker.RetryAfter(s.retryDelay, err) //nolint:wrapcheck
	default:
		s.meter.GeocodeCreditSpent()
		return fmt.Errorf("placesjob: geocoding %s: %w", photo.UID, err)
	}
}

// savePlace persists p, wrapping a store failure with context.
func (s *Service) savePlace(ctx context.Context, p places.Place) error {
	if _, err := s.places.SavePlace(ctx, p); err != nil {
		return fmt.Errorf("placesjob: saving place for %s: %w", p.PhotoUID, err)
	}
	return nil
}

// BackfillPlaces enqueues a `places` job for every non-archived, geotagged photo
// that has no cached place yet, returning how many uids it scheduled. Photos that
// are already geocoded are never touched, and a photo whose job is already queued
// is a harmless no-op (the enqueuer dedupes), so the backfill is safe to run
// repeatedly.
func (s *Service) BackfillPlaces(ctx context.Context) (int, error) {
	uids, err := s.places.ListPhotosMissingPlaces(ctx, 0)
	if err != nil {
		return 0, fmt.Errorf("placesjob: listing photos missing places: %w", err)
	}
	enqueued := 0
	for _, uid := range uids {
		if err := s.enqueuer.EnqueuePlaces(ctx, uid); err != nil {
			return enqueued, fmt.Errorf("placesjob: enqueuing places for %s: %w", uid, err)
		}
		enqueued++
	}
	return enqueued, nil
}

// parsePlace extracts the country / region / city / place-name hierarchy from a
// mapy.com reverse-geocode result. The place name is the geocoded point's own
// (most specific) name; the rest come from the regionalStructure entries, matched
// by their bare type (the optional "regional." prefix is stripped). A level the
// geocoder did not supply stays empty.
func parsePlace(result *mapy.GeocodeResult) (country, region, city, placeName string) {
	placeName = result.Name
	for _, item := range result.RegionalStructure {
		switch regionalKind(item.Type) {
		case "country":
			country = item.Name
		case "region":
			region = item.Name
		case "municipality":
			city = item.Name
		}
	}
	if placeName == "" && len(result.RegionalStructure) > 0 {
		placeName = result.RegionalStructure[0].Name
	}
	return country, region, city, placeName
}

// regionalKind normalizes a mapy.com regionalStructure type ("regional.country",
// "country", ...) to its bare kind by dropping the optional "regional." prefix, so
// the parser matches whether or not mapy.com namespaces the type.
func regionalKind(t string) string {
	return strings.TrimPrefix(t, "regional.")
}

// sameCoord reports whether two optional coordinates are equal, treating two
// absent values (a photo without GPS) as equal. The stored value is exactly what
// was read from the photo, so an exact float comparison is correct here.
func sameCoord(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
