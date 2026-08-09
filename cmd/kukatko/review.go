package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/duplicates"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/geoestimate"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/outliers"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
	"github.com/panbotka/kukatko/internal/review"
	"github.com/panbotka/kukatko/internal/reviewapi"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/sweep"
	"github.com/panbotka/kukatko/internal/vectors"
)

// buildReviewAPI assembles the review game over the shared pool. The queue
// side composes the same searches the /recognition and /expand pages use — the
// recognition scan (reusing the candidate service as its finder, bounded by
// cfg.Sweep) for face questions and the expand service for label questions —
// tuned to the uncertainty band from cfg.Review. It calls the sweep service's
// bounded Scan, never the full Sweep behind /faces/sweep: the per-rebuild
// budgets in cfg.Review are what keep the queue off the library's growth curve.
// The answer side reuses the
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
	photoStore := photos.NewStore(db.Pool())
	vectorStore := vectors.NewStore(db.Pool())
	svc := review.New(review.Config{
		Sweeper:          sweepSvc,
		Expander:         buildExpandService(cfg, db, mediaStore),
		Organize:         organize.NewStore(db.Pool()),
		Faces:            vectorStore,
		Feedback:         feedback.NewStore(db.Pool()),
		Assigner:         matchSvc,
		Places:           buildPlaceReviewerOrNil(cfg, db),
		Duplicates:       buildReviewDuplicatesOrNil(cfg, db, vectorStore),
		Outliers:         buildOutlierService(db),
		Subjects:         people.NewStore(db.Pool()),
		Photos:           photoStore,
		Media:            mediaurl.NewBuilder(mediaStore),
		BandMin:          cfg.Review.BandMin,
		BandMax:          cfg.Review.BandMax,
		SureMin:          cfg.Review.SureMin,
		SureShare:        cfg.Review.SureShare,
		QueueSize:        cfg.Review.QueueSize,
		CacheTTL:         cfg.Review.CacheTTL,
		MaxLabels:        cfg.Review.MaxLabels,
		LabelConcurrency: cfg.Review.LabelConcurrency,
		FaceBudget:       cfg.Review.FaceBudget,
		LabelBudget:      cfg.Review.LabelBudget,
		BuildTimeout:     cfg.Review.BuildTimeout,
		MaxPerEntity:     cfg.Review.MaxPerEntity,
		OutlierBudget:    cfg.Review.OutlierBudget,
		OutlierThreshold: cfg.Review.OutlierThreshold,
	})
	return reviewapi.NewAPI(reviewapi.Config{
		Service:      svc,
		Leaderboard:  review.NewLeaderboardStore(db.Pool()),
		RequireWrite: authAPI.RequireWrite,
		RequireAuth:  authAPI.RequireAuth,
	})
}

// buildPlaceReviewerOrNil returns the reviewer behind the game's place check, or
// a nil interface (not a typed-nil pointer, so review's == nil check fires and
// the question type is simply never asked) when location estimation is switched
// off. Without the estimator there are no estimates to rule on, so the check has
// nothing to do either.
func buildPlaceReviewerOrNil(cfg *config.Config, db *database.DB) review.PlaceReviewer {
	if !cfg.LocationEstimate.Enabled {
		return nil
	}
	return geoestimate.NewReviewer(geoestimate.ReviewConfig{
		Catalogue: photos.NewStore(db.Pool()),
		Places:    places.NewStore(db.Pool()),
	})
}

// buildReviewDuplicatesOrNil returns the detector behind the game's duplicate
// check, or a nil interface when duplicate detection is switched off in config —
// the same switch that makes GET /duplicates answer 503, applied to the same
// data, so the two views can never disagree about whether duplicates exist.
func buildReviewDuplicatesOrNil(
	cfg *config.Config, db *database.DB, vectorStore *vectors.Store,
) review.DuplicateFinder {
	if !cfg.Duplicate.Enabled {
		return nil
	}
	photoStore := photos.NewStore(db.Pool())
	return duplicates.New(duplicates.Config{
		Photos:           photoStore,
		Phashes:          photoStore,
		Embeddings:       vectorStore,
		Feedback:         feedback.NewStore(db.Pool()),
		PhashMaxDiff:     cfg.Duplicate.PhashMaxDiff,
		EmbeddingMaxDist: cfg.Duplicate.EmbeddingMaxDist,
	})
}

// buildOutlierService assembles the per-subject outlier ranking the game's
// outlier check reads. It is the same service GET /subjects/{uid}/outliers uses,
// down to excluding the faces a user has already vouched for — so answering yes
// in the game removes the face from the /outliers page too.
func buildOutlierService(db *database.DB) *outliers.Service {
	return outliers.New(outliers.Config{
		Faces:    vectors.NewStore(db.Pool()),
		People:   people.NewStore(db.Pool()),
		Feedback: feedback.NewStore(db.Pool()),
	})
}
