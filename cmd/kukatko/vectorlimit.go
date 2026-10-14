package main

import (
	"net/http"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/ratelimit"
)

// newVectorThrottle returns the middleware that throttles a group of
// vector-search routes: one token-bucket per acting user (ratelimit.vector),
// then one per project (ratelimit.vector_project). Every route the middleware
// returned by one call guards draws on the same two limiters, so a caller cannot
// multiply its budget by spreading itself over the group's endpoints.
//
// The user's bucket is checked first, deliberately: a person over their own
// share is refused without touching the project's bucket, so one member cannot
// spend the budget the rest of their project shares. A token an admin marked
// unlimited (auth.RateLimitExempt) skips both. The middleware must be mounted
// inside the route's auth guard — the keys are the identity and the project the
// guard resolved.
func newVectorThrottle(cfg *config.Config) func(http.Handler) http.Handler {
	perUser := userThrottle(cfg.RateLimit.Vector)
	perProject := ratelimit.New(cfg.RateLimit.VectorProject.RatePerSec, cfg.RateLimit.VectorProject.Burst).
		KeyedMiddlewareExcept(projectRateKey, auth.RateLimitExempt)
	return func(next http.Handler) http.Handler {
		return perUser(perProject(next))
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
