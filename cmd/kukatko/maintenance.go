package main

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/backup"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/embedjob"
	"github.com/panbotka/kukatko/internal/facejob"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/maintenance"
	"github.com/panbotka/kukatko/internal/maintenanceapi"
	"github.com/panbotka/kukatko/internal/metajob"
	"github.com/panbotka/kukatko/internal/metrics"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/sidecarexport"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/thumbjob"
	"github.com/panbotka/kukatko/internal/vectors"
)

// maintenanceDisk adapts backup.DiskOriginals (which walks the originals root) to
// maintenance.DiskScanner, converting its LocalOriginal entries to DiskFile and
// dropping the metadata sidecars.
type maintenanceDisk struct {
	disk *backup.DiskOriginals
}

// List walks the originals root and returns every file that is an original as a
// maintenance.DiskFile.
//
// It filters out the metadata sidecar tree, and that filter is the whole reason
// this is not a straight conversion. The integrity scan defines an orphan as "on
// disk but not in the catalogue", which every sidecar is by construction — they
// describe photos, they are not photos, and no row will ever point at one.
// Without this the scan would report one orphan per photo forever, Report.Clean
// would never be true again, and `maintenance repair --import-orphans` would try
// to ingest every YAML file in the library.
//
// The filter lives here rather than in backup.DiskOriginals deliberately: that
// walk also feeds the S3 backup sync, which *should* copy sidecars — the curation
// travelling with the originals into the backup bucket is the point. The two
// callers want different lists, so they diverge at the adapter.
func (d maintenanceDisk) List(ctx context.Context) ([]maintenance.DiskFile, error) {
	originals, err := d.disk.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing originals on disk: %w", err)
	}
	files := make([]maintenance.DiskFile, 0, len(originals))
	for _, o := range originals {
		if isSidecarKey(o.Key) {
			continue
		}
		files = append(files, maintenance.DiskFile{Key: o.Key, Size: o.Size})
	}
	return files, nil
}

// isSidecarKey reports whether key names a metadata sidecar rather than an
// original.
func isSidecarKey(key string) bool {
	return strings.HasPrefix(key, sidecarexport.Prefix+"/")
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
	thumbnailer := thumb.New(store, cfg.Storage.CachePath, thumbOptions(cfg, reg)...)
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
// repairs), the embedding and face backfills, and the orphan importer (the upload
// pipeline). It returns a service ready to scan and repair.
func buildMaintenanceService(
	cfg *config.Config, db *database.DB, enqueuer *jobs.Enqueuer,
	embedSvc *embedjob.Service, faceSvc *facejob.Service, reg *metrics.Registry,
) (*maintenance.Service, error) {
	store, err := newStorage(cfg)
	if err != nil {
		return nil, err
	}
	thumbnailer := thumb.New(store, cfg.Storage.CachePath, thumbOptions(cfg, reg)...)
	photoStore := photos.NewStore(db.Pool())
	ingestSvc := ingest.New(ingest.Config{
		Storage:     store,
		Photos:      photoStore,
		Thumbnailer: thumbnailer,
		Enqueuer:    enqueuer,
		Duplicate:   cfg.Duplicate,
		MaxFileSize: cfg.Upload.MaxFileSizeBytes(),
	})
	return maintenance.New(maintenance.Config{
		Photos:    photoStore,
		Vectors:   vectors.NewStore(db.Pool()),
		Originals: store,
		Disk:      maintenanceDisk{disk: backup.NewDiskOriginals(cfg.Storage.OriginalsPath)},
		Thumbs:    maintenance.NewThumbCache(thumbnailer),
		Enqueuer:  enqueuer,
		Embed:     embedSvc,
		Faces:     faceSvc,
		Importer:  orphanImporter{storage: store, ingest: ingestSvc},
	}), nil
}

// buildMaintenanceAndThumb assembles the thumbnail job service and the
// library-maintenance service in one step, so the serve wiring threads a single
// error check. The thumbnail service regenerates thumbnails/pHashes and drives
// the missing-thumbnail backfill; the maintenance service drives scans and
// repairs.
func buildMaintenanceAndThumb(
	cfg *config.Config, db *database.DB, enqueuer *jobs.Enqueuer,
	embedSvc *embedjob.Service, faceSvc *facejob.Service, reg *metrics.Registry,
) (*thumbjob.Service, *maintenance.Service, error) {
	thumbSvc, err := buildThumbService(cfg, db, enqueuer, reg)
	if err != nil {
		return nil, nil, err
	}
	maintenanceSvc, err := buildMaintenanceService(cfg, db, enqueuer, embedSvc, faceSvc, reg)
	if err != nil {
		return nil, nil, err
	}
	return thumbSvc, maintenanceSvc, nil
}

// buildMaintenanceAPI assembles the maintainer-only maintenance HTTP API over
// svc. Library maintenance is an operations capability, so the maintainer guard
// is supplied via authAPI (maintenanceapi stays decoupled from auth's wiring).
func buildMaintenanceAPI(svc *maintenance.Service, authAPI *auth.API) *maintenanceapi.API {
	return maintenanceapi.NewAPI(maintenanceapi.Config{
		Service:           svc,
		RequireMaintainer: authAPI.RequireMaintainer,
	})
}
