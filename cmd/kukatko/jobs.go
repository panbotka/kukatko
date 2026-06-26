package main

import (
	"context"
	"log"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/embedjob"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/jobsapi"
	"github.com/panbotka/kukatko/internal/processapi"
	"github.com/panbotka/kukatko/internal/worker"
)

// buildJobs assembles the background job subsystem: the in-process worker (with
// the built-in handlers plus the image_embed handler registered) that drains the
// shared queue store, the admin HTTP API exposing queue stats/listings/requeue,
// and the admin processing API (embedding backfill). The worker is returned to
// the serve command to run for the process lifetime; both APIs mount their
// admin-guarded routes via authAPI so the api packages stay decoupled from
// auth's wiring.
func buildJobs(
	cfg *config.Config, store *jobs.Store, authAPI *auth.API, embedSvc *embedjob.Service,
) (*worker.Worker, *jobsapi.API, *processapi.API) {
	registry := worker.NewRegistry()
	worker.RegisterBuiltins(registry)
	registry.Register(jobs.TypeImageEmbed, embedSvc.Handle)

	w := worker.New(worker.Config{
		Queue:             store,
		Registry:          registry,
		Concurrency:       cfg.Worker.Count,
		PollInterval:      cfg.Worker.PollInterval,
		StaleAfter:        cfg.Worker.StaleAfter,
		StaleScanInterval: cfg.Worker.StaleScanInterval,
	})

	jobAPI := jobsapi.NewAPI(jobsapi.Config{
		Store:        store,
		RequireAdmin: authAPI.RequireAdmin,
	})
	procAPI := processapi.NewAPI(processapi.Config{
		Backfiller:   embedSvc,
		RequireAdmin: authAPI.RequireAdmin,
	})
	return w, jobAPI, procAPI
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
