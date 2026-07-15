package photoapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
)

// FavoriteStore is the subset of the organize repository the photo API needs to
// expose per-user favorites: toggling a photo's favorite state for a user and
// reporting which of a page's photos that user has favorited. It is an interface
// so photoapi depends on the behaviour, not the organize store's construction;
// organize.Store satisfies it and a test fake can stand in.
type FavoriteStore interface {
	// AddFavorite marks photoUID as a favorite of userUID. It is idempotent and
	// returns organize.ErrPhotoNotFound when the photo does not exist.
	AddFavorite(ctx context.Context, userUID, photoUID string) error
	// RemoveFavorite unfavorites photoUID for userUID. It is idempotent.
	RemoveFavorite(ctx context.Context, userUID, photoUID string) error
	// FavoritedAmong reports which of photoUIDs userUID has favorited, as a set
	// keyed by photo UID (only favorited UIDs present).
	FavoritedAmong(ctx context.Context, userUID string, photoUIDs []string) (map[string]bool, error)
}

// PhotoOrganizer is the subset of the organize repository the detail endpoint
// needs to list a photo's album and label memberships, so the detail view can
// show them as inline chips. It is an interface so photoapi depends on the
// behaviour; organize.Store satisfies it and a test fake can stand in. When nil
// the detail response simply omits the memberships.
type PhotoOrganizer interface {
	// AlbumsForPhoto returns the albums the photo belongs to, ordered by title.
	AlbumsForPhoto(ctx context.Context, photoUID string) ([]organize.Album, error)
	// LabelsForPhoto returns the labels attached to the photo, ordered by priority.
	LabelsForPhoto(ctx context.Context, photoUID string) ([]organize.Label, error)
}

// albumRef is the compact album reference embedded in a photo detail response:
// just enough to render and link an inline album chip.
type albumRef struct {
	UID   string `json:"uid"`
	Title string `json:"title"`
}

// labelRef is the compact label reference embedded in a photo detail response.
type labelRef struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
}

// photoView is a photo annotated with the current user's per-user state for the
// list, search and detail responses: the is-favorite flag plus the star rating
// and pick/reject flag (carried by the embedded photos.Photo's rating/flag
// fields). It embeds photos.Photo so every photo field marshals at the top level,
// adding is_favorite alongside.
type photoView struct {
	photos.Photo
	// IsFavorite reports whether the current user has favorited this photo.
	IsFavorite bool `json:"is_favorite"`
	// StackCount is how many photos the stack has when this photo is a stacked
	// primary (always ≥ 2), and 0 (omitted) otherwise. It drives the grid tile's
	// member-count badge. Non-primary members are hidden from listings, so a photo
	// carrying a count is always the one visible member of its stack.
	StackCount int `json:"stack_count,omitempty"`
}

// annotate pairs each photo with the current user's per-user annotations —
// is_favorite plus the star rating and pick/reject flag — resolving the whole
// page's favorites and ratings in one query each, and stamps on the media URLs
// the client fetches. A nil favorites or ratings store leaves those annotations
// at their defaults (is_favorite false, rating 0, flag "none") rather than
// failing, so the catalogue keeps working without either backend.
//
// This is the one place a media URL enters a list response, and reaching it means
// the caller was authorized to see these photos: see the mediaurl package doc on
// why that makes the archive a real security boundary.
func (a *API) annotate(
	ctx context.Context, userUID string, list []photos.Photo,
) ([]photoView, error) {
	views := make([]photoView, len(list))
	for i, p := range list {
		// Normalise the raw catalogue default (empty flag) to "none" so a photo
		// without a rating row still reports a valid flag.
		p.Flag = string(organize.FlagNone)
		a.media.DecorateOne(&p)
		views[i] = photoView{Photo: p}
	}
	if len(list) == 0 {
		return views, nil
	}
	uids := make([]string, len(list))
	for i, p := range list {
		uids[i] = p.UID
	}
	if err := a.applyFavorites(ctx, userUID, uids, views); err != nil {
		return nil, err
	}
	if err := a.annotateRatings(ctx, userUID, uids, views); err != nil {
		return nil, err
	}
	return views, a.annotateStacks(ctx, views)
}

