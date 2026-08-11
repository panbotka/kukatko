package main

import (
	"context"
	"fmt"
	"log/slog"

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

// verifyEmbeddingDim asks the sidecar which image model it serves and reports how
// its vector width compares with the configured embedding.image_dim. A
// disagreement is not fatal and must not be: the sidecar is a separate service
// that can be swapped while Kukátko runs, and refusing to start would take the
// whole library down over two features that already degrade on their own. What it
// must not do is stay quiet — an unnoticed mismatch surfaces as every image_embed
// job failing with a non-transient dimension error and semantic search silently
// answering from full text, which reads as "the box is off" rather than "the
// model changed underneath us".
//
// The box is powered off most of the time, so an unreachable sidecar is the
// normal case and is logged at debug level only; the check is a one-off at
// startup and never blocks it.
func verifyEmbeddingDim(ctx context.Context, cfg *config.Config, logger *slog.Logger) {
	if cfg.Embedding.URL == "" {
		return
	}
	client, err := embedding.New(embeddingClientConfig(cfg))
	if err != nil {
		logger.Debug("embedding: dimension check skipped", "error", err)
		return
	}
	health, err := client.Health(ctx)
	if err != nil {
		logger.Debug("embedding: sidecar dimensions unverified", "error", err)
		return
	}
	logEmbeddingDim(logger, health, cfg.Embedding.ImageDim)
}

// logEmbeddingDim compares the sidecar's reported image-vector width with want
// and logs the outcome: a warning naming both values on a mismatch, an info line
// recording the loaded model when they agree, and a debug line when the sidecar
// reports no dimension at all (an older build — unknown, not mismatched).
func logEmbeddingDim(logger *slog.Logger, health embedding.SidecarHealth, want int) {
	switch {
	case health.Dim == 0:
		logger.Debug("embedding: sidecar reports no image dimension", "model", health.Model)
	case health.Dim != want:
		logger.Warn("embedding: sidecar image dimension differs from configured image_dim; "+
			"image_embed jobs will fail and semantic search will degrade to full text",
			"sidecar_dim", health.Dim, "configured_image_dim", want,
			"model", health.Model, "pretrained", health.Pretrained)
	default:
		logger.Info("embedding: sidecar image model",
			"model", health.Model, "pretrained", health.Pretrained,
			"dim", health.Dim, "precision", health.Precision)
	}
}
