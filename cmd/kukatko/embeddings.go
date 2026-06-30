package main

import (
	"fmt"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/embedjob"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/metrics"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/vectors"
)

// buildEmbedService assembles the embedding subsystem: the on-disk original
// store and thumbnailer (the preview sent to the sidecar), the photo and vector
// repositories, and the offline-aware embeddings sidecar client. It returns the
// embedjob.Service (the image_embed handler and backfill) plus the vector store
// and the sidecar client, which the photo API reuses to back the similar-photos
// endpoint and semantic/hybrid search. enqueuer is the shared queue adapter the
// backfill schedules jobs through.
func buildEmbedService(
	cfg *config.Config, db *database.DB, enqueuer *jobs.Enqueuer, reg *metrics.Registry,
) (*embedjob.Service, *vectors.Store, embedding.Client, error) {
	store, err := storage.NewFS(cfg.Storage.OriginalsPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("initialising originals storage: %w", err)
	}
	thumbnailer := thumb.New(store, cfg.Storage.CachePath, thumbOptions(cfg, reg)...)
	photoStore := photos.NewStore(db.Pool())
	vectorStore := vectors.NewStore(db.Pool())

	client, err := embedding.New(embedding.Config{
		BaseURL:  cfg.Embedding.URL,
		ImageDim: cfg.Embedding.ImageDim,
		FaceDim:  cfg.Embedding.FaceDim,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("initialising embedding client: %w", err)
	}
	instrumented := instrumentEmbedding(client, reg)

	svc := embedjob.New(embedjob.Config{
		Photos:           photoStore,
		Vectors:          vectorStore,
		Client:           instrumented,
		Previewer:        thumbnailer,
		Enqueuer:         enqueuer,
		DuplicateMaxDist: cfg.Duplicate.EmbeddingMaxDist,
	})
	return svc, vectorStore, instrumented, nil
}