// applyFavorites sets each view's IsFavorite flag from one FavoritedAmong query,
// leaving them false when no favorites store is wired.
func (a *API) applyFavorites(ctx context.Context, userUID string, uids []string, views []photoView) error {
	if a.favorites == nil {
		return nil
	}
	favored, err := a.favorites.FavoritedAmong(ctx, userUID, uids)
	if err != nil {
		return fmt.Errorf("photoapi: resolving favorites: %w", err)
	}
	for i := range views {
		views[i].IsFavorite = favored[views[i].UID]
	}
	return nil
}

// handleAddFavorite marks the photo named in the path as a favorite of the current
// user. It is idempotent (favoriting twice still succeeds) and returns 204. A
// missing photo yields 404; an unwired favorites backend yields 503.
func (a *API) handleAddFavorite(w http.ResponseWriter, r *http.Request) {
	a.runFavoriteToggle(w, r, a.addFavorite)
}

// handleRemoveFavorite unfavorites the photo named in the path for the current
// user. It is idempotent (removing a non-favorite still succeeds) and returns 204.
func (a *API) handleRemoveFavorite(w http.ResponseWriter, r *http.Request) {
	a.runFavoriteToggle(w, r, a.removeFavorite)
}

// addFavorite adds the favorite for the acting user, wrapping (but preserving via
// %w) a missing-photo error so the caller can still map it to 404.
func (a *API) addFavorite(ctx context.Context, userUID, photoUID string) error {
	if err := a.favorites.AddFavorite(ctx, userUID, photoUID); err != nil {
		return fmt.Errorf("photoapi: adding favorite: %w", err)
	}
	return nil
}

// removeFavorite removes the favorite for the acting user.
func (a *API) removeFavorite(ctx context.Context, userUID, photoUID string) error {
	if err := a.favorites.RemoveFavorite(ctx, userUID, photoUID); err != nil {
		return fmt.Errorf("photoapi: removing favorite: %w", err)
	}
	return nil
}

// runFavoriteToggle resolves the acting user, applies op to the path photo and
// writes 204 on success. It answers 503 when no favorites backend is wired, 401
// when unauthenticated, and 404 when the photo does not exist.
func (a *API) runFavoriteToggle(
	w http.ResponseWriter, r *http.Request,
	op func(ctx context.Context, userUID, photoUID string) error,
) {
	if a.favorites == nil {
		writeError(w, http.StatusServiceUnavailable, "favorites backend not configured")
		return
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	uid := chi.URLParam(r, "uid")
	if err := op(r.Context(), user.UID, uid); err != nil {
		writeFavoriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleFavorites lists the current user's favorited photos reusing the photo-list
// shape: it scopes the list to the caller's favorites and otherwise honours every
// filter, the sort and pagination just like GET /photos. It answers 503 when no
// favorites backend is wired and 400 on an invalid filter value.
func (a *API) handleFavorites(w http.ResponseWriter, r *http.Request) {
	if a.favorites == nil {
		writeError(w, http.StatusServiceUnavailable, "favorites backend not configured")
		return
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	params, err := parseListParams(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	params.FavoriteOf = user.UID
	a.writeFavoritePage(w, r, user.UID, params)
}

// writeFavoritePage runs the favorites-scoped list and writes the annotated page.
// It is shared by GET /favorites and the favorite=true branch of GET /photos. It
// scopes the per-user rating filters and the rating sort to the caller by binding
// RatedBy here, so min_rating/flag and sort=rating apply for any list request.
func (a *API) writeFavoritePage(
	w http.ResponseWriter, r *http.Request, userUID string, params photos.ListParams,
) {
	params.RatedBy = &userUID
	list, err := a.store.List(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing photos failed")
		return
	}
	total, err := a.store.Count(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "counting photos failed")
		return
	}
	views, err := a.annotate(r.Context(), userUID, list)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "annotating photos failed")
		return
	}
	writeJSON(w, http.StatusOK, pageResponse(params, views, total))
}

// writeFavoriteError maps a favorites store error to an HTTP response: 404 for a
// missing photo, otherwise 500.
func writeFavoriteError(w http.ResponseWriter, err error) {
	if errors.Is(err, organize.ErrPhotoNotFound) {
		writeError(w, http.StatusNotFound, "photo not found")
		return
	}
	writeError(w, http.StatusInternalServerError, "updating favorite failed")
}
