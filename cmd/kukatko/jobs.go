package main

import (
	"context"
	"log"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/jobsapi"
	"github.com/panbotka/kukatko/internal/worker"
)

// buildJobs assembles the background job subsystem: the shared queue store, the
// in-process worker (with its built-in handlers registered) that drains it, and
// the admin HTTP API exposing queue stats, listings and requeue. The worker is
// returned to the serve command to run for the process lifetime; the API mounts
// its admin-guarded routes via authAPI so the jobsapi package stays decoupled
// from auth's wiring.
func buildJobs(cfg *config.Config, db *database.DB, authAPI *auth.API) (*worker.Worker, *jobsapi.API) {
	store := jobs.NewStore(db.Pool())

	registry := worker.NewRegistry()
	worker.RegisterBuiltins(registry)

	w := worker.New(worker.Config{
		Queue:             store,
		Registry:          registry,
		Concurrency:       cfg.Worker.Count,
		PollInterval:      cfg.Worker.PollInterval,
		StaleAfter:        cfg.Worker.StaleAfter,
		StaleScanInterval: cfg.Worker.StaleScanInterval,
	})

	api := jobsapi.NewAPI(jobsapi.Config{
		Store:        store,
		RequireAdmin: authAPI.RequireAdmin,
	})
	return w, api
}

// startWorker runs w in the background, tied to ctx so it stops on shutdown. A
// non-nil return from Run (none under current semantics) is logged rather than
// crashing the process.
func startWorker(ctx context.Context, w *worker.Worker) {
	go func() {
		if err := w.Run(ctx); err != nil {
			log.Printf("background worker stopped: %v", err)
		}
	}()
}
