package main

import (
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/facejob"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// buildFaceService assembles the face-detection subsystem: the configured originals
// store (the full-resolution image streamed to the sidecar), the photo and vector
// repositories, and the shared embeddings sidecar client. It returns the
// facejob.Service, which provides the face_detect worker handler and the
// face-detection backfill. enqueuer is the shared queue adapter the backfill
// schedules jobs through; vectorStore and client are shared with the embedding
// subsystem.
func buildFaceService(
	cfg *config.Config, db *database.DB, enqueuer *jobs.Enqueuer,
	vectorStore *vectors.Store, client embedding.Client,
) (*facejob.Service, error) {
	store, err := newStorage(cfg)
	if err != nil {
		return nil, err
	}
	svc := facejob.New(facejob.Config{
		Photos:      photos.NewStore(db.Pool()),
		Vectors:     vectorStore,
		Client:      client,
		Source:      facejob.NewStorageSource(store),
		Enqueuer:    enqueuer,
		MinDetScore: cfg.Faces.MinDetScore,
	})
	return svc, nil
}
