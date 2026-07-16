// Package organizeapi exposes the album and label catalogue over HTTP: listing
// albums and labels with their photo counts, creating/editing/deleting them,
// managing an album's photo membership (add, remove) and attaching or
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

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/organize"
)

// AlbumStore is the subset of organize.Store the album endpoints need. It is an
// interface so the handlers depend on behaviour rather than the concrete store,
// keeping them unit-testable with fakes. Every mutation takes an audit.Entry the
// store writes in the same transaction as the change.
type AlbumStore interface {
	// ListAlbums returns every album with its photo count, effective cover and
	// capture-time span.
	ListAlbums(ctx context.Context) ([]organize.AlbumSummary, error)
	// CreateAlbumAudited inserts an album (auditing the change) and returns it with
	// its generated UID/slug.
	CreateAlbumAudited(ctx context.Context, album organize.Album, entry audit.Entry) (organize.Album, error)
	// GetAlbumByUID returns one album or organize.ErrAlbumNotFound.
	GetAlbumByUID(ctx context.Context, uid string) (organize.Album, error)
	// UpdateAlbumAudited rewrites an album's editable fields (auditing the change),
	// or returns organize.ErrAlbumNotFound.
	UpdateAlbumAudited(
		ctx context.Context, uid string, upd organize.AlbumUpdate, entry audit.Entry,
	) (organize.Album, error)
	// DeleteAlbumAudited removes an album (auditing the change), or returns
	// organize.ErrAlbumNotFound.
	DeleteAlbumAudited(ctx context.Context, uid string, entry audit.Entry) error
	// AddPhotosAudited adds photos to an album (idempotent) as one audited batch.
	AddPhotosAudited(ctx context.Context, albumUID string, photoUIDs []string, entry audit.Entry) error
	// RemovePhotosAudited removes photos from an album (idempotent) as one audited
	// batch.
	RemovePhotosAudited(ctx context.Context, albumUID string, photoUIDs []string, entry audit.Entry) error
	// ListPhotoUIDs returns an album's photo UIDs in display (chronological) order.
	ListPhotoUIDs(ctx context.Context, albumUID string) ([]string, error)
}

// LabelStore is the subset of organize.Store the label endpoints need. Every
// mutation takes an audit.Entry the store writes in the same transaction.
type LabelStore interface {
	// ListLabels returns every label with its photo count.
	ListLabels(ctx context.Context) ([]organize.LabelCount, error)
	// CreateLabelAudited inserts a label (auditing the change) and returns it with
	// its generated UID/slug.
	CreateLabelAudited(ctx context.Context, label organize.Label, entry audit.Entry) (organize.Label, error)
	// GetLabelByUID returns one label or organize.ErrLabelNotFound.
	GetLabelByUID(ctx context.Context, uid string) (organize.Label, error)
	// UpdateLabelAudited rewrites a label's editable fields (auditing the change),
	// or returns organize.ErrLabelNotFound.
	UpdateLabelAudited(
		ctx context.Context, uid string, upd organize.LabelUpdate, entry audit.Entry,
	) (organize.Label, error)
	// DeleteLabelAudited removes a label (auditing the change), or returns
	// organize.ErrLabelNotFound.
	DeleteLabelAudited(ctx context.Context, uid string, entry audit.Entry) error
	// AttachLabelAudited attaches a label to a photo with a source and uncertainty
	// (auditing the change).
	AttachLabelAudited(
		ctx context.Context, photoUID, labelUID string,
		source organize.LabelSource, uncertainty int, entry audit.Entry,
	) error
	// DetachLabelAudited removes a label from a photo (idempotent, auditing the
	// change).
	DetachLabelAudited(ctx context.Context, photoUID, labelUID string, entry audit.Entry) error
}

// API exposes the album and label endpoints over HTTP. The route guards are
// supplied by the caller (the auth subsystem) so this package depends on auth's
// behaviour, not its wiring.
type API struct {
	albums       AlbumStore
	labels       LabelStore
	sidecar      SidecarEnqueuer
	requireAuth  func(http.Handler) http.Handler
	requireWrite func(http.Handler) http.Handler
}

// SidecarEnqueuer schedules a rewrite of a photo's metadata sidecar — the YAML
// file in storage holding its metadata and curation. Album membership and labels
// are exactly the kind of curation that exists nowhere but the database, so a
// change to either makes a photo's sidecar stale. It is satisfied by
// jobs.Enqueuer; a nil SidecarEnqueuer disables the scheduling.
type SidecarEnqueuer interface {
	// EnqueueSidecar schedules a sidecar write for photoUID.
	EnqueueSidecar(ctx context.Context, photoUID string) error
}

// Config bundles the dependencies of NewAPI.
type Config struct {
	// Albums backs the album reads, mutations and membership management.
	Albums AlbumStore
	// Labels backs the label reads, mutations and attachment management.
	Labels LabelStore
	// Sidecar schedules a sidecar rewrite for the photos whose membership or labels
	// changed. When nil no sidecar is scheduled and the mutation still succeeds.
	Sidecar SidecarEnqueuer
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
		sidecar:      cfg.Sidecar,
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
//	PATCH  /albums/{uid}          RequireWrite  edit title/description/cover/private
//	DELETE /albums/{uid}          RequireWrite  delete an album
//	POST   /albums/{uid}/photos   RequireWrite  add photos to the album
//	DELETE /albums/{uid}/photos   RequireWrite  remove photos from the album
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

// auditEntry builds an audit entry for a mutation, stamping the acting user
// (resolved from the request's auth context) plus the request's client IP and
// User-Agent onto the given action, target and details. The store writes the
// returned entry inside the mutation's transaction.
//
// The mutating routes are guarded by RequireWrite, so a principal is present in
// production; an absent principal yields an empty actor UID (stored as NULL)
// rather than failing, which keeps the handlers exercisable behind pass-through
// guards in unit tests.
func (a *API) auditEntry(
	r *http.Request, action, targetType, targetUID string, details map[string]any,
) audit.Entry {
	user, _ := auth.UserFromContext(r.Context())
	return audit.FromRequest(r, user.UID).Entry(action, targetType, targetUID, details)
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
