package main

import (
	"context"
	"errors"
	"fmt"
	"path"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/embedjob"
	"github.com/panbotka/kukatko/internal/facejob"
	"github.com/panbotka/kukatko/internal/hlsjob"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/maintenance"
	"github.com/panbotka/kukatko/internal/maintenanceapi"
	"github.com/panbotka/kukatko/internal/metajob"
	"github.com/panbotka/kukatko/internal/metrics"
	"github.com/panbotka/kukatko/internal/namelessjob"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/placesjob"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/thumbjob"
	"github.com/panbotka/kukatko/internal/vectors"
)

// maintenanceStore adapts the configured storage backend to
// maintenance.StoreScanner: the scan inventories whatever store this instance
// actually keeps its originals in, so an object-store library is reconciled
// against the bucket and a local one against the originals root.
//
// The kind travels with the scanner because the report says which store it read.
// Both backends list their keys (storage.KeyLister), so the assertion below can
// only fail for a backend that forgot to — an error rather than a silent scan of
// nothing.
func maintenanceStore(cfg *config.Config, store storage.Storage) (*maintenance.StoreOriginals, error) {
	lister, ok := store.(storage.KeyLister)
	if !ok {
		return nil, fmt.Errorf("storage backend %q cannot list its keys", cfg.Storage.Backend)
	}
	kind := maintenance.StoreDisk
	if cfg.Storage.Backend == config.StorageBackendR2 {
		kind = maintenance.StoreObject
	}
	return maintenance.NewStoreOriginals(kind, lister), nil
}

// orphanImporter adapts the upload pipeline to maintenance.OrphanImporter: it
// opens an orphan original through the storage layer and runs it through ingest,
// which catalogues it (deduplicating on content hash).
type orphanImporter struct {
	storage storage.Storage
	ingest  *ingest.Service
}

// ImportOriginal opens the original at key and ingests it, mapping the ingest
// outcome to a maintenance ImportOutcome. A per-file ingest error is surfaced as
// an error so the caller can tally it.
func (o orphanImporter) ImportOriginal(ctx context.Context, key string) (maintenance.ImportOutcome, error) {
	reader, err := o.storage.Open(ctx, key)
	if err != nil {
		return maintenance.ImportCreated, fmt.Errorf("opening orphan %s: %w", key, err)
	}
	defer func() { _ = reader.Close() }()

	res := o.ingest.Ingest(ctx, reader, path.Base(key), "")
	switch res.Outcome {
	case ingest.OutcomeCreated:
		return maintenance.ImportCreated, nil
	case ingest.OutcomeDuplicate:
		return maintenance.ImportDuplicate, nil
	default:
		return maintenance.ImportCreated, errors.New(res.Error)
	}
}

// buildThumbService assembles the thumbnail job service: it regenerates a photo's
// missing thumbnails and recomputes its pHash when absent (the thumbnail job
// handler and the library-maintenance thumbnail/pHash repairs), and — wired with
// the queue enqueuer and photo lister — drives the admin missing-thumbnail
// backfill behind POST /process/thumbnails. The returned service exposes both
// Handle (for the worker registry) and BackfillThumbnails (for processapi).
func buildThumbService(
	cfg *config.Config, db *database.DB, enqueuer *jobs.Enqueuer, reg *metrics.Registry,
) (*thumbjob.Service, error) {
	store, err := newStorage(cfg)
	if err != nil {
		return nil, err
	}
	thumbnailer := thumb.New(store, cfg.Storage.CachePath, thumbOptions(cfg, reg, db)...)
	photoStore := photos.NewStore(db.Pool())
	svc := thumbjob.New(thumbjob.Config{
		Photos:      photoStore,
		Thumbnailer: thumbnailer,
		Decoder:     thumbjob.NewStorageDecoder(store),
		Lister:      photoStore,
		Enqueuer:    enqueuer,
	})
	return svc, nil
}

