package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/sweep"
	"github.com/panbotka/kukatko/internal/sweepapi"
)

// buildSweepAPI assembles the recognition-sweep endpoint over the shared pool: the
// sweep service iterates the named subjects (people store) and reuses the same
// candidate service the per-subject search uses as its finder, so the two never
// drift. Concurrency and the subject cap come from cfg.Sweep; a nil logger falls back
// to slog.Default(), which obs.Setup has already pointed at the app's JSON logger.
// The write guard is supplied via authAPI so sweepapi stays decoupled from auth.
func buildSweepAPI(
	cfg *config.Config, db *database.DB, authAPI *auth.API, mediaStore storage.Storage,
) *sweepapi.API {
	svc := sweep.New(sweep.Config{
		Subjects:    people.NewStore(db.Pool()),
		Finder:      buildCandidatesService(cfg, db, mediaStore),
		Concurrency: cfg.Sweep.Concurrency,
		MaxSubjects: cfg.Sweep.MaxSubjects,
	})
	return sweepapi.NewAPI(sweepapi.Config{
		Service:      svc,
		RequireWrite: authAPI.RequireWrite,
	})
}
