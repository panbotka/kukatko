package photoapi

import (
	"net/http"

	"github.com/panbotka/kukatko/internal/auth"
)

// handleTimeline returns the month-granularity date histogram of the photo
// library. It accepts the same filter query parameters as GET /photos (archived,
// private, has_gps, date range, camera, lens, uploader, album/label scope,
// country/city place scope, favorite, min_rating/flag and the q substring
// filter) via parseListParams, and the aggregation respects them so the buckets
// match exactly what the list would return in the same order. Buckets are ordered
// newest-first by capture time to mirror the default grid, and each carries the
// running cumulative count of photos before it so a frontend scrubber can map a
// month to a scroll index. The sort/order params are ignored — the histogram is
// always grouped by date and the scrubber assumes the default date sort. An
// invalid filter value yields 400.
func (a *API) handleTimeline(w http.ResponseWriter, r *http.Request) {
	params, err := parseListParams(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	favorite, err := favoriteRequested(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if favorite {
		params.FavoriteOf = user.UID
	}
	// Scope the per-user rating filters to the caller so min_rating/flag select
	// the same photos here as they do in GET /photos.
	params.RatedBy = &user.UID

	timeline, err := a.store.TimelineBuckets(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "building timeline failed")
		return
	}
	writeJSON(w, http.StatusOK, timeline)
}
