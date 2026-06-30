// Package places is the database access layer for the per-photo reverse-geocoded
// place cache. A photo's GPS coordinate is resolved (by the background `places`
// job) into a country / region / city / place-name hierarchy and stored in the
// photo_places side table so the library can later be browsed and filtered by
// location without re-hitting the rate-limited geocoder.
//
// The Store owns no connection; it borrows the shared pgx pool. It exposes the
// three operations the job needs: read a photo's cached place (to decide whether
// a re-geocode is required), upsert a place (the geocode result, or an empty
// "processed" marker for a photo without GPS), and list geotagged photos that
// still lack place data (the backfill source).
package places

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrPlaceNotFound is returned by GetPlace when a photo has no place row yet.
var ErrPlaceNotFound = errors.New("places: place not found")

// Place is one photo_places row: the cached place hierarchy for a photo plus the
// coordinates the geocode was computed from. Lat and Lng are nil for a photo
// without GPS, whose row exists only to mark it processed.
type Place struct {
	PhotoUID   string    `json:"photo_uid"`
	Country    string    `json:"country"`
	Region     string    `json:"region"`
	City       string    `json:"city"`
	PlaceName  string    `json:"place_name"`
	Lat        *float64  `json:"lat,omitempty"`
	Lng        *float64  `json:"lng,omitempty"`
	GeocodedAt time.Time `json:"geocoded_at"`
}

// Store reads and writes the photo_places cache over a shared pgx pool.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// getPlaceSQL fetches one photo's cached place row.
const getPlaceSQL = `
SELECT photo_uid, country, region, city, place_name, lat, lng, geocoded_at
FROM photo_places
WHERE photo_uid = $1`

// GetPlace returns the cached place for photoUID, or ErrPlaceNotFound when the
// photo has not been geocoded yet.
func (s *Store) GetPlace(ctx context.Context, photoUID string) (Place, error) {
	var p Place
	err := s.pool.QueryRow(ctx, getPlaceSQL, photoUID).Scan(
		&p.PhotoUID, &p.Country, &p.Region, &p.City, &p.PlaceName, &p.Lat, &p.Lng, &p.GeocodedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Place{}, ErrPlaceNotFound
	}
	if err != nil {
		return Place{}, fmt.Errorf("places: getting place for %s: %w", photoUID, err)
	}
	return p, nil
}

// savePlaceSQL upserts a photo's place row, stamping geocoded_at to now on every
// write so the timestamp reflects the most recent geocode.
const savePlaceSQL = `
INSERT INTO photo_places (photo_uid, country, region, city, place_name, lat, lng, geocoded_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, now())
ON CONFLICT (photo_uid) DO UPDATE SET
    country     = EXCLUDED.country,
    region      = EXCLUDED.region,
    city        = EXCLUDED.city,
    place_name  = EXCLUDED.place_name,
    lat         = EXCLUDED.lat,
    lng         = EXCLUDED.lng,
    geocoded_at = now()
RETURNING photo_uid, country, region, city, place_name, lat, lng, geocoded_at`

// SavePlace inserts or replaces the place row for p.PhotoUID and returns the
// persisted record (with the database-assigned geocoded_at). It is idempotent on
// the photo_uid primary key, so re-running the geocode for a photo overwrites the
// previous answer rather than failing.
func (s *Store) SavePlace(ctx context.Context, p Place) (Place, error) {
	var saved Place
	err := s.pool.QueryRow(ctx, savePlaceSQL,
		p.PhotoUID, p.Country, p.Region, p.City, p.PlaceName, p.Lat, p.Lng).Scan(
		&saved.PhotoUID, &saved.Country, &saved.Region, &saved.City, &saved.PlaceName,
		&saved.Lat, &saved.Lng, &saved.GeocodedAt)
	if err != nil {
		return Place{}, fmt.Errorf("places: saving place for %s: %w", p.PhotoUID, err)
	}
	return saved, nil
}

// listMissingPlacesSQL selects the uids of non-archived, geotagged photos that
// have no photo_places row yet, newest first. The %s placeholder receives a LIMIT
// clause only when a positive limit is requested.
const listMissingPlacesSQL = `
SELECT p.uid
FROM photos p
LEFT JOIN photo_places pp ON pp.photo_uid = p.uid
WHERE pp.photo_uid IS NULL
  AND p.archived_at IS NULL
  AND p.lat IS NOT NULL
  AND p.lng IS NOT NULL
ORDER BY p.created_at DESC, p.uid DESC%s`

// ListPhotosMissingPlaces returns the uids of non-archived photos that carry GPS
// coordinates but have no cached place yet, newest first. A positive limit caps
// the result; a non-positive limit returns every missing photo. It backs the
// place backfill, which enqueues a `places` job per returned uid.
func (s *Store) ListPhotosMissingPlaces(ctx context.Context, limit int) ([]string, error) {
	query := fmt.Sprintf(listMissingPlacesSQL, "")
	var args []any
	if limit > 0 {
		query = fmt.Sprintf(listMissingPlacesSQL, "\nLIMIT $1")
		args = []any{limit}
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("places: listing photos missing places: %w", err)
	}
	defer rows.Close()

	var uids []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("places: scanning photo uid: %w", err)
		}
		uids = append(uids, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("places: iterating photo uids: %w", err)
	}
	return uids, nil
}
