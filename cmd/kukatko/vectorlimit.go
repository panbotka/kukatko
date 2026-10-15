package main

import (
	"net/http"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/ratelimit"
)

// newVectorThrottle returns the middleware that throttles the browse pair of
// vector searches (GET /search, GET /photos/{uid}/similar): one token-bucket
// per acting user (ratelimit.vector), then one per project
// (ratelimit.vector_project). See userThenProjectThrottle for the order and the
// exemption.
func newVectorThrottle(cfg *config.Config) func(http.Handler) http.Handler {
	return userThenProjectThrottle(cfg.RateLimit.Vector, cfg.RateLimit.VectorProject)
}

// newHeavyVectorThrottle returns the middleware that throttles the routes
// where one request scans a whole project's vectors — the duplicate groups,
// the review game's queue, the recognition sweep, the per-subject candidate
// search and the collection expansion: one token-bucket per acting user
// (ratelimit.heavy_vector), then one per project
// (ratelimit.heavy_vector_project). The serve command builds it once and hands
// the same middleware to all five routes, so they share one budget.
func newHeavyVectorThrottle(cfg *config.Config) func(http.Handler) http.Handler {
	return userThenProjectThrottle(cfg.RateLimit.HeavyVector, cfg.RateLimit.HeavyVectorProject)
}

// userThenProjectThrottle returns a middleware over two fresh limiters: one
// keyed by the acting user (perUser), then one keyed by the request's project
// (perProject). Every route the returned middleware guards draws on the same two
// limiters, so a caller cannot multiply its budget by spreading itself over the
// group's endpoints.
//
// The user's bucket is checked first, deliberately: a person over their own
// share is refused without touching the project's bucket, so one member cannot
// spend the budget the rest of their project shares. A token an admin marked
// unlimited (auth.RateLimitExempt) skips both. The middleware must be mounted
// inside the route's auth guard — the keys are the identity and the project the
// guard resolved.
func userThenProjectThrottle(perUser, perProject config.RateLimitRule) func(http.Handler) http.Handler {
	userLimit := userThrottle(perUser)
	projectLimit := ratelimit.New(perProject.RatePerSec, perProject.Burst).
		KeyedMiddlewareExcept(projectRateKey, auth.RateLimitExempt)
	return func(next http.Handler) http.Handler {
		return userLimit(projectLimit(next))
	}
}

// userThrottle returns a fresh per-user throttle for rule: a limiter of its own,
// keyed by the acting user (commentRateKey), which an API token flagged
// unlimited skips. Like every keyed throttle it must sit inside the auth guard.
func userThrottle(rule config.RateLimitRule) func(http.Handler) http.Handler {
	return ratelimit.New(rule.RatePerSec, rule.Burst).KeyedMiddlewareExcept(commentRateKey, auth.RateLimitExempt)
}

// projectRateKey is the rate-limit bucket key of a per-project rule: the uid of
// the project the guard admitted the request into. A route without a project
// cannot carry such a rule, so the empty fallback only keeps the function total.
func projectRateKey(r *http.Request) string {
	return auth.ProjectUIDFromContext(r.Context())
}

// geocodeProjectRate maps the ratelimit.geocode_project rule onto the maps API's
// per-project geocode share: the rule's "non-positive disables" becomes the
// maps API's negative rate (its zero would mean "use the default").
func geocodeProjectRate(rule config.RateLimitRule) (ratePerSec float64, burst int) {
	if rule.RatePerSec <= 0 {
		return -1, rule.Burst
	}
	return rule.RatePerSec, rule.Burst
}
