package main

import (
	"fmt"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/mapsapi"
	"github.com/panbotka/kukatko/internal/mapy"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/ratelimit"
)

// newMapsHealth returns the shared tracker of the mapy.com upstream's health, or
// nil when no API key is configured (the map backend is then simply absent, not
// degraded). The same tracker is handed to the maps API, which records every
// upstream outcome onto it, and to the system-status API, which reports it — so a
// rejected key surfaces on the admin dashboard, not just as a grey map.
func newMapsHealth(cfg *config.Config) *mapy.Health {
	if cfg.Maps.MapyAPIKey == "" {
		return nil
	}
	return mapy.NewHealth()
}

// buildMapsAPI assembles the maps subsystem: the mapy.com proxy client (built
// only when an API key is configured, so the key stays server-side) backing the
// tile, reverse-geocode and place-search endpoints, and the photo store backing
// the GeoJSON feed. Read access reuses the auth subsystem's RequireAuth guard.
// When no key is configured those three endpoints answer 503 while the GeoJSON
// feed (which needs no key) keeps working. Proxied tiles and geocode answers are
// cached server-side so a re-browsed area or a repeated place search costs no
// mapy.com credits, and every upstream outcome is recorded on the shared health
// tracker.
func buildMapsAPI(
	cfg *config.Config, db *database.DB, authAPI *auth.API, health *mapy.Health,
) (*mapsapi.API, error) {
	var tiles mapsapi.TileFetcher
	var geocoder mapsapi.Geocoder
	var places mapsapi.PlaceSearcher
	if cfg.Maps.MapyAPIKey != "" {
		client, err := mapy.New(mapy.Config{
			BaseURL:   cfg.Maps.BaseURL,
			APIKey:    cfg.Maps.MapyAPIKey,
			UserAgent: cfg.Maps.UserAgent,
		})
		if err != nil {
			return nil, fmt.Errorf("initialising mapy.com client: %w", err)
		}
		tiles, geocoder, places = client, client, client
	}

	tileLimit := ratelimit.New(cfg.RateLimit.Tiles.RatePerSec, cfg.RateLimit.Tiles.Burst)
	return mapsapi.NewAPI(mapsapi.Config{
		Tiles:          tiles,
		Geocoder:       geocoder,
		Places:         places,
		Photos:         photos.NewStore(db.Pool()),
		Health:         health,
		RequireAuth:    authAPI.RequireAuth,
		TileRateLimit:  tileLimit.Middleware,
		TileCacheBytes: cfg.Maps.TileCacheBytes,
		TileCacheTTL:   cfg.Maps.TileCacheTTL,
	}), nil
}
