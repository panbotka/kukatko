package main

import (
	"fmt"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/backup"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mapy"
	"github.com/panbotka/kukatko/internal/system"
	"github.com/panbotka/kukatko/internal/systemapi"
)

// buildSystemAPI assembles the admin-only system-status API. It builds a fresh,
// stateless embeddings client (only used for its cheap Healthy probe) and reuses
// the shared pool for the job-queue and import-run stores; the optional backup
// service drives the backup section (nil-safe). The admin guard is supplied via
// authAPI so systemapi stays decoupled from auth's wiring.
func buildSystemAPI(
	cfg *config.Config, db *database.DB, authAPI *auth.API, backupSvc *backup.Service,
	mapsHealth *mapy.Health,
) (*systemapi.API, error) {
	client, err := embedding.New(embedding.Config{
		BaseURL:  cfg.Embedding.URL,
		ImageDim: cfg.Embedding.ImageDim,
		FaceDim:  cfg.Embedding.FaceDim,
	})
	if err != nil {
		return nil, fmt.Errorf("initialising embedding client: %w", err)
	}

	// A nil *backup.Service must be passed as a nil interface, not a non-nil
	// interface wrapping a nil pointer, so the status section reports
	// not-configured rather than panicking. The same holds for the maps health
	// tracker, which is nil when no mapy.com key is configured.
	var backupReporter system.BackupReporter
	if backupSvc != nil {
		backupReporter = backupSvc
	}
	var mapsReporter system.MapsReporter
	if mapsHealth != nil {
		mapsReporter = mapsHealth
	}

	pool := db.Pool()
	svc := system.New(system.Config{
		DB:            db,
		Embeddings:    client,
		EmbeddingURL:  cfg.Embedding.URL,
		Jobs:          jobs.NewStore(pool),
		Backup:        backupReporter,
		Maps:          mapsReporter,
		Imports:       importer.NewStore(pool),
		OriginalsPath: cfg.Storage.OriginalsPath,
		CachePath:     cfg.Storage.CachePath,
	})
	return systemapi.NewAPI(systemapi.Config{
		Service:      svc,
		RequireAdmin: authAPI.RequireAdmin,
	}), nil
}
