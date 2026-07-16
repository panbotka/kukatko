package main

import (
	"context"
	"log"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/cluster"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/embedjob"
	"github.com/panbotka/kukatko/internal/facejob"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/jobsapi"
	"github.com/panbotka/kukatko/internal/maintenanceapi"
	"github.com/panbotka/kukatko/internal/metajob"
	"github.com/panbotka/kukatko/internal/metrics"
	"github.com/panbotka/kukatko/internal/placesjob"
	"github.com/panbotka/kukatko/internal/ppimport"
	"github.com/panbotka/kukatko/internal/processapi"
	"github.com/panbotka/kukatko/internal/sidecarjob"
	"github.com/panbotka/kukatko/internal/thumbjob"
	"github.com/panbotka/kukatko/internal/worker"
)

// buildJobs assembles the background job subsystem: the in-process worker (with
// the built-in handlers plus the image_embed and face_detect handlers registered)
// that drains the shared queue store, the admin HTTP API exposing queue
// stats/listings/requeue, and the admin processing API (embedding, face and
// thumbnail backfills plus the face-clustering trigger). The worker is returned
// to the serve command to run for the process lifetime; both APIs mount their
// admin-guarded routes via authAPI so the api packages stay decoupled from
// auth's wiring. The psMigrate handler (nil when photo-sorter is not configured)
// registers the ps_migrate job. The places handler (nil when no mapy.com key is
// configured) registers the `places` reverse-geocode job and backs the place
// backfill. It also builds the thumbnail service (regenerating thumbnails/pHashes,
// and backing the missing-thumbnail backfill), the metadata service (re-reading a
// photo's original into the IPTC/XMP and file-technical columns, and backing the
// metadata backfill), the metadata sidecar export service (nil when the export is
// switched off; it registers the `sidecar` job that writes each photo's curation
// to a YAML file in storage and backs the sidecar backfill) and the
// library-maintenance service/API, since all are part of the job subsystem; a
// build failure for any of them is returned as an error.
func buildJobs(
	cfg *config.Config, db *database.DB, store *jobs.Store, authAPI *auth.API, enqueuer *jobs.Enqueuer,
	embedSvc *embedjob.Service, faceSvc *facejob.Service, clusterSvc *cluster.Service,
	importSvc *ppimport.Service, psMigrate worker.HandlerFunc, reg *metrics.Registry,
) (*worker.Worker, *jobsapi.API, *processapi.API, *maintenanceapi.API, error) {
	thumbSvc, maintenanceSvc, err := buildMaintenanceAndThumb(cfg, db, enqueuer, embedSvc, faceSvc, reg)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	placesSvc, err := buildPlacesServiceOrNil(cfg, db, enqueuer)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	metaSvc, err := buildMetaService(cfg, db, enqueuer)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	sidecarSvc, err := buildSidecarServiceOrNil(cfg, db, enqueuer)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	registry := buildRegistry(registryServices{
		embed: embedSvc, face: faceSvc, thumb: thumbSvc, meta: metaSvc,
		imp: importSvc, psMigrate: psMigrate, places: placesSvc, sidecar: sidecarSvc,
	})

	w := worker.New(worker.Config{
		Queue:             store,
		Registry:          registry,
		Concurrency:       cfg.Worker.Count,
		PollInterval:      cfg.Worker.PollInterval,
		StaleAfter:        cfg.Worker.StaleAfter,
		StaleScanInterval: cfg.Worker.StaleScanInterval,
		Metrics:           workerObserver(reg),
	})

	jobAPI := jobsapi.NewAPI(jobsapi.Config{Store: store, RequireAdmin: authAPI.RequireAdmin})
	// Pass the places backfiller as a nil interface (not a typed nil pointer) when
	// it is not configured, so processapi's nil check disables /process/places.
	var placesBF processapi.PlacesBackfiller
	if placesSvc != nil {
		placesBF = placesSvc
	}
	procAPI := processapi.NewAPI(processapi.Config{
		Backfiller:          embedSvc,
		FaceBackfiller:      faceSvc,
		Reclusterer:         clusterSvc,
		PlacesBackfiller:    placesBF,
		ThumbnailBackfiller: thumbSvc,
		MetadataBackfiller:  metaSvc,
		// A nil interface (not a typed-nil pointer) disables /process/sidecars when
		// the metadata sidecar export is off.
		SidecarBackfiller: sidecarBackfillerOrNil(sidecarSvc),
		// A nil interface (not a typed-nil pointer) disables /process/stacks when
		// the stacking feature is off.
		StacksDetector: stacksDetectorOrNil(cfg, db),
		// Likewise a nil interface disables /process/locations when location
		// estimation is switched off.
		LocationEstimator: locationEstimatorOrNil(cfg, db, enqueuer),
		RequireAdmin:      authAPI.RequireAdmin,
	})
	return w, jobAPI, procAPI, buildMaintenanceAPI(maintenanceSvc, authAPI), nil
}

// registryServices bundles the job handlers buildRegistry wires, so the
// registration list is one parameter rather than eight.
type registryServices struct {
	embed     *embedjob.Service
	face      *facejob.Service
	thumb     *thumbjob.Service
	meta      *metajob.Service
	imp       *ppimport.Service
	psMigrate worker.HandlerFunc
	places    *placesjob.Service
	sidecar   *sidecarjob.Service
}

// buildRegistry returns the worker registry with every configured handler
// registered. The always-available handlers register unconditionally; the
// config-gated ones (import, photo-sorter migration, places, sidecar) register
// only when their service was built, because an unregistered type is never
// claimed — so a job of a type with no handler would sit queued forever.
func buildRegistry(svc registryServices) *worker.Registry {
	registry := worker.NewRegistry()
	worker.RegisterBuiltins(registry)
	registry.Register(jobs.TypeImageEmbed, svc.embed.Handle)
	registry.Register(jobs.TypeFaceDetect, svc.face.Handle)
	registry.Register(jobs.TypeThumbnail, svc.thumb.Handle)
	registry.Register(jobs.TypeMetadata, svc.meta.Handle)
	if svc.imp != nil {
		registry.Register(jobs.TypePPImport, svc.imp.Handle)
	}
	if svc.psMigrate != nil {
		registry.Register(jobs.TypePSMigrate, svc.psMigrate)
	}
	if svc.places != nil {
		registry.Register(jobs.TypePlaces, svc.places.Handle)
	}
	if svc.sidecar != nil {
		registry.Register(jobs.TypeSidecar, svc.sidecar.Handle)
	}
	return registry
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
