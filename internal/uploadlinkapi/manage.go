package uploadlinkapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/uploadlink"
)

// PublicPathPrefix is the frontend route a link's code hangs off: a link reads
// /u/<code> (web/src/App.tsx, UploadLinkPage).
const PublicPathPrefix = "/u/"

// day is one day of validity.
const day = 24 * time.Hour

// linkView is one link as the management routes return it: the stored record
// plus its state at the time of the answer.
type linkView struct {
	uploadlink.Link
	State uploadlink.State `json:"state"`
}

// listResponse is the body of GET /upload-links: the links plus the validity
// bounds the create and extend forms offer.
type listResponse struct {
	Links       []linkView `json:"links"`
	DefaultDays int        `json:"default_days"`
	MaxDays     int        `json:"max_days"`
}

// createRequest is the body of POST /upload-links.
type createRequest struct {
	Title     string   `json:"title"`
	Note      string   `json:"note"`
	AlbumUIDs []string `json:"album_uids"`
	LabelUIDs []string `json:"label_uids"`
	// ValidDays is the validity in days; 0 takes the configured default.
	ValidDays int `json:"valid_days"`
}

// createResponse is the body of a created link: the only response that ever
// carries the code.
type createResponse struct {
	Link linkView `json:"link"`
	Code string   `json:"code"`
	Path string   `json:"path"`
}

// extendRequest is the body of POST /upload-links/{uid}/extend: the link is
// valid for this many days from now.
type extendRequest struct {
	ValidDays int `json:"valid_days"`
}

// linkResponse wraps one link.
type linkResponse struct {
	Link linkView `json:"link"`
}

// errValidDays is the 400 for a validity outside 1..MaxDays.
var errValidDays = errors.New("valid_days is out of range")

// view projects link onto its response shape at the current time.
func (a *API) view(link uploadlink.Link) linkView {
	return linkView{Link: link, State: link.StateAt(a.now())}
}

// handleList writes the caller's links — or, for an administrator, everybody's —
// newest first.
func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	creator := user.UID
	if user.Role.IsAdmin() {
		creator = ""
	}
	links, err := a.store.List(r.Context(), creator)
	if err != nil {
		a.serverError(w, r, "listing upload links", err)
		return
	}
	views := make([]linkView, len(links))
	for i, link := range links {
		views[i] = a.view(link)
	}
	writeJSON(w, http.StatusOK, listResponse{Links: views, DefaultDays: a.defaultDays, MaxDays: a.maxDays})
}

// handleCreate creates a link and answers 201 with it and its code, 400 for a
// body or targets the store refuses.
func (a *API) handleCreate(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req createRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	days, err := a.validDays(req.ValidDays)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	in := uploadlink.NewLink{
		Title: trimmed(req.Title), Note: trimmed(req.Note), CreatedBy: user.UID,
		ExpiresAt: a.now().Add(time.Duration(days) * day),
		AlbumUIDs: req.AlbumUIDs, LabelUIDs: req.LabelUIDs,
	}
	entry := audit.FromRequest(r, user.UID).Entry(audit.ActionUploadLinkCreate, "", "", map[string]any{
		"title": in.Title, "album_uids": in.AlbumUIDs, "label_uids": in.LabelUIDs,
		"expires_at": in.ExpiresAt.UTC(),
	})
	link, code, err := a.store.Create(r.Context(), in, entry)
	if err != nil {
		a.writeStoreError(w, r, "creating upload link", err)
		return
	}
	writeJSON(w, http.StatusCreated, createResponse{Link: a.view(link), Code: code, Path: PublicPathPrefix + code})
}

// handleExtend makes a link valid for valid_days from now. Only the creator or
// an administrator may; anybody else gets the 404 an unknown link gets. A
// revoked link is 409.
func (a *API) handleExtend(w http.ResponseWriter, r *http.Request) {
	user, link, ok := a.ownedLink(w, r)
	if !ok {
		return
	}
	var req extendRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	days, err := a.validDays(req.ValidDays)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	entry := audit.FromRequest(r, user.UID).Entry(audit.ActionUploadLinkExtend, "", "", nil)
	updated, err := a.store.Extend(r.Context(), link.UID, a.now().Add(time.Duration(days)*day), entry)
	if err != nil {
		a.writeStoreError(w, r, "extending upload link", err)
		return
	}
	writeJSON(w, http.StatusOK, linkResponse{Link: a.view(updated)})
}

// handleRevoke revokes a link for good. Only the creator or an administrator
// may; anybody else gets the 404 an unknown link gets. Revoking twice is a
// no-op.
func (a *API) handleRevoke(w http.ResponseWriter, r *http.Request) {
	user, link, ok := a.ownedLink(w, r)
	if !ok {
		return
	}
	entry := audit.FromRequest(r, user.UID).Entry(audit.ActionUploadLinkRevoke, "", "",
		map[string]any{"title": link.Title})
	updated, err := a.store.Revoke(r.Context(), link.UID, entry)
	if err != nil {
		a.writeStoreError(w, r, "revoking upload link", err)
		return
	}
	writeJSON(w, http.StatusOK, linkResponse{Link: a.view(updated)})
}

// ownedLink resolves the {uid} link for a caller who may manage it — its
// creator or an administrator — writing the error response and returning false
// otherwise. Somebody else's link is a 404, never a 403, so the management
// routes do not confirm that a UID exists.
func (a *API) ownedLink(w http.ResponseWriter, r *http.Request) (auth.User, uploadlink.Link, bool) {
	user, ok := a.currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return auth.User{}, uploadlink.Link{}, false
	}
	link, err := a.store.Get(r.Context(), chi.URLParam(r, "uid"))
	if err != nil {
		a.writeStoreError(w, r, "reading upload link", err)
		return auth.User{}, uploadlink.Link{}, false
	}
	if !user.Role.IsAdmin() && (link.CreatedBy == nil || *link.CreatedBy != user.UID) {
		writeError(w, http.StatusNotFound, uploadlink.ErrNotFound.Error())
		return auth.User{}, uploadlink.Link{}, false
	}
	return user, link, true
}

// validDays resolves a requested validity: 0 takes the default, anything else
// must lie in 1..maxDays.
func (a *API) validDays(days int) (int, error) {
	if days == 0 {
		return a.defaultDays, nil
	}
	if days < 1 || days > a.maxDays {
		return 0, errValidDays
	}
	return days, nil
}

// writeStoreError maps a store error onto its response: the validation errors
// are 400, an unknown link 404, a revoked one 409, anything else a logged 500.
func (a *API) writeStoreError(w http.ResponseWriter, r *http.Request, doing string, err error) {
	switch {
	case errors.Is(err, uploadlink.ErrNotFound):
		writeError(w, http.StatusNotFound, uploadlink.ErrNotFound.Error())
	case errors.Is(err, uploadlink.ErrRevoked):
		writeError(w, http.StatusConflict, uploadlink.ErrRevoked.Error())
	case errors.Is(err, uploadlink.ErrNoTargets), errors.Is(err, uploadlink.ErrTooManyTargets),
		errors.Is(err, uploadlink.ErrTargetNotFound), errors.Is(err, uploadlink.ErrTitleTooLong),
		errors.Is(err, uploadlink.ErrNoteTooLong):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		a.serverError(w, r, doing, err)
	}
}
