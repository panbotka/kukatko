package main

import (
	"fmt"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/hlsjob"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/metrics"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/processapi"
)

// buildHLSServiceOrNil assembles the streaming-encode service — the
// `hls_transcode` job handler that cuts an uploaded video into the segments a
// browser streams and records what its playlists must advertise — or nil when
// the feature is switched off.
//
// The configured rendition names are resolved into the encoder's plan here, so an
// unknown name fails startup with a message naming it rather than producing a
// library of videos that are quietly missing a quality level.
//
// The same Service is both the worker handler and the backfill behind
// POST /process/hls, so it is wired with the catalogue lister and the queue
// enqueuer that backfill needs.
func buildHLSServiceOrNil(
	cfg *config.Config, db *database.DB, enqueuer *jobs.Enqueuer, reg *metrics.Registry,
) (*hlsjob.Service, error) {
	if !cfg.Video.HLS.Enabled {
		return nil, nil //nolint:nilnil // a disabled feature has no service, and that is not an error
	}
	plan, err := hlsPlan(cfg.Video.HLS.Renditions)
	if err != nil {
		return nil, err
	}
	store, err := newStorage(cfg)
	if err != nil {
		return nil, err
	}
	return hlsjob.New(hlsjob.Config{
		Photos:     photos.NewStore(db.Pool()),
		Objects:    store,
		Renditions: hlsjob.NewStore(db.Pool()),
		// The same store and the same queue as the handler: POST /process/hls
		// schedules exactly the job the upload pipeline schedules, over the videos
		// the catalogue says have never been encoded.
		Lister:         photos.NewStore(db.Pool()),
		Enqueuer:       enqueuer,
		Plan:           plan,
		SegmentSeconds: cfg.Video.HLS.SegmentSeconds,
		TempDir:        cfg.Storage.TempPath,
		// The encode is the most expensive thing this application does, so it
		// reports what each rendition cost; with no registry it records nothing.
		Metrics: encodeObserver(reg),
	}), nil
}

// hlsPlan resolves the configured rendition names into the encoder's renditions,
// in the configured order. An empty list, or a name the encoder does not know, is
// an error: both mean the instance asked for an encode it cannot produce.
func hlsPlan(names []string) ([]hls.Rendition, error) {
	if len(names) == 0 {
		return nil, fmt.Errorf("video.hls.renditions: %w", hlsjob.ErrNoRenditions)
	}
	plan := make([]hls.Rendition, 0, len(names))
	for _, name := range names {
		rendition, ok := hls.ByName(name)
		if !ok {
			return nil, fmt.Errorf("video.hls.renditions: unknown rendition %q", name)
		}
		plan = append(plan, rendition)
	}
	return plan, nil
}

// hlsBackfillerOrNil returns svc as a processapi.HLSBackfiller, or a nil
// interface (not a typed-nil pointer, so processapi's == nil check fires and
// disables /process/hls) when streaming is off.
func hlsBackfillerOrNil(svc *hlsjob.Service) processapi.HLSBackfiller {
	if svc == nil {
		return nil
	}
	return svc
}

// hlsEnqueuerOrNil returns the queue adapter the upload pipeline schedules
// `hls_transcode` jobs through, or a nil interface when the feature is off — the
// pipeline then enqueues none, which matters because with no handler registered
// the job would wait in the queue forever.
func hlsEnqueuerOrNil(cfg *config.Config, enqueuer *jobs.Enqueuer) ingest.HLSEnqueuer {
	if !cfg.Video.HLS.Enabled {
		return nil
	}
	return enqueuer
}