// buildMetaService assembles the metadata job service: it re-reads a photo's
// original file and fills the IPTC/XMP and file-technical columns that are still
// empty (the `metadata` job handler), and — wired with the queue enqueuer and photo
// lister — drives the admin metadata backfill behind POST /process/metadata. It
// reads originals through the storage layer, so it covers both local and R2
// libraries. The returned service exposes both Handle (for the worker registry) and
// BackfillMetadata (for processapi).
func buildMetaService(
	cfg *config.Config, db *database.DB, enqueuer *jobs.Enqueuer,
) (*metajob.Service, error) {
	store, err := newStorage(cfg)
	if err != nil {
		return nil, err
	}
	photoStore := photos.NewStore(db.Pool())
	return metajob.New(metajob.Config{
		Photos:    photoStore,
		Extractor: metajob.NewStorageExtractor(store),
		Lister:    photoStore,
		Enqueuer:  enqueuer,
	}), nil
}

// buildMaintenanceService assembles the library-maintenance service over the
// shared collaborators: the photo and vector catalogues, the originals store and
// its on-disk walk, the thumbnail cache check, the queue adapter (thumbnail/pHash
// repairs), the embedding and face backfills, the face-matching service (which
// owns the face↔marker pairing the cache repair re-derives), and the orphan
// importer (the upload pipeline). placesSvc backs the reverse-geocode backfill and
// the scan's count of it; it is nil when no mapy.com key is configured, which
// leaves that finding empty and makes `repair --places` refuse. It returns a
// service ready to scan and repair.
func buildMaintenanceService(
	cfg *config.Config, db *database.DB, enqueuer *jobs.Enqueuer,
	embedSvc *embedjob.Service, faceSvc *facejob.Service, placesSvc *placesjob.Service,
	reg *metrics.Registry,
) (*maintenance.Service, error) {
	store, err := newStorage(cfg)
	if err != nil {
		return nil, err
	}
	thumbnailer := thumb.New(store, cfg.Storage.CachePath, thumbOptions(cfg, reg, db)...)
	photoStore := photos.NewStore(db.Pool())
	ingestSvc := ingest.New(ingest.Config{
		Storage:     store,
		Photos:      photoStore,
		Thumbnailer: thumbnailer,
		Enqueuer:    enqueuer,
		OCR:         ocrEnqueuerOrNil(cfg, enqueuer),
		Places:      placesEnqueuerOrNil(cfg, enqueuer),
		Duplicate:   cfg.Duplicate,
		MaxFileSize: cfg.Upload.MaxFileSizeBytes(),
		MaxPixels:   cfg.Thumb.MaxPixels,
	})
	storeScanner, err := maintenanceStore(cfg, store)
	if err != nil {
		return nil, err
	}
	streaming, err := maintenanceStreamingOrZero(cfg, db, store)
	if err != nil {
		return nil, err
	}
	return maintenance.New(maintenance.Config{
		Photos:    photoStore,
		Vectors:   vectors.NewStore(db.Pool()),
		Originals: store,
		Store:     storeScanner,
		Thumbs:    maintenance.NewThumbCache(thumbnailer),
		Enqueuer:  enqueuer,
		Embed:     embedSvc,
		Faces:     faceSvc,
		FaceCache: buildFaceMatch(cfg, db),
		Importer:  orphanImporter{storage: store, ingest: ingestSvc},
		Places:    maintenancePlaceBackfillerOrNil(placesSvc),
		Sidecar:   maintenanceSidecarOrNil(cfg, enqueuer),
		Streaming: streaming,
	}), nil
}

