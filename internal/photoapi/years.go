package photoapi

import (
	"net/http"

	"github.com/panbotka/kukatko/internal/auth"
)

// handleYears returns the calendar years that hold photos, newest first, each
// with its photo count — the option list behind the library's year facet.
//
// It accepts the same filter query parameters as GET /photos (archived, has_gps,
// date range, camera, lens, uploader, album/label scope, country/city place
// scope, favorite, min_rating/flag and the q substring filter) via
// parseListParams, and the aggregation respects them, so a year's count is
// exactly what the grid would show after selecting that year. The caller's
// visibility is therefore enforced by the same clauses as the list: archived
// photos are counted only when the caller asks for them.
//
// The year filter itself is the one parameter deliberately dropped: a facet must
// not narrow its own option list, or picking 2019 would leave 2019 as the only
// year on offer and the reader could never switch. Every other filter still
// applies, so the counts track the rest of the view. sort/order and pagination
// are ignored — the aggregation is always grouped by year. An invalid filter
// value yields 400.
func (a *API) handleYears(w http.ResponseWriter, r *http.Request) {
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
	// The facet offers every year, whichever one is currently selected.
	params.Year = nil

	years, err := a.store.YearBuckets(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "aggregating years failed")
		return
	}
	writeJSON(w, http.StatusOK, years)
}
