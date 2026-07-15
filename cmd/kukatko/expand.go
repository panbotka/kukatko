package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/expand"
	"github.com/panbotka/kukatko/internal/expandapi"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/vectors"
)

// buildExpandAPI assembles the "expand a collection" search over the shared pool:
// the expand service (which votes each of an album's or a label's members' embedding
// neighbours together) behind its editor/admin endpoints. The media store stamps
// the candidate photos' URLs; the write guard is supplied via authAPI so the
// expandapi package stays decoupled from auth's wiring.
func buildExpandAPI(
	cfg *config.Config, db *database.DB, authAPI *auth.API, mediaStore storage.Storage,
) *expandapi.API {
	return expandapi.NewAPI(expandapi.Config{
		Service:      buildExpandService(cfg, db, mediaStore),
		RequireWrite: authAPI.RequireWrite,
	})
}

// buildExpandService assembles the read-only collection-expansion search over the
// shared pool: the vectors, organize, feedback and photos stores plus the media
// builder, tuned from cfg.Expand. Album and label share the one service; only the
// source-set resolution differs.
func buildExpandService(
	cfg *config.Config, db *database.DB, mediaStore storage.Storage,
) *expand.Service {
	return expand.New(expand.Config{
		Vectors:     vectors.NewStore(db.Pool()),
		Organize:    organize.NewStore(db.Pool()),
		Feedback:    feedback.NewStore(db.Pool()),
		Photos:      photos.NewStore(db.Pool()),
		Media:       mediaurl.NewBuilder(mediaStore),
		MaxDistance: cfg.Expand.MaxDistance,
		Limit:       cfg.Expand.Limit,
		MaxLimit:    cfg.Expand.MaxLimit,
		SearchLimit: cfg.Expand.SearchLimit,
		SourceCap:   cfg.Expand.SourceCap,
		Concurrency: cfg.Expand.Concurrency,
	})
}
