package main

import (
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/geoestimate"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/processapi"
)

// buildGeoEstimateServiceOrNil builds the missing-location estimator from
// config, returning nil when the feature's master switch is off so
// /process/locations degrades to 503 rather than quietly estimating nothing. The
// service borrows a fresh photos.Store over the shared pool.
//
// The enqueuer is passed through so each new estimate schedules its reverse
// geocode and reaches the places hierarchy. It may be nil, in which case
// estimates are stored but not geocoded.
func buildGeoEstimateServiceOrNil(
	cfg *config.Config, db *database.DB, enqueuer *jobs.Enqueuer,
) *geoestimate.Service {
	if !cfg.LocationEstimate.Enabled {
		return nil
	}
	return geoestimate.New(geoestimate.Config{
		Store:        photos.NewStore(db.Pool()),
		Enqueuer:     enqueuer,
		Window:       cfg.LocationEstimate.Window,
		RadiusMeters: cfg.LocationEstimate.RadiusMeters,
	})
}

// locationEstimatorOrNil returns the estimator as a processapi.LocationEstimator,
// or a nil interface (not a typed-nil pointer, so processapi's == nil check fires
// and disables /process/locations) when the feature is off.
func locationEstimatorOrNil(
	cfg *config.Config, db *database.DB, enqueuer *jobs.Enqueuer,
) processapi.LocationEstimator {
	if s := buildGeoEstimateServiceOrNil(cfg, db, enqueuer); s != nil {
		return s
	}
	return nil
}
