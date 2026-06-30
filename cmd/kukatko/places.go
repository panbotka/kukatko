package main

import (
	"fmt"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mapy"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
	"github.com/panbotka/kukatko/internal/placesjob"
)

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
		BaseURL: cfg.Maps.BaseURL,
		APIKey:  cfg.Maps.MapyAPIKey,
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
