package photoapi

import (
	"net/http"

	"github.com/panbotka/kukatko/internal/auth"
)

// handleTimeline returns the month-granularity date histogram of the photo
// library. It accepts the same filter query parameters as GET /photos (archived,
// has_gps, date range, camera, lens, uploader, album/label scope, country/city
// place scope, favorite, min_rating/flag and the q substring filter) via
// parseListParams, and the aggregation respects them so the buckets
// match exactly what the list would return in the same order. Buckets mirror the
// grid: newest-first by capture time by default, oldest-first when the request
// asks for an ascending sort, and grouped on the same COALESCE(taken_at,
// created_at) an album scope is ordered by — each carrying the running cumulative
// count of photos before it, so a frontend scrubber can map a month to a scroll
// index whichever way the grid beside it runs. The sort *key* is otherwise
// ignored: the histogram is always grouped by date. An invalid filter value
// yields 400.
func (a *API) handleTimeline(w http.ResponseWriter, r *http.Request) {
	// The unknown q tokens are dropped: an aggregation has no place to report
	// them, and the paginated list the histogram accompanies already does.
	params, _, err := parseListParams(r.URL.Query())
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
