package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/duplicates"
	"github.com/panbotka/kukatko/internal/duplicatesapi"
	"github.com/panbotka/kukatko/internal/dupmerge"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// buildDuplicatesAPI assembles the editor/admin duplicates HTTP API. When
// duplicate detection is disabled in config the listing route answers 503, but
// the merge (resolve) route always works — it only needs the pool, not the
// detector, so a group discovered while detection was on can still be resolved.
// Embedding-based grouping reads vectors already stored in Postgres, so it works
// even while the embeddings box is offline. The feedback store supplies the pairs
// the user settled as "not duplicates", which the scan drops so a dismissed pair
// stays gone across re-scans. The write guard is supplied via authAPI so
// duplicatesapi stays decoupled from auth's wiring.
func buildDuplicatesAPI(
	cfg *config.Config, db *database.DB, authAPI *auth.API, vectorStore *vectors.Store,
) *duplicatesapi.API {
	merge := dupmerge.NewService(db.Pool())
	if !cfg.Duplicate.Enabled {
		return duplicatesapi.NewAPI(duplicatesapi.Config{
			Service: nil, Merge: merge, RequireWrite: authAPI.RequireWrite,
		})
	}
	photoStore := photos.NewStore(db.Pool())
	svc := duplicates.New(duplicates.Config{
		Photos:           photoStore,
		Phashes:          photoStore,
		Embeddings:       vectorStore,
		Feedback:         feedback.NewStore(db.Pool()),
		PhashMaxDiff:     cfg.Duplicate.PhashMaxDiff,
		EmbeddingMaxDist: cfg.Duplicate.EmbeddingMaxDist,
	})
	return duplicatesapi.NewAPI(duplicatesapi.Config{
		Service: svc, Merge: merge, RequireWrite: authAPI.RequireWrite,
	})
}
