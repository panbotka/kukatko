package main

import (
	"fmt"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/backup"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/duplicates"
	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mapy"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/placesjob"
	"github.com/panbotka/kukatko/internal/system"
	"github.com/panbotka/kukatko/internal/systemapi"
	"github.com/panbotka/kukatko/internal/vectors"
)

// buildSystemAPI assembles the system API: the maintainer-only status dashboard
// and the library statistics every signed-in user may read. It builds a fresh,
// stateless embeddings client (only used for its cheap Healthy probe) and reuses
// the shared pool for the job-queue, import-run and library-counts stores; the
// optional backup service drives the backup section (nil-safe). Both route
// guards are supplied via authAPI (systemapi stays decoupled from auth's
// wiring): maintainer for the operations view, plain authentication for the
// counts.
//
// It returns the service alongside the API so /metrics can export the same
// aggregation the dashboard reads instead of counting the catalogue twice.
func buildSystemAPI(
	cfg *config.Config, db *database.DB, authAPI *auth.API, backupSvc *backup.Service,
	mapsHealth *mapy.Health, geocodeBudget *placesjob.WindowBudget,
) (*systemapi.API, *system.Service, error) {
	client, err := embedding.New(embeddingClientConfig(cfg))
	if err != nil {
		return nil, nil, fmt.Errorf("initialising embedding client: %w", err)
	}

	// A nil *backup.Service must be passed as a nil interface, not a non-nil
	// interface wrapping a nil pointer, so the status section reports
	// not-configured rather than panicking. The same holds for the maps health
	// tracker and the geocode credit budget, both nil when no mapy.com key is
	// configured.
	var backupReporter system.BackupReporter
	if backupSvc != nil {
		backupReporter = backupSvc
	}
	var mapsReporter system.MapsReporter
	if mapsHealth != nil {
		mapsReporter = mapsHealth
	}
	var geocodeReporter system.GeocodeReporter
	if geocodeBudget != nil {
		geocodeReporter = geocodeBudget
	}

	// One store answers all three library aggregations (the counts, the chart
	// series and the dashboard), so they can never be built from different
	// catalogues.
	pool := db.Pool()
	libraryStore := system.NewStore(pool)
	svc := system.New(system.Config{
		DB:            db,
		Embeddings:    client,
		EmbeddingURL:  cfg.Embedding.URL,
		Jobs:          jobs.NewStore(pool),
		Backup:        backupReporter,
		Maps:          mapsReporter,
		Geocode:       geocodeReporter,
		Imports:       importer.NewStore(pool),
		Library:       libraryStore,
		Charts:        libraryStore,
		Dashboard:     libraryStore,
		Duplicates:    buildSystemDuplicatesOrNil(cfg, db),
		OriginalsPath: cfg.Storage.OriginalsPath,
		CachePath:     cfg.Storage.CachePath,
	})
	api := systemapi.NewAPI(systemapi.Config{
		Service:           svc,
		RequireMaintainer: authAPI.RequireMaintainer,
		RequireAuth:       authAPI.RequireAuth,
	})
	return api, svc, nil
}

// buildSystemDuplicatesOrNil returns the near-duplicate detector behind the
// dashboard's duplicates tile, or a nil interface when duplicate detection is
// switched off in config — the same switch that makes GET /duplicates answer
// 503, so the tile and the page can never disagree about whether duplicates are
// detected at all.
//
// It is built from the same config as buildDuplicatesAPI's detector rather than
// shared with it (the two APIs are assembled in different phases of serve), the
// way the review game's detector already is. The dashboard never scans on a
// request: internal/system runs this in the background and reports when it last
// finished.
func buildSystemDuplicatesOrNil(cfg *config.Config, db *database.DB) system.DuplicateCounter {
	if !cfg.Duplicate.Enabled {
		return nil
	}
	photoStore := photos.NewStore(db.Pool())
	return duplicates.New(duplicates.Config{
		Photos:           photoStore,
		Phashes:          photoStore,
		Embeddings:       vectors.NewStore(db.Pool()),
		Feedback:         feedback.NewStore(db.Pool()),
		PhashMaxDiff:     cfg.Duplicate.PhashMaxDiff,
		EmbeddingMaxDist: cfg.Duplicate.EmbeddingMaxDist,
	})
}
