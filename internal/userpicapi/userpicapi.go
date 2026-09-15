// Package userpicapi is the HTTP surface of a user's profile picture: the one
// route that serves it, and the three that change it.
//
//	GET    /users/{uid}/avatar   RequireAuth   the account's square JPEG, or 404
//	GET    /auth/picture         RequireAuth   which source is answering for me
//	PUT    /auth/picture         RequireAuth   upload one, or point at a photo
//	DELETE /auth/picture         RequireAuth   clear mine
//
// The serving route deliberately has the same shape as the subject avatar it
// grew out of (internal/avatarapi): a JPEG, an ETag answering 304 on
// revalidation, a private cache — ten minutes for somebody else's picture, none
// at all for the caller's own, see ownCacheControl — and, for an account with no
// picture from any source, a 404, which is the client's cue to draw the coloured
// initial. Nothing in the response says which of the three sources answered; the
// chain is the server's business, and a client that had to know would have to
// re-implement it.
//
// The three writing routes are self-service and self-scoped: the account they
// change is the session's, taken from the principal and never from the request,
// so there is nothing to point at somebody else. None of them is audited,
// following POST /auth/password — the audit trail records what was done *to* an
// account by somebody else, and a person changing their own profile is not that.
package userpicapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/avatar"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/userpic"
)

// cacheControl is the caching policy for somebody else's profile picture. Like a
// subject avatar and unlike a thumbnail it is *not* immutable: the URL names an
// account, and what stands for that account changes when its owner uploads
// another picture or links themselves to a different person. So it is cached for
// ten minutes and revalidated after that against the ETag — a changed picture
// costs one 304-sized request per reader, and a comment thread redrawn inside
// the window costs none at all. It is private because it is served only to
// authenticated callers.
const cacheControl = "private, max-age=600, must-revalidate"

// ownCacheControl is the policy for the picture of the account asking for it.
// That one picture is the only one that changes under its own reader: nobody
// else's account is edited on the page they happen to be looking at, but theirs
// is — they set it a moment ago on /account, and the bar at the top of that very
// page draws it. Ten minutes of freshness there means the change they just made
// appears not to have happened, and goes on not having happened across a reload,
// because must-revalidate only governs what a *stale* entry may do.
//
// So their own picture is never reused without asking, and the ETag does the
// deciding it was already computing. The cost is one conditional request per
// page load, answered 304 and empty for the picture that has not changed;
// everybody else's avatar in a thread still costs nothing.
const ownCacheControl = "private, max-age=0, must-revalidate"

// uploadField is the multipart form field an uploaded picture arrives in. It is
// singular where the library's own upload endpoint takes "files": a person has
// one profile picture, and accepting a list would only raise the question of
// which one won.
const uploadField = "picture"

// Pictures is the chain this API serves and edits. It is an interface so the
// handlers are unit-testable without a database; it is satisfied by
// *userpic.Service.
type Pictures interface {
	// Resolve returns the first answer of the chain, or userpic.ErrNoPicture.
	Resolve(ctx context.Context, userUID string) (userpic.Source, userpic.Origin, error)
	// Describe reports which source currently answers, without the bytes.
	Describe(ctx context.Context, userUID string) (userpic.State, error)
	// SetUpload normalizes and stores submitted image bytes.
	SetUpload(ctx context.Context, userUID string, data []byte) error
	// SetPhoto points the account's picture at a library photo.
	SetPhoto(ctx context.Context, userUID, photoUID string) error
	// Clear removes the stored picture.
	Clear(ctx context.Context, userUID string) error
}

// Photos resolves a picked or inherited photo to its stored record, which
// carries the file hash the rendition cache is keyed by and the frame the face
// box is measured against. It is satisfied by *photos.Store.
type Photos interface {
	// GetByUID returns one photo or photos.ErrPhotoNotFound.
	GetByUID(ctx context.Context, uid string) (photos.Photo, error)
}

// Renderer cuts and caches the square rendition of a photo. It is the very
// renderer the subject avatar uses — the picked-photo and inherited-face cases
// are the same crop, so they must not grow a second cropper. Satisfied by
// *avatar.Renderer.
type Renderer interface {
	// Open returns a reader over the rendered avatar and its ETag. The caller
	// owns the reader and must close it.
	Open(ctx context.Context, photo photos.Photo, face *avatar.Box) (io.ReadCloser, string, error)
}

