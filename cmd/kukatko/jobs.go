package main

import (
	"context"
	"log"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/clusterjob"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/embedjob"
	"github.com/panbotka/kukatko/internal/facejob"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/jobsapi"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/maintenance"
	"github.com/panbotka/kukatko/internal/maintenanceapi"
	"github.com/panbotka/kukatko/internal/metajob"
	"github.com/panbotka/kukatko/internal/metrics"
	"github.com/panbotka/kukatko/internal/namelessjob"
	"github.com/panbotka/kukatko/internal/ocrjob"
	"github.com/panbotka/kukatko/internal/placesjob"
	"github.com/panbotka/kukatko/internal/processapi"
	"github.com/panbotka/kukatko/internal/sidecarjob"
	"github.com/panbotka/kukatko/internal/storyboardjob"
	"github.com/panbotka/kukatko/internal/thumbjob"
	"github.com/panbotka/kukatko/internal/worker"
)

// buildJobs assembles the background job subsystem: the in-process worker (with
// the built-in handlers plus the image_embed and face_detect handlers registered)
// that drains the shared queue store, the maintainer-only HTTP API exposing queue
// stats/listings/requeue, and the maintainer-only processing API (embedding, face
// and thumbnail backfills plus the face-clustering trigger). The worker is
// returned to the serve command to run for the process lifetime; both APIs are
// operations surfaces, so they mount their maintainer-guarded routes via authAPI
// (the api packages stay decoupled from auth's wiring). The places handler (nil when no mapy.com key is
// configured) registers the `places` reverse-geocode job and backs the place
// backfill; it spends its metered mapy.com credits against the shared
// geocodeBudget the caller also hands to the system status. It also builds the
// thumbnail service (regenerating thumbnails/pHashes,
// and backing the missing-thumbnail backfill), the metadata service (re-reading a
// photo's original into the IPTC/XMP and file-technical columns, and backing the
// metadata backfill), the metadata sidecar export service (nil when the export is
// switched off; it registers the `sidecar` job that writes each photo's curation
// to a YAML file in storage and backs the sidecar backfill), the text-recognition
// service (nil when OCR is off; it registers the `ocr` job that reads what a
// photo's signs say and backs the OCR backfill, reusing embedClient because it is
// the same sidecar on the same box), the mail service (nil when mail is off; it
// registers the `mail_send` job that renders a queued message and hands it to the
// SMTP server) and the library-maintenance service/API, since all are part of the
// job subsystem; a build failure for any of them is
// returned as an error.
func buildJobs(
	cfg *config.Config, db *database.DB, store *jobs.Store, authAPI *auth.API, enqueuer *jobs.Enqueuer,
	embedSvc *embedjob.Service, faceSvc *facejob.Service, clusterSvc *clusterjob.Service,
	storyboardSvc *storyboardjob.Service, embedClient embedding.Client, reg *metrics.Registry,
	placesSvc *placesjob.Service,
) (*worker.Worker, *jobsapi.API, *processapi.API, *maintenanceapi.API, error) {
	svcs, maintenanceSvc, err := buildJobServices(jobServiceDeps{
		cfg: cfg, db: db, store: store, enqueuer: enqueuer, embed: embedSvc, face: faceSvc,
		storyboard: storyboardSvc, embedClient: embedClient, places: placesSvc, reg: reg,
		cluster: clusterSvc,
	})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	registry := buildRegistry(svcs)

	w := worker.New(worker.Config{
		Queue:             store,
		Registry:          registry,
		Concurrency:       cfg.Worker.Count,
		TypeConcurrency:   cfg.Worker.TypeCount,
		PollInterval:      cfg.Worker.PollInterval,
		StaleAfter:        cfg.Worker.StaleAfter,
		StaleScanInterval: cfg.Worker.StaleScanInterval,
		Metrics:           workerObserver(reg),
	})

	jobAPI := jobsapi.NewAPI(jobsapi.Config{Store: store, RequireMaintainer: authAPI.RequireMaintainer})
	// Pass the places backfiller as a nil interface (not a typed nil pointer) when
	// it is not configured, so processapi's nil check disables /process/places.
	var placesBF processapi.PlacesBackfiller
	if svcs.places != nil {
		placesBF = svcs.places
	}
	procAPI := processapi.NewAPI(processapi.Config{
		Backfiller:          embedSvc,
		FaceBackfiller:      faceSvc,
		Reclusterer:         clusterSvc,
		PlacesBackfiller:    placesBF,
		ThumbnailBackfiller: svcs.thumb,
		// The same service: the thumbnail job is what computes a photo's blurred
		// placeholder, so the two backfills schedule the same job and differ only in
		// which photos they pick.
		BlurhashBackfiller: svcs.thumb,
		MetadataBackfiller: svcs.meta,
		// A nil interface (not a typed-nil pointer) disables /process/sidecars when
		// the metadata sidecar export is off.
		SidecarBackfiller: sidecarBackfillerOrNil(svcs.sidecar),
		// Likewise a nil interface disables /process/ocr when text recognition is off.
		OCRBackfiller: ocrBackfillerOrNil(svcs.ocr),
		// A nil interface (not a typed-nil pointer) disables /process/stacks when
		// the stacking feature is off.
		StacksDetector: stacksDetectorOrNil(cfg, db),
		// Likewise a nil interface disables /process/locations when location
		// estimation is switched off.
		LocationEstimator: locationEstimatorOrNil(cfg, db, enqueuer),
		RequireMaintainer: authAPI.RequireMaintainer,
	})
	return w, jobAPI, procAPI, buildMaintenanceAPI(maintenanceSvc, svcs.nameless, db, authAPI), nil
}

