// Package organizeapi exposes the album and label catalogue over HTTP: listing
// albums and labels with their photo counts, creating/editing/deleting them,
// managing an album's photo membership (add, remove, reorder) and attaching or
// detaching labels to photos. Browsing an album's or a label's photos is served
// by the shared photo-list endpoint scoped with the ?album= / ?label= query
// parameters, so this package owns only the catalogue and membership surface and
// the frontend reuses the same photo grid.
//
// Reads are open to any authenticated user; mutations require the editor/admin
// write guard. The guards are injected and the store is an interface, so the
// package stays decoupled from auth's wiring and is unit-testable with fakes.
package organizeapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/organize"
)

// AlbumStore is the subset of organize.Store the album endpoints need. It is an
// interface so the handlers depend on behaviour rather than the concrete store,
// keeping them unit-testable with fakes.
type AlbumStore interface {
	// ListAlbums returns every album with its photo count, effective cover and
	// capture-time span.
	ListAlbums(ctx context.Context) ([]organize.AlbumSummary, error)
	// CreateAlbum inserts an album and returns it with its generated UID/slug.
	CreateAlbum(ctx context.Context, album organize.Album) (organize.Album, error)
	// GetAlbumByUID returns one album or organize.ErrAlbumNotFound.
	GetAlbumByUID(ctx context.Context, uid string) (organize.Album, error)
	// UpdateAlbum rewrites an album's editable fields, or returns
	// organize.ErrAlbumNotFound.
	UpdateAlbum(ctx context.Context, uid string, upd organize.AlbumUpdate) (organize.Album, error)
	// DeleteAlbum removes an album, or returns organize.ErrAlbumNotFound.
	DeleteAlbum(ctx context.Context, uid string) error
	// AddPhoto adds a photo to an album at the given position (idempotent).
	AddPhoto(ctx context.Context, albumUID, photoUID string, sortOrder int) error
	// RemovePhoto removes a photo from an album (idempotent).
	RemovePhoto(ctx context.Context, albumUID, photoUID string) error
	// ReorderPhotos sets the album's photo order to the given UID sequence.
	ReorderPhotos(ctx context.Context, albumUID string, orderedPhotoUIDs []string) error
	// ListPhotoUIDs returns an album's photo UIDs in display order.
	ListPhotoUIDs(ctx context.Context, albumUID string) ([]string, error)
}

// LabelStore is the subset of organize.Store the label endpoints need.
type LabelStore interface {
	// ListLabels returns every label with its photo count.
	ListLabels(ctx context.Context) ([]organize.LabelCount, error)
	// CreateLabel inserts a label and returns it with its generated UID/slug.
	CreateLabel(ctx context.Context, label organize.Label) (organize.Label, error)
	// GetLabelByUID returns one label or organize.ErrLabelNotFound.
	GetLabelByUID(ctx context.Context, uid string) (organize.Label, error)
	// UpdateLabel rewrites a label's editable fields, or returns
	// organize.ErrLabelNotFound.
	UpdateLabel(ctx context.Context, uid string, upd organize.LabelUpdate) (organize.Label, error)
	// DeleteLabel removes a label, or returns organize.ErrLabelNotFound.
	DeleteLabel(ctx context.Context, uid string) error
	// AttachLabel attaches a label to a photo with a source and uncertainty.
	AttachLabel(ctx context.Context, photoUID, labelUID string, source organize.LabelSource, uncertainty int) error
	// DetachLabel removes a label from a photo (idempotent).
	DetachLabel(ctx context.Context, photoUID, labelUID string) error
}

