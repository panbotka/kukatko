package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/ratelimit"
	"github.com/panbotka/kukatko/internal/resolveapi"
)

// buildResolveAPI assembles GET /resolve/{kind}/{uid} over the shared pool: the
// project an old, unprefixed link means, told only to a caller who may enter
// it. Any signed-in account may ask — the route names no project.
//
// It is rate-limited from the comment rule's settings, keyed by the caller, in
// a bucket of its own (like the push subscription write): one number to tune,
// and a script sweeping uids cannot use up anybody's allowance for commenting.
func buildResolveAPI(cfg *config.Config, db *database.DB, authAPI *auth.API) *resolveapi.API {
	limit := ratelimit.New(cfg.RateLimit.Comment.RatePerSec, cfg.RateLimit.Comment.Burst)
	return resolveapi.NewAPI(resolveapi.Config{
		Resolver:        resolveapi.NewStore(db.Pool()),
		RequireSignedIn: authAPI.RequireSignedIn,
		Throttle:        limit.KeyedMiddlewareExcept(commentRateKey, auth.RateLimitExempt),
	})
}
