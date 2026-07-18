package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/review"
	"github.com/panbotka/kukatko/internal/reviewapi"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/sweep"
	"github.com/panbotka/kukatko/internal/vectors"
)

// buildReviewAPI assembles the review game over the shared pool. The queue
// side composes the same searches the /recognition and /expand pages use — a
// recognition sweep (reusing the candidate service as its finder, bounded by
// cfg.Sweep) for face questions and the expand service for label questions —
// tuned to the uncertainty band from cfg.Review. The answer side reuses the
// photo API's facematch service (matchSvc) so face confirmations go through
// the one assign state machine, the organize store for label attaches and the
// feedback store for rejections. The leaderboard aggregates the review-tagged
// audit rows straight from the shared pool. The write and auth guards are
// supplied via authAPI so reviewapi stays decoupled from auth's wiring.
func buildReviewAPI(
	cfg *config.Config, db *database.DB, authAPI *auth.API, mediaStore storage.Storage,
	matchSvc *facematch.Service,
) *reviewapi.API {
	sweepSvc := sweep.New(sweep.Config{
		Subjects:    people.NewStore(db.Pool()),
		Finder:      buildCandidatesService(cfg, db, mediaStore),
		Concurrency: cfg.Sweep.Concurrency,
		MaxSubjects: cfg.Sweep.MaxSubjects,
	})
	svc := review.New(review.Config{
		Sweeper:          sweepSvc,
		Expander:         buildExpandService(cfg, db, mediaStore),
		Organize:         organize.NewStore(db.Pool()),
		Faces:            vectors.NewStore(db.Pool()),
		Feedback:         feedback.NewStore(db.Pool()),
		Assigner:         matchSvc,
		BandMin:          cfg.Review.BandMin,
		BandMax:          cfg.Review.BandMax,
		QueueSize:        cfg.Review.QueueSize,
		CacheTTL:         cfg.Review.CacheTTL,
		MaxLabels:        cfg.Review.MaxLabels,
		LabelConcurrency: cfg.Review.LabelConcurrency,
	})
	return reviewapi.NewAPI(reviewapi.Config{
		Service:      svc,
		Leaderboard:  review.NewLeaderboardStore(db.Pool()),
		RequireWrite: authAPI.RequireWrite,
		RequireAuth:  authAPI.RequireAuth,
	})
}