// API exposes the album and label endpoints over HTTP. The route guards are
// supplied by the caller (the auth subsystem) so this package depends on auth's
// behaviour, not its wiring.
type API struct {
	albums       AlbumStore
	labels       LabelStore
	requireAuth  func(http.Handler) http.Handler
	requireWrite func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI.
type Config struct {
	// Albums backs the album reads, mutations and membership management.
	Albums AlbumStore
	// Labels backs the label reads, mutations and attachment management.
	Labels LabelStore
	// RequireAuth guards the read endpoints for any signed-in user.
	RequireAuth func(http.Handler) http.Handler
	// RequireWrite guards the mutating endpoints for editors and admins.
	RequireWrite func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{
		albums:       cfg.Albums,
		labels:       cfg.Labels,
		requireAuth:  cfg.RequireAuth,
		requireWrite: cfg.RequireWrite,
	}
}

// RegisterRoutes mounts the album and label endpoints onto r, which the caller
// has scoped under the API base path (for example /api/v1):
//
//	GET    /albums                RequireAuth   list albums with counts + cover
//	POST   /albums                RequireWrite  create an album
//	GET    /albums/{uid}          RequireAuth   one album
//	PATCH  /albums/{uid}          RequireWrite  edit title/description/cover/order/private
//	DELETE /albums/{uid}          RequireWrite  delete an album
//	POST   /albums/{uid}/photos   RequireWrite  add photos to the album
//	DELETE /albums/{uid}/photos   RequireWrite  remove photos from the album
//	PATCH  /albums/{uid}/order    RequireWrite  reorder the album's photos
//
//	GET    /labels                RequireAuth   list labels with counts
//	POST   /labels                RequireWrite  create a label
//	GET    /labels/{uid}          RequireAuth   one label
//	PATCH  /labels/{uid}          RequireWrite  edit name/priority
//	DELETE /labels/{uid}          RequireWrite  delete a label
//	POST   /labels/{uid}/photos   RequireWrite  attach the label to a photo
//	DELETE /labels/{uid}/photos   RequireWrite  detach the label from a photo
//
// An album's or label's photos are browsed via the shared GET /photos endpoint
// with the ?album={uid} / ?label={uid} scope, so no list-photos route lives here.
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/albums", func(r chi.Router) {
		r.With(a.requireAuth).Get("/", a.handleAlbumList)
		r.With(a.requireWrite).Post("/", a.handleAlbumCreate)
		r.With(a.requireAuth).Get("/{uid}", a.handleAlbumGet)
		r.With(a.requireWrite).Patch("/{uid}", a.handleAlbumUpdate)
		r.With(a.requireWrite).Delete("/{uid}", a.handleAlbumDelete)
		r.With(a.requireWrite).Post("/{uid}/photos", a.handleAlbumAddPhotos)
		r.With(a.requireWrite).Delete("/{uid}/photos", a.handleAlbumRemovePhotos)
		r.With(a.requireWrite).Patch("/{uid}/order", a.handleAlbumReorder)
	})
	r.Route("/labels", func(r chi.Router) {
		r.With(a.requireAuth).Get("/", a.handleLabelList)
		r.With(a.requireWrite).Post("/", a.handleLabelCreate)
		r.With(a.requireAuth).Get("/{uid}", a.handleLabelGet)
		r.With(a.requireWrite).Patch("/{uid}", a.handleLabelUpdate)
		r.With(a.requireWrite).Delete("/{uid}", a.handleLabelDelete)
		r.With(a.requireWrite).Post("/{uid}/photos", a.handleLabelAttach)
		r.With(a.requireWrite).Delete("/{uid}/photos", a.handleLabelDetach)
	})
}

// errorBody is the JSON body returned for error responses.
type errorBody struct {
	Error string `json:"error"`
}

// writeJSON writes payload as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("organizeapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

// albumStatus maps a store error from an album operation to the HTTP status and
// client message: a missing album or photo is 404, an invalid type is 400, and
// anything else is a 500 with a generic message.
func albumStatus(err error) (int, string) {
	switch {
	case errors.Is(err, organize.ErrAlbumNotFound):
		return http.StatusNotFound, "album not found"
	case errors.Is(err, organize.ErrPhotoNotFound):
		return http.StatusNotFound, "photo not found"
	case errors.Is(err, organize.ErrInvalidType):
		return http.StatusBadRequest, err.Error()
	default:
		return http.StatusInternalServerError, "album operation failed"
	}
}

// labelStatus maps a store error from a label operation to the HTTP status and
// client message: a missing label or photo is 404, an invalid source is 400, and
// anything else is a 500 with a generic message.
func labelStatus(err error) (int, string) {
	switch {
	case errors.Is(err, organize.ErrLabelNotFound):
		return http.StatusNotFound, "label not found"
	case errors.Is(err, organize.ErrPhotoNotFound):
		return http.StatusNotFound, "photo not found"
	case errors.Is(err, organize.ErrInvalidSource):
		return http.StatusBadRequest, err.Error()
	default:
		return http.StatusInternalServerError, "label operation failed"
	}
}