// maintenanceStreamingOrZero returns the collaborators the integrity check's
// streaming half needs — the rendition catalogue and the store its segments live
// in — or a zero Streaming when `video.hls.enabled` is off.
//
// The zero value is not a degraded mode but the correct one: with streaming off
// nothing encodes, so a rendition row dropped as broken would never be produced
// again and an object swept as orphaned would never be rewritten. The scan then
// reports nothing and, just as importantly, asks the store nothing.
//
// A backend that cannot list one prefix is an error rather than a silent
// downgrade, for the same reason the originals scan insists on a key listing: a
// check that quietly reconciles against nothing reports a clean library.
func maintenanceStreamingOrZero(
	cfg *config.Config, db *database.DB, store storage.Storage,
) (maintenance.Streaming, error) {
	if !cfg.Video.HLS.Enabled {
		return maintenance.Streaming{}, nil
	}
	segments, ok := store.(maintenance.SegmentStore)
	if !ok {
		return maintenance.Streaming{}, fmt.Errorf(
			"storage backend %q cannot list a key prefix", cfg.Storage.Backend)
	}
	return maintenance.Streaming{
		Renditions: hlsjob.NewStore(db.Pool()),
		Segments:   segments,
	}, nil
}

// maintenanceSidecarOrNil returns the sidecar scheduler the repairs follow a
// catalogue write with, or nil when the sidecar export is off on this instance.
//
// It is the same switch every mutating API goes through (sidecarSchedulerFor), in
// its maintenance shape: with the export off no `sidecar` handler is registered,
// so an enqueued job would sit in the queue for ever. Returning nil rather than
// the no-op scheduler keeps that visible in the Config as "this instance has no
// sidecars", the way a nil Places says it has no mapy.com key.
func maintenanceSidecarOrNil(cfg *config.Config, enqueuer *jobs.Enqueuer) maintenance.SidecarScheduler {
	if !cfg.Sidecar.Enabled {
		return nil
	}
	return enqueuer
}

// buildMaintenanceAndThumb assembles the thumbnail job service and the
// library-maintenance service in one step, so the serve wiring threads a single
// error check. The thumbnail service regenerates thumbnails/pHashes and drives
// the missing-thumbnail backfill; the maintenance service drives scans and
// repairs.
func buildMaintenanceAndThumb(
	cfg *config.Config, db *database.DB, enqueuer *jobs.Enqueuer,
	embedSvc *embedjob.Service, faceSvc *facejob.Service, placesSvc *placesjob.Service,
	reg *metrics.Registry,
) (*thumbjob.Service, *maintenance.Service, error) {
	thumbSvc, err := buildThumbService(cfg, db, enqueuer, reg)
	if err != nil {
		return nil, nil, err
	}
	maintenanceSvc, err := buildMaintenanceService(cfg, db, enqueuer, embedSvc, faceSvc, placesSvc, reg)
	if err != nil {
		return nil, nil, err
	}
	return thumbSvc, maintenanceSvc, nil
}

// buildNamelessService assembles the nameless-subject repair over the people
// store and the job queue: the admin surface reports and schedules through it,
// the worker runs its two handlers. It needs no configuration, so it is always
// available — the repair for a catch-all subject an importer minted must not
// depend on an optional feature being switched on.
func buildNamelessService(db *database.DB, store *jobs.Store) *namelessjob.Service {
	return namelessjob.New(people.NewStore(db.Pool()), store, nil)
}

// buildMaintenanceAPI assembles the maintainer-only maintenance HTTP API over
// svc, the audit store (for the retention purge of old audit entries) and the
// nameless-subject repair. Library maintenance is an operations capability, so
// the maintainer guard is supplied via authAPI (maintenanceapi stays decoupled
// from auth's wiring).
func buildMaintenanceAPI(
	svc *maintenance.Service, nameless *namelessjob.Service, db *database.DB, authAPI *auth.API,
) *maintenanceapi.API {
	return maintenanceapi.NewAPI(maintenanceapi.Config{
		Service:           svc,
		Audit:             audit.NewStore(db.Pool()),
		Nameless:          nameless,
		RequireMaintainer: authAPI.RequireMaintainer,
	})
}
