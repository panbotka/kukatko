package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/duplicates"
	"github.com/panbotka/kukatko/internal/duplicatesapi"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// buildDuplicatesAPI assembles the editor/admin duplicates HTTP API. When
// duplicate detection is disabled in config the API is mounted with a nil
// service, so the route still exists but answers 503 — keeping the surface
// uniform. Embedding-based grouping reads vectors already stored in Postgres, so
// it works even while the embeddings box is offline. The write guard is supplied
// via authAPI so duplicatesapi stays decoupled from auth's wiring.
func buildDuplicatesAPI(
	cfg *config.Config, db *database.DB, authAPI *auth.API, vectorStore *vectors.Store,
) *duplicatesapi.API {
	if !cfg.Duplicate.Enabled {
		return duplicatesapi.NewAPI(duplicatesapi.Config{Service: nil, RequireWrite: authAPI.RequireWrite})
	}
	photoStore := photos.NewStore(db.Pool())
	svc := duplicates.New(duplicates.Config{
		Photos:           photoStore,
		Phashes:          photoStore,
		Embeddings:       vectorStore,
		PhashMaxDiff:     cfg.Duplicate.PhashMaxDiff,
		EmbeddingMaxDist: cfg.Duplicate.EmbeddingMaxDist,
	})
	return duplicatesapi.NewAPI(duplicatesapi.Config{Service: svc, RequireWrite: authAPI.RequireWrite})
}
