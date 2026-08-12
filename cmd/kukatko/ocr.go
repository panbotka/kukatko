package main

import (
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/metrics"
	"github.com/panbotka/kukatko/internal/ocrjob"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/processapi"
	"github.com/panbotka/kukatko/internal/thumb"
)

// buildOCRServiceOrNil assembles the text-recognition service — the `ocr` job
// handler that reads what a photo's signs, shop fronts and scanned pages say, and
// the backfill behind POST /process/ocr — or nil when OCR is switched off.
//
// It reuses the already-instrumented embeddings client (same service, same box,
// same offline behaviour) and its own thumbnailer, because the preview it sends
// is a larger one than image embedding uses: fit_720 loses the small print OCR
// exists to read.
func buildOCRServiceOrNil(
	cfg *config.Config, db *database.DB, enqueuer *jobs.Enqueuer,
	client embedding.Client, reg *metrics.Registry,
) (*ocrjob.Service, error) {
	if !cfg.Embedding.OCR.Enabled {
		return nil, nil //nolint:nilnil // a disabled feature has no service, and that is not an error
	}
	store, err := newStorage(cfg)
	if err != nil {
		return nil, err
	}
	photoStore := photos.NewStore(db.Pool())
	return ocrjob.New(ocrjob.Config{
		Photos:        photoStore,
		Client:        client,
		Previewer:     thumb.New(store, cfg.Storage.CachePath, thumbOptions(cfg, reg, db)...),
		Lister:        photoStore,
		Enqueuer:      enqueuer,
		PreviewSize:   cfg.Embedding.OCR.PreviewSize,
		MinConfidence: cfg.Embedding.OCR.MinConfidence,
	}), nil
}

// ocrBackfillerOrNil returns svc as a processapi.OCRBackfiller, or a nil
// interface (not a typed-nil pointer, so processapi's == nil check fires and
// disables /process/ocr) when OCR is off.
func ocrBackfillerOrNil(svc *ocrjob.Service) processapi.OCRBackfiller {
	if svc == nil {
		return nil
	}
	return svc
}

// ocrEnqueuerOrNil returns the queue adapter the upload pipeline schedules `ocr`
// jobs through, or a nil interface when OCR is off — the pipeline then enqueues
// none, which matters because with the feature off no handler is registered and
// the job would wait in the queue forever.
func ocrEnqueuerOrNil(cfg *config.Config, enqueuer *jobs.Enqueuer) ingest.OCREnqueuer {
	if !cfg.Embedding.OCR.Enabled {
		return nil
	}
	return enqueuer
}
