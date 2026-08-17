package photoapi

import (
	"net/http"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
)

// uploaderFacet is one entry of the uploader facet: who uploaded photos into the
// current view, and how many of them are theirs. UID is empty for the photos
// with no uploader — the ones an import brought in — and so is Name, because
// naming that group ("imported") is the client's job: only it knows the reader's
// language.
type uploaderFacet struct {
	UID   string `json:"uid"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// uploadersResponse is the JSON body of the uploader facet: the contributors to
// the current view, largest contribution first. It is an object rather than a
// bare array so the facet can grow a field without breaking every client.
type uploadersResponse struct {
	Uploaders []uploaderFacet `json:"uploaders"`
}

// handleUploaders returns the users who uploaded at least one photo within the
// currently applied filter, each with its photo count, largest first — the
// option list behind the library's uploader facet. In an album it therefore
// lists that event's contributors, not every account on the instance.
//
// It accepts the same filter query parameters as GET /photos via parseListParams
// (archived, has_gps, date range, camera, lens, album/label/person scope,
// country/city, favorite, min_rating/flag and the q query language), and the
// aggregation respects them, so an uploader's count is exactly what the grid
// would show after selecting them. The caller's visibility is enforced by the
// same clauses as the list.
//
// The uploader filter itself is the one parameter deliberately dropped, exactly
// as the year facet drops year=: a facet must not narrow its own option list, or
// picking a person would leave that person as the only one on offer and the
// reader could never switch. sort/order and pagination are ignored — the
// aggregation is always grouped by uploader. An invalid filter value yields 400.
//
// The photos with no uploader are reported as their own entry rather than
// silently dropped, so the counts add up to what the grid shows.
func (a *API) handleUploaders(w http.ResponseWriter, r *http.Request) {
	// The unknown q tokens are dropped: an aggregation has no place to report
	// them, and the paginated list the facet accompanies already does.
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
	// The facet counts the same photos the grid lists, so the caller-dependent
	// tokens resolve here too; an unresolvable `person:me` yields no uploaders,
	// matching an empty grid.
	applyMeTokens(&params, user)
	// The facet offers every uploader, whichever one is currently selected.
	params.UploadedBy, params.NoUploader = "", false

	buckets, err := a.store.UploaderBuckets(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "aggregating uploaders failed")
		return
	}
	writeJSON(w, http.StatusOK, uploadersResponse{Uploaders: uploaderFacets(buckets)})
}

// uploaderFacets renders the store's buckets as the facet's JSON entries, naming
// each uploader the way the photo detail names one (display name, falling back
// to the username). The slice is empty, never nil, so the response always
// carries an array.
func uploaderFacets(buckets []photos.UploaderBucket) []uploaderFacet {
	out := make([]uploaderFacet, 0, len(buckets))
	for _, bucket := range buckets {
		out = append(out, uploaderFacet{
			UID:   bucket.UID,
			Name:  uploaderName(bucket.DisplayName, bucket.Username),
			Count: bucket.Count,
		})
	}
	return out
}

// uploaderName is how an uploader is named to a reader: their display name, or
// their username when they never set one. It is shared by the photo detail's
// uploader object and the uploader facet, so the same person is not called two
// different things on two screens. Both empty — the no-uploader bucket — yields
// "", which the client renders as the imported group.
func uploaderName(displayName, username string) string {
	if displayName != "" {
		return displayName
	}
	return username
}