// jobServiceDeps bundles what buildJobServices needs, so the construction step
// takes one parameter rather than ten.
type jobServiceDeps struct {
	cfg         *config.Config
	db          *database.DB
	store       *jobs.Store
	enqueuer    *jobs.Enqueuer
	embed       *embedjob.Service
	face        *facejob.Service
	storyboard  *storyboardjob.Service
	embedClient embedding.Client
	// places is the reverse-geocode service, already built by the caller because
	// the regeocode rebuild endpoint shares it (nil when no mapy.com key is set).
	places *placesjob.Service
	// cluster is the face-grouping job service, already built by the caller because
	// the clusters API shares it (it schedules the preparation the listing needs).
	cluster *clusterjob.Service
	reg     *metrics.Registry
}

// buildJobServices constructs every handler the worker registry needs, returning
// them bundled together with the library-maintenance service (which shares the
// thumbnail service's construction and is not a job handler itself). The
// config-gated ones — sidecar, OCR, mail — come back nil when their
// feature is switched off; buildRegistry then registers no handler for them, which is what
// keeps a job of a type nothing can claim from ever being enqueued. The places
// service is passed in rather than built here, since the rebuild endpoints share
// it, and is nil with no mapy.com key by the same rule.
func buildJobServices(d jobServiceDeps) (registryServices, *maintenance.Service, error) {
	thumbSvc, maintenanceSvc, err := buildMaintenanceAndThumb(d.cfg, d.db, d.enqueuer, d.embed, d.face, d.reg)
	if err != nil {
		return registryServices{}, nil, err
	}
	metaSvc, err := buildMetaService(d.cfg, d.db, d.enqueuer)
	if err != nil {
		return registryServices{}, nil, err
	}
	sidecarSvc, err := buildSidecarServiceOrNil(d.cfg, d.db, d.enqueuer)
	if err != nil {
		return registryServices{}, nil, err
	}
	ocrSvc, err := buildOCRServiceOrNil(d.cfg, d.db, d.enqueuer, d.embedClient, d.reg)
	if err != nil {
		return registryServices{}, nil, err
	}
	mailSvc, err := buildMailServiceOrNil(d.cfg)
	if err != nil {
		return registryServices{}, nil, err
	}
	return registryServices{
		embed: d.embed, face: d.face, thumb: thumbSvc, meta: metaSvc,
		places: d.places, sidecar: sidecarSvc, ocr: ocrSvc, mail: mailSvc,
		nameless: buildNamelessService(d.db, d.store), storyboard: d.storyboard,
		cluster: d.cluster,
	}, maintenanceSvc, nil
}

// registryServices bundles the job handlers buildRegistry wires, so the
// registration list is one parameter rather than nine.
type registryServices struct {
	embed      *embedjob.Service
	face       *facejob.Service
	thumb      *thumbjob.Service
	meta       *metajob.Service
	places     *placesjob.Service
	sidecar    *sidecarjob.Service
	ocr        *ocrjob.Service
	mail       *mailjob.Service
	nameless   *namelessjob.Service
	storyboard *storyboardjob.Service
	cluster    *clusterjob.Service
}

// buildRegistry returns the worker registry with every configured handler
// registered. The always-available handlers register unconditionally; the
// config-gated ones (places, sidecar, ocr, mail_send) register only when their service was
// built, because an unregistered type is never claimed — so a job of a type with
// no handler would sit queued forever.
func buildRegistry(svc registryServices) *worker.Registry {
	registry := worker.NewRegistry()
	worker.RegisterBuiltins(registry)
	registry.Register(jobs.TypeImageEmbed, svc.embed.Handle)
	registry.Register(jobs.TypeFaceDetect, svc.face.Handle)
	registry.Register(jobs.TypeThumbnail, svc.thumb.Handle)
	registry.Register(jobs.TypeMetadata, svc.meta.Handle)
	registry.Register(jobs.TypeNamelessDetach, svc.nameless.HandleDetach)
	registry.Register(jobs.TypeNamelessRestore, svc.nameless.HandleRestore)
	registry.Register(jobs.TypeStoryboard, svc.storyboard.Handle)
	registry.Register(jobs.TypeFaceCluster, svc.cluster.Handle)
	if svc.places != nil {
		registry.Register(jobs.TypePlaces, svc.places.Handle)
	}
	if svc.sidecar != nil {
		registry.Register(jobs.TypeSidecar, svc.sidecar.Handle)
	}
	if svc.ocr != nil {
		registry.Register(jobs.TypeOCR, svc.ocr.Handle)
	}
	if svc.mail != nil {
		registry.Register(jobs.TypeMailSend, svc.mail.Handle)
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