// API exposes the profile picture endpoints over HTTP.
type API struct {
	pictures    Pictures
	photos      Photos
	renderer    Renderer
	requireAuth func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. A nil Renderer makes the serving
// route answer 503 for anything that has to be cut out of a photograph, which is
// what a build without a derived-media cache would do; an uploaded picture is
// unaffected, since it is stored ready to serve.
type Config struct {
	// Pictures resolves and edits the chain.
	Pictures Pictures
	// Photos resolves a photo uid to its stored record.
	Photos Photos
	// Renderer cuts the rendition served for a picked or inherited photo.
	Renderer Renderer
	// RequireAuth guards every route here for any signed-in user.
	RequireAuth func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{
		pictures:    cfg.Pictures,
		photos:      cfg.Photos,
		renderer:    cfg.Renderer,
		requireAuth: cfg.RequireAuth,
	}
}

// RegisterRoutes mounts the profile picture endpoints onto r, which the caller
// has scoped under the API base path (for example /api/v1). The routes are flat
// patterns rather than mounted subrouters so /auth/picture can coexist with
// auth's own /auth group on the same router without a chi Mount conflict.
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireAuth).Get("/users/{uid}/avatar", a.handleAvatar)
	r.With(a.requireAuth).Get("/auth/picture", a.handleDescribe)
	r.With(a.requireAuth).Put("/auth/picture", a.handleSet)
	r.With(a.requireAuth).Delete("/auth/picture", a.handleClear)
}

// handleAvatar streams the account's picture as a JPEG. An account that does not
// exist, has no picture from any source, or whose picked photo has since gone
// all answer 404 — every client draws the coloured initial for all three, so
// they are one answer to it. A caller presenting the current ETag gets 304.
func (a *API) handleAvatar(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	source, _, err := a.pictures.Resolve(r.Context(), uid)
	if err != nil {
		writeResolveError(w, err)
		return
	}
	policy := cachePolicy(r, uid)
	if source.Upload != nil {
		serveBytes(w, r, source.Upload, policy)
		return
	}
	a.servePhoto(w, r, source, policy)
}

// cachePolicy picks how long this answer may be reused without asking again: the
// caller's own picture never is, anybody else's for ten minutes. See
// ownCacheControl for why the two differ. A request with no principal — which
// RequireAuth does not let through — is nobody's own picture.
func cachePolicy(r *http.Request, uid string) string {
	if user, ok := auth.UserFromContext(r.Context()); ok && user.UID == uid {
		return ownCacheControl
	}
	return cacheControl
}

