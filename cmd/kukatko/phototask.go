package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/phototask"
	"github.com/panbotka/kukatko/internal/phototaskapi"
	"github.com/panbotka/kukatko/internal/ratelimit"
)

// buildPhotoTaskAPI assembles the work-queue HTTP API over the shared pool: the
// tasks themselves behind the write guard, and their threads behind the read one
// so every signed-in role — viewers included — can answer the question a task
// asks.
//
// The thread is rate-limited from the same configuration as the per-photo
// comments, in its own bucket. Sharing the settings keeps one number to tune;
// separate buckets mean a burst of answers on a task cannot use up a person's
// allowance for commenting on photographs, which is a different activity.
//
// The task store reports every fresh hand-over to the task_assigned notifier,
// which tells the person asked inside the assignment's transaction.
func buildPhotoTaskAPI(cfg *config.Config, db *database.DB, authAPI *auth.API) *phototaskapi.API {
	limit := ratelimit.New(cfg.RateLimit.Comment.RatePerSec, cfg.RateLimit.Comment.Burst)
	store := phototask.NewStore(db.Pool()).WithAssignObserver(buildTaskAssignNotifier(cfg, db))
	return phototaskapi.NewAPI(phototaskapi.Config{
		Store:           store,
		Comments:        comments.NewStore(db.Pool()),
		RequireAuth:     authAPI.RequireAuth,
		RequireWrite:    authAPI.RequireWrite,
		CommentThrottle: limit.KeyedMiddlewareExcept(commentRateKey, auth.RateLimitExempt),
	})
}
