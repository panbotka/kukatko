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
// Every collaborator — the photo store, the place cache, the geocoder and the
// rate limiter — is an interface so the Service unit-tests with fakes and no
// network or database.
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
		retryDelay:     retryDelay,
		rateLimitDelay: rateLimitDelay,
	}
}

// jobPayload is the JSON shape of a `places` job's payload.
type jobPayload struct {
	PhotoUID string `json:"photo_uid"`
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
	photo, err := s.photos.GetByUID(ctx, photoUID)
	if err != nil {
		return fmt.Errorf("placesjob: loading photo %s: %w", photoUID, err)
	}
	current, err := s.alreadyCurrent(ctx, photo)
	if err != nil {
		return err
	}
	if current {
		return nil // already geocoded for these coordinates — idempotent skip
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

// geocodeAndStore reverse-geocodes the photo's coordinates (after acquiring a rate
// limiter token) and caches the parsed place. Limiter exhaustion and a mapy.com
// outage both defer the job without burning a retry attempt.
func (s *Service) geocodeAndStore(ctx context.Context, photo photos.Photo) error {
	if !s.limiter.Allow() {
		// Own credit budget is spent for now; try again shortly. RetryAfter is our
		// worker control-flow signal, not a foreign error to annotate.
		return worker.RetryAfter(s.rateLimitDelay, errLocalRateLimited) //nolint:wrapcheck
	}
	result, err := s.geocoder.ReverseGeocode(ctx, *photo.Lat, *photo.Lng)
	if err != nil {
		return s.classifyGeocodeErr(ctx, photo, err)
	}
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

// classifyGeocodeErr turns a reverse-geocode failure into the right outcome: no
// match is recorded as processed (at these coordinates, so it is not retried
// forever); an unavailable or rate-limited upstream defers the job without
// burning an attempt; anything else is an ordinary retryable error.
func (s *Service) classifyGeocodeErr(ctx context.Context, photo photos.Photo, err error) error {
	switch {
	case errors.Is(err, mapy.ErrNotFound):
		return s.savePlace(ctx, places.Place{PhotoUID: photo.UID, Lat: photo.Lat, Lng: photo.Lng})
	case errors.Is(err, mapy.ErrUnavailable), errors.Is(err, mapy.ErrRateLimited):
		// RetryAfter is our worker control-flow signal, not a foreign error to wrap.
		return worker.RetryAfter(s.retryDelay, err) //nolint:wrapcheck
	default:
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