// servePhoto cuts the picture out of a library photo and streams it, which is
// the path a picked photo and an inherited face share.
func (a *API) servePhoto(w http.ResponseWriter, r *http.Request, source userpic.Source, policy string) {
	if a.renderer == nil {
		writeError(w, http.StatusServiceUnavailable, "profile pictures not available")
		return
	}
	photo, err := a.photos.GetByUID(r.Context(), source.PhotoUID)
	if err != nil {
		writeResolveError(w, err)
		return
	}
	reader, etag, err := a.renderer.Open(r.Context(), photo, faceBox(source.Face))
	if err != nil {
		log.Printf("userpicapi: rendering picture from %s: %v", source.PhotoUID, err)
		writeError(w, http.StatusInternalServerError, "profile picture unavailable")
		return
	}
	defer func() { _ = reader.Close() }()

	if writeCacheHeaders(w, r, etag, policy) {
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	if _, err := io.Copy(w, reader); err != nil {
		// The response is already committed, so there is nothing to tell the
		// client; a dropped connection while an avatar loads is entirely ordinary.
		log.Printf("userpicapi: streaming picture from %s: %v", source.PhotoUID, err)
	}
}

// serveBytes streams an uploaded picture straight from the row it is stored in.
// Its ETag is a digest of the bytes themselves, so replacing the picture
// invalidates every cached copy of it and re-uploading the identical file
// invalidates none.
func serveBytes(w http.ResponseWriter, r *http.Request, data []byte, policy string) {
	digest := sha256.Sum256(data)
	if writeCacheHeaders(w, r, strconv.Quote("up-"+hex.EncodeToString(digest[:])), policy) {
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if _, err := io.Copy(w, bytes.NewReader(data)); err != nil {
		log.Printf("userpicapi: streaming uploaded picture: %v", err)
	}
}

// writeCacheHeaders stamps the validators onto the response and reports whether
// the request was answered with 304, in which case the caller must write no body.
func writeCacheHeaders(w http.ResponseWriter, r *http.Request, etag, policy string) bool {
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", policy)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	return false
}

// handleDescribe reports which source of the chain currently answers for the
// caller, so the account page can label the picture it shows and say what
// clearing it would fall back to. It never returns the bytes.
func (a *API) handleDescribe(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	state, err := a.pictures.Describe(r.Context(), user.UID)
	if err != nil {
		log.Printf("userpicapi: describing picture of %s: %v", user.UID, err)
		writeError(w, http.StatusInternalServerError, "could not read the profile picture")
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// pickRequest is the JSON body of a PUT that points the picture at a library
// photo, rather than uploading one.
type pickRequest struct {
	PhotoUID string `json:"photo_uid"`
}

// handleSet replaces the caller's own picture. The two ways to give one are told
// apart by the request's content type, because they are genuinely different
// payloads and not two encodings of one: a multipart body carries an image to
// keep, a JSON body carries the uid of a photograph the library already holds.
//
// It answers 204, 400 for an unreadable body, an undecodable picture or a photo
// that may not be one (private or hidden), 413 for an over-large upload, and 500
// otherwise.
func (a *API) handleSet(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "a Content-Type is required")
		return
	}
	if mediaType == "multipart/form-data" {
		a.setUpload(w, r, user.UID)
		return
	}
	a.setPicked(w, r, user.UID)
}

// setUpload reads the submitted picture out of the multipart body and stores the
// re-encoded square this package's service makes of it.
//
// The declared length is checked before the body is touched and the body is
// wrapped in a MaxBytesReader regardless, so an over-large upload is refused
// rather than buffered — a client that lies about Content-Length is stopped by
// the second bound, and one that tells the truth never reaches it.
func (a *API) setUpload(w http.ResponseWriter, r *http.Request, userUID string) {
	if r.ContentLength > userpic.MaxUploadBytes {
		writeError(w, http.StatusRequestEntityTooLarge, userpic.ErrTooLarge.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, userpic.MaxUploadBytes+1)
	file, _, err := r.FormFile(uploadField)
	if err != nil {
		writeError(w, http.StatusBadRequest, "the request carries no "+uploadField+" file")
		return
	}
	defer func() { _ = file.Close() }()

	data, err := userpic.ReadUpload(file)
	if err != nil {
		writeSetError(w, err)
		return
	}
	if err := a.pictures.SetUpload(r.Context(), userUID, data); err != nil {
		writeSetError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setPicked points the caller's picture at a photograph of the library.
func (a *API) setPicked(w http.ResponseWriter, r *http.Request, userUID string) {
	var req pickRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.PhotoUID == "" {
		writeError(w, http.StatusBadRequest, "photo_uid is required")
		return
	}
	if err := a.pictures.SetPhoto(r.Context(), userUID, req.PhotoUID); err != nil {
		writeSetError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleClear removes the caller's own stored picture. It is idempotent — an
// account with nothing stored is already in the state being asked for — and it
// does not necessarily restore the coloured initial: a linked account falls back
// to that person's face, which is the chain's default rather than a leftover.
func (a *API) handleClear(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if err := a.pictures.Clear(r.Context(), user.UID); err != nil {
		log.Printf("userpicapi: clearing picture of %s: %v", user.UID, err)
		writeError(w, http.StatusInternalServerError, "could not clear the profile picture")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// faceBox converts the chain's face box into the renderer's, passing nil through
// — which is how an uploaded pick and a hand-picked cover both say "show me
// whole".
func faceBox(face *userpic.Box) *avatar.Box {
	if face == nil {
		return nil
	}
	return &avatar.Box{X: face.X, Y: face.Y, W: face.W, H: face.H}
}

// writeResolveError maps a lookup failure to its response: everything that means
// "there is no picture for this account" is a 404, anything else a 500 with a
// generic message.
func writeResolveError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, userpic.ErrNoPicture), errors.Is(err, photos.ErrPhotoNotFound):
		writeError(w, http.StatusNotFound, "user has no picture")
	default:
		log.Printf("userpicapi: resolving picture: %v", err)
		writeError(w, http.StatusInternalServerError, "profile picture lookup failed")
	}
}

// writeSetError maps a failed write to its response. Every rejected *input* is a
// 400 — an undecodable picture and a photo that may not be one are both "what
// you sent cannot be a profile picture" — except an over-large upload, which has
// its own status so a client can tell "too big" from "wrong sort of file".
func writeSetError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, userpic.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, userpic.ErrTooLarge.Error())
	case errors.Is(err, userpic.ErrUnsupportedFormat):
		writeError(w, http.StatusBadRequest, userpic.ErrUnsupportedFormat.Error())
	case errors.Is(err, userpic.ErrPhotoNotAllowed):
		writeError(w, http.StatusBadRequest, userpic.ErrPhotoNotAllowed.Error())
	case errors.Is(err, userpic.ErrPhotoNotFound):
		writeError(w, http.StatusBadRequest, userpic.ErrPhotoNotFound.Error())
	default:
		log.Printf("userpicapi: setting picture: %v", err)
		writeError(w, http.StatusInternalServerError, "could not save the profile picture")
	}
}

// errorBody is the JSON body returned for error responses.
type errorBody struct {
	Error string `json:"error"`
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

// writeJSON writes v as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("userpicapi: encoding JSON response: %v", err)
	}
}
