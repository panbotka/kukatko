package organizeapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/organize"
)

// albumsResponse is the JSON body returned by the album-list endpoint.
type albumsResponse struct {
	Albums []organize.AlbumSummary `json:"albums"`
}

// photoUIDsResponse is the JSON body returned by the album membership endpoints,
// echoing the album's photos in display (chronological) order after the mutation
// so the client can refresh without a second request.
type photoUIDsResponse struct {
	PhotoUIDs []string `json:"photo_uids"`
}

// handleAlbumList returns every album with its photo count, the cover to render
// for it (hand-picked, else its newest photo) and the span of capture times
// across its photos. It answers 500 if the store fails.
func (a *API) handleAlbumList(w http.ResponseWriter, r *http.Request) {
	albums, err := a.albums.ListAlbums(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing albums failed")
		return
	}
	writeJSON(w, http.StatusOK, albumsResponse{Albums: albums})
}

// handleAlbumCreate creates an album from the request body and returns it with
// its generated UID and unique slug. A malformed body or empty title answers 400;
// an invalid type answers 400.
func (a *API) handleAlbumCreate(w http.ResponseWriter, r *http.Request) {
	in, err := decodeAlbumInput(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	album, err := a.albums.CreateAlbum(r.Context(), in.toAlbum())
	if err != nil {
		status, msg := albumStatus(err)
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusCreated, album)
}

// handleAlbumGet returns the album identified by the path UID, or 404 if missing.
func (a *API) handleAlbumGet(w http.ResponseWriter, r *http.Request) {
	album, err := a.albums.GetAlbumByUID(r.Context(), chi.URLParam(r, "uid"))
	if err != nil {
		status, msg := albumStatus(err)
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusOK, album)
}

// handleAlbumUpdate rewrites an album's editable fields (title, description,
// cover, private) and returns the refreshed album. The album's
// structural type is preserved because it is not user-editable. A malformed body
// or empty title answers 400; a missing album answers 404.
func (a *API) handleAlbumUpdate(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	existing, err := a.albums.GetAlbumByUID(r.Context(), uid)
	if err != nil {
		status, msg := albumStatus(err)
		writeError(w, status, msg)
		return
	}
	in, err := decodeAlbumInput(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	album, err := a.albums.UpdateAlbum(r.Context(), uid, in.toUpdate(existing.Type))
	if err != nil {
		status, msg := albumStatus(err)
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusOK, album)
}

// handleAlbumDelete removes the album identified by the path UID, answering 204
// on success or 404 if no such album exists.
func (a *API) handleAlbumDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.albums.DeleteAlbum(r.Context(), chi.URLParam(r, "uid")); err != nil {
		status, msg := albumStatus(err)
		writeError(w, status, msg)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAlbumAddPhotos adds the requested photos to the album — which is always
// presented chronologically, so no position is involved — and returns the
// refreshed membership order. A malformed or empty body answers 400; a missing
// album or photo answers 404.
func (a *API) handleAlbumAddPhotos(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if !a.requireAlbum(w, r, uid) {
		return
	}
	in, err := decodePhotoUIDs(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	for _, photoUID := range in.PhotoUIDs {
		if err := a.albums.AddPhoto(r.Context(), uid, photoUID); err != nil {
			status, msg := albumStatus(err)
			writeError(w, status, msg)
			return
		}
	}
	a.writeMembership(w, r, uid)
}

// handleAlbumRemovePhotos removes the requested photos from the album and returns
// the refreshed membership order. Removing a photo that is not a member is a
// no-op. A malformed or empty body answers 400; a missing album answers 404.
func (a *API) handleAlbumRemovePhotos(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if !a.requireAlbum(w, r, uid) {
		return
	}
	in, err := decodePhotoUIDs(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	for _, photoUID := range in.PhotoUIDs {
		if err := a.albums.RemovePhoto(r.Context(), uid, photoUID); err != nil {
			writeError(w, http.StatusInternalServerError, "removing album photo failed")
			return
		}
	}
	a.writeMembership(w, r, uid)
}

// requireAlbum reports whether the album exists, writing the mapped error
// response (404 on a missing album) and returning false otherwise. It gives the
// membership handlers a clean 404 before they mutate.
func (a *API) requireAlbum(w http.ResponseWriter, r *http.Request, uid string) bool {
	if _, err := a.albums.GetAlbumByUID(r.Context(), uid); err != nil {
		status, msg := albumStatus(err)
		writeError(w, status, msg)
		return false
	}
	return true
}

// writeMembership writes the album's current photo order as the membership
// response, or 500 if the order cannot be loaded.
func (a *API) writeMembership(w http.ResponseWriter, r *http.Request, uid string) {
	order, err := a.albums.ListPhotoUIDs(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading album photos failed")
		return
	}
	writeJSON(w, http.StatusOK, photoUIDsResponse{PhotoUIDs: order})
}
