package main

import (
	"fmt"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/mapsapi"
	"github.com/panbotka/kukatko/internal/mapy"
	"github.com/panbotka/kukatko/internal/photos"
)

// buildMapsAPI assembles the maps subsystem: the mapy.com proxy client (built
// only when an API key is configured, so the key stays server-side) backing the
// tile and reverse-geocode endpoints, and the photo store backing the GeoJSON
// feed. Read access reuses the auth subsystem's RequireAuth guard. When no key is
// configured the tile and reverse-geocode endpoints answer 503 while the GeoJSON
// feed (which needs no key) keeps working.
func buildMapsAPI(cfg *config.Config, db *database.DB, authAPI *auth.API) (*mapsapi.API, error) {
	var tiles mapsapi.TileFetcher
	var geocoder mapsapi.Geocoder
	if cfg.Maps.MapyAPIKey != "" {
		client, err := mapy.New(mapy.Config{
			BaseURL: cfg.Maps.BaseURL,
			APIKey:  cfg.Maps.MapyAPIKey,
		})
		if err != nil {
			return nil, fmt.Errorf("initialising mapy.com client: %w", err)
		}
		tiles, geocoder = client, client
	}

	return mapsapi.NewAPI(mapsapi.Config{
		Tiles:       tiles,
		Geocoder:    geocoder,
		Photos:      photos.NewStore(db.Pool()),
		RequireAuth: authAPI.RequireAuth,
	}), nil
}
