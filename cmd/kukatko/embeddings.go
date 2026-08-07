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
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/vectors"
)

// embeddingClientConfig translates the embedding section of the app config into
// the sidecar client's own config. Every construction site goes through it so
// the timeouts — in particular the short dial timeout that keeps an offline box
// from stalling a search — apply to health probes and queue work alike, rather
// than only wherever someone remembered to pass them.
func embeddingClientConfig(cfg *config.Config) embedding.Config {
	return embedding.Config{
		BaseURL:        cfg.Embedding.URL,
		ImageDim:       cfg.Embedding.ImageDim,
		FaceDim:        cfg.Embedding.FaceDim,
		DialTimeout:    cfg.Embedding.DialTimeout,
		RequestTimeout: cfg.Embedding.RequestTimeout,
		TextTimeout:    cfg.Embedding.TextTimeout,
	}
}

// buildEmbedService assembles the embedding subsystem: the configured original
// store and thumbnailer (the preview sent to the sidecar), the photo and vector
// repositories, and the offline-aware embeddings sidecar client. It returns the
// embedjob.Service (the image_embed handler and backfill) plus the vector store
// and the sidecar client, which the photo API reuses to back the similar-photos
// endpoint and semantic/hybrid search. enqueuer is the shared queue adapter the
// backfill schedules jobs through.
func buildEmbedService(
	cfg *config.Config, db *database.DB, enqueuer *jobs.Enqueuer, reg *metrics.Registry,
) (*embedjob.Service, *vectors.Store, embedding.Client, error) {
	store, err := newStorage(cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	thumbnailer := thumb.New(store, cfg.Storage.CachePath, thumbOptions(cfg, reg)...)
	photoStore := photos.NewStore(db.Pool())
	vectorStore := vectors.NewStore(db.Pool())

	client, err := embedding.New(embeddingClientConfig(cfg))
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
