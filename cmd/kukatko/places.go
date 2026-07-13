package main

import (
	"fmt"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mapy"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
	"github.com/panbotka/kukatko/internal/placesapi"
	"github.com/panbotka/kukatko/internal/placesjob"
)

// buildPlacesAPI assembles the places browse HTTP API over the shared pool: a
// signed-in user listing the country/city place hierarchy (with per-place photo
// counts) of the non-archived library and drilling into a single country. The
// read guard is supplied via authAPI so placesapi stays decoupled from auth's
// wiring; the aggregation runs over the photos store, which joins the
// photo_places cache.
func buildPlacesAPI(db *database.DB, authAPI *auth.API) *placesapi.API {
	return placesapi.NewAPI(placesapi.Config{
		Store:       photos.NewStore(db.Pool()),
		RequireAuth: authAPI.RequireAuth,
	})
}

// buildPlacesServiceOrNil assembles the reverse-geocode (places) job service when
// a mapy.com API key is configured, returning (nil, nil) otherwise so the `places`
// handler is not registered and the /process/places endpoint answers 503. The
// mapy.com client is built only when the key is present, keeping the key
// server-side. The geocode rate limiter caps how often the job reaches mapy.com,
// protecting the monthly credit budget.
func buildPlacesServiceOrNil(
	cfg *config.Config, db *database.DB, enqueuer *jobs.Enqueuer,
) (*placesjob.Service, error) {
	if cfg.Maps.MapyAPIKey == "" {
		return nil, nil //nolint:nilnil // (nil, nil) is the documented "not configured" signal.
	}
	client, err := mapy.New(mapy.Config{
		BaseURL:   cfg.Maps.BaseURL,
		APIKey:    cfg.Maps.MapyAPIKey,
		UserAgent: cfg.Maps.UserAgent,
	})
	if err != nil {
		return nil, fmt.Errorf("initialising mapy.com client for places: %w", err)
	}
	return placesjob.New(placesjob.Config{
		Photos:   photos.NewStore(db.Pool()),
		Places:   places.NewStore(db.Pool()),
		Geocoder: client,
		Enqueuer: enqueuer,
		Limiter:  placesjob.NewTokenBucket(cfg.Maps.GeocodeRatePerSec, cfg.Maps.GeocodeBurst),
	}), nil
}
