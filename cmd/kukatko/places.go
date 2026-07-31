package main

import (
	"fmt"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mapy"
	"github.com/panbotka/kukatko/internal/metrics"
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

// newGeocodeBudget builds the reverse-geocode credit budget, or nil when no
// mapy.com key is configured (nothing geocodes, so nothing is spent). One
// instance is shared by the `places` job that spends the credits, the system
// status that reports the spend and the metrics collector that exports it —
// mirroring the maps health tracker.
func newGeocodeBudget(cfg *config.Config) *placesjob.WindowBudget {
	if cfg.Maps.MapyAPIKey == "" {
		return nil
	}
	return placesjob.NewWindowBudget(placesjob.BudgetConfig{
		Limit:  cfg.Maps.GeocodeBudget,
		Window: cfg.Maps.GeocodeBudgetWindow,
	})
}

// geocodeBudgetMetrics exports the credit budget through reg, a no-op when
// metrics are disabled, no budget exists or the cap is switched off (an
// unenforced budget would otherwise scrape as a permanent "0 remaining"). The
// gauge is sampled at scrape time, so it follows the window rolling over even
// while no job runs.
func geocodeBudgetMetrics(reg *metrics.Registry, budget *placesjob.WindowBudget) {
	if reg == nil || budget == nil || !budget.Snapshot().Enabled {
		return
	}
	reg.RegisterGeocodeBudget(func() (int, int) {
		snapshot := budget.Snapshot()
		return snapshot.Remaining, snapshot.Limit
	})
}

// buildPlacesServiceOrNil assembles the reverse-geocode (places) job service when
// a mapy.com API key is configured, returning (nil, nil) otherwise so the `places`
// handler is not registered and the /process/places endpoint answers 503. The
// mapy.com client is built only when the key is present, keeping the key
// server-side.
//
// Credits are bounded twice: the rate limiter caps how often the job reaches
// mapy.com, and the shared budget caps how many geocodes a period may spend at
// all, so a full-library import cannot drain the quota unattended. What it does
// spend is counted into the metrics registry (nil when metrics are off).
func buildPlacesServiceOrNil(
	cfg *config.Config, db *database.DB, enqueuer *jobs.Enqueuer,
	budget *placesjob.WindowBudget, reg *metrics.Registry,
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
		Budget:   geocodeBudgetOrNil(budget),
		Meter:    creditMeter(reg),
	}), nil
}

// geocodeBudgetOrNil returns budget as a placesjob.CreditBudget, or a nil
// interface when none was built, so the service falls back to its unlimited
// default instead of holding a typed nil.
func geocodeBudgetOrNil(budget *placesjob.WindowBudget) placesjob.CreditBudget {
	if budget == nil {
		return nil
	}
	return budget
}
