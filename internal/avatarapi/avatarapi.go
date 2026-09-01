// Package avatarapi serves the small square picture that stands for a subject:
// GET /subjects/{uid}/avatar, a ~320 px JPEG of the person's face (or of the
// cover photo somebody chose for them), cut server-side by the avatar package.
//
// It exists so the people index stops paying for its own crops. The grid used to
// fetch a whole-frame preview per tile and crop it in CSS, which for a face
// covering a few per cent of its photo meant megapixels of image for a 150 px
// square. This route hands over exactly the pixels the tile paints.
//
// The endpoint is a read for any signed-in user, mirroring the subject list it
// illustrates; the guard is injected so this package stays decoupled from auth's
// wiring, as does every store it reads through.
package avatarapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/avatar"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
)

// cacheControl is the caching policy for an avatar. Unlike a thumbnail it is
// *not* immutable: the URL names a subject, and which face (or cover photo)
// stands for that subject changes when a curator picks another one. So the
// picture is cached for ten minutes and revalidated after that against the ETag,
// which is keyed by the photo and the exact crop — a re-picked cover therefore
// costs one 304-sized request per tile, and a page redrawn within the window
// costs none at all. It is private because it is served only to authenticated
// callers.
const cacheControl = "private, max-age=600, must-revalidate"

// Subjects is the subset of people.Store the endpoint reads: which picture stands
// for a subject. It is an interface so the handler is unit-testable with a fake.
type Subjects interface {
	// SubjectAvatar returns the subject's cover photo or best face, or
	// people.ErrSubjectNotFound / people.ErrNoAvatar.
	SubjectAvatar(ctx context.Context, uid string) (people.AvatarSource, error)
}

// Photos is the subset of photos.Store the endpoint reads: the record behind the
// avatar's photo, which carries the file hash the cache is keyed by and the frame
// the crop is measured against.
type Photos interface {
	// GetByUID returns one photo or photos.ErrPhotoNotFound.
	GetByUID(ctx context.Context, uid string) (photos.Photo, error)
}

// Renderer cuts and caches the avatar itself. It is satisfied by
// *avatar.Renderer; the interface keeps the HTTP layer testable without a cache
// directory or a JPEG decoder.
type Renderer interface {
	// Open returns a reader over the rendered avatar and its ETag. The caller owns
	// the reader and must close it.
	Open(ctx context.Context, photo photos.Photo, face *avatar.Box) (io.ReadCloser, string, error)
}

// API exposes the subject avatar endpoint over HTTP.
type API struct {
	subjects    Subjects
	photos      Photos
	renderer    Renderer
	requireAuth func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. A nil Renderer makes the endpoint
// answer 503, which is what a build without a derived-media cache would do.
type Config struct {
	// Subjects resolves a subject to the picture that stands for it.
	Subjects Subjects
	// Photos resolves that picture's photo to its stored record.
	Photos Photos
	// Renderer cuts and caches the rendition that is served.
	Renderer Renderer
	// RequireAuth guards the endpoint for any signed-in user.
	RequireAuth func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{
		subjects:    cfg.Subjects,
		photos:      cfg.Photos,
		renderer:    cfg.Renderer,
		requireAuth: cfg.RequireAuth,
	}
}

// RegisterRoutes mounts the avatar endpoint onto r, which the caller has scoped
// under the API base path (for example /api/v1):
//
//	GET /subjects/{uid}/avatar  RequireAuth  the subject's square JPEG avatar
//
// A flat pattern (rather than a mounted subrouter) is used so this route can
// coexist on the same router with peopleapi's /subjects group and outlierapi's
// /subjects/{uid}/outliers without a chi Mount conflict.
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireAuth).Get("/subjects/{uid}/avatar", a.handleAvatar)
}

// handleAvatar streams the subject's avatar as a JPEG. A subject that does not
// exist, has no picture at all, or whose picture names a photo that has since
// gone answers 404 — the people index draws its placeholder for all three, so
// they are the same answer to it. A caller presenting the current ETag gets 304.
func (a *API) handleAvatar(w http.ResponseWriter, r *http.Request) {
	if a.renderer == nil {
		writeError(w, http.StatusServiceUnavailable, "avatars not available")
		return
	}
	source, err := a.subjects.SubjectAvatar(r.Context(), chi.URLParam(r, "uid"))
	if err != nil {
		writeSourceError(w, err)
		return
	}
	photo, err := a.photos.GetByUID(r.Context(), source.PhotoUID)
	if err != nil {
		writeSourceError(w, err)
		return
	}

	reader, etag, err := a.renderer.Open(r.Context(), photo, faceBox(source.Face))
	if err != nil {
		log.Printf("avatarapi: rendering avatar of %s: %v", source.PhotoUID, err)
		writeError(w, http.StatusInternalServerError, "avatar unavailable")
		return
	}
	defer func() { _ = reader.Close() }()

	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", cacheControl)
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	if _, err := io.Copy(w, reader); err != nil {
		// The response is already committed, so there is nothing to tell the
		// client; a dropped connection while a tile loads is entirely ordinary.
		log.Printf("avatarapi: streaming avatar of %s: %v", source.PhotoUID, err)
	}
}

// faceBox converts the store's face box into the renderer's, passing nil through
// — which is how a hand-picked cover photo says "show me whole".
func faceBox(face *people.Box) *avatar.Box {
	if face == nil {
		return nil
	}
	return &avatar.Box{X: face.X, Y: face.Y, W: face.W, H: face.H}
}

// writeSourceError maps a lookup failure to its response: everything that means
// "there is no picture for this subject" is a 404, anything else a 500 with a
// generic message.
func writeSourceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, people.ErrSubjectNotFound),
		errors.Is(err, people.ErrNoAvatar),
		errors.Is(err, photos.ErrPhotoNotFound):
		writeError(w, http.StatusNotFound, "subject has no avatar")
	default:
		log.Printf("avatarapi: resolving avatar source: %v", err)
		writeError(w, http.StatusInternalServerError, "avatar lookup failed")
	}
}

// errorBody is the JSON body returned for error responses.
type errorBody struct {
	Error string `json:"error"`
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(errorBody{Error: message}); err != nil {
		log.Printf("avatarapi: encoding JSON response: %v", err)
	}
}
