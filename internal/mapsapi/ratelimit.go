package mapsapi

import (
	"net/http"

	"github.com/panbotka/kukatko/internal/auth"
)

// instanceBucket is the one key of the instance-wide geocode limiter: every
// project draws on the same mapy.com credit budget, so they share one bucket.
const instanceBucket = "instance"

// allowGeocode reports whether a geocode lookup for projectUID may reach
// mapy.com now, spending one token from the project's own bucket and one from
// the instance-wide bucket when it can.
//
// The project's share is checked first, deliberately: a project that is over its
// share is refused without touching the instance bucket, so one busy project can
// never drain the credits every other project's lookups wait on — which is the
// whole point of having the per-project bucket at all. The instance bucket stays
// the ceiling on the sum, because the budget it guards is the instance's.
func (a *API) allowGeocode(projectUID string) bool {
	if !a.projectGeocodeLimiter.Allow(projectUID) {
		return false
	}
	return a.geocodeLimiter.Allow(instanceBucket)
}

// projectCacheKey prefixes a geocode cache key with the project it was asked
// in. Both geocode caches are shared memory, and a hit is answered visibly
// faster than a miss that goes to mapy.com — so a shared entry would tell a
// member of one project that somebody, somewhere, had looked this place up.
// Keying the entry by project keeps that signal inside the library it came
// from, at the cost of each project paying once for a place another project has
// already asked about; masking the latency instead would cost every hit a
// deliberate delay, which defeats the typeahead the cache exists for.
func projectCacheKey(projectUID, key string) string {
	return projectUID + "|" + key
}

// requestProject returns the uid of the project the request runs in, as the
// route guard resolved it. It is the default of API.projectOf.
func requestProject(r *http.Request) string {
	return auth.ProjectUIDFromContext(r.Context())
}
