// Package resolveapi answers GET /resolve/{kind}/{uid}: which project an old,
// unprefixed link means. Every address the app wrote before projects existed —
// `/photos/<uid>` in a bookmark, `/tasks/<uid>` in an e-mail, a notification
// row stored before its link carried `/p/<slug>` — names an entity but not its
// project. Sending such a link to the reader's last project is a guess that is
// wrong as soon as they have two; the entity's own row knows better.
//
// The resolver tells the caller the slug only when the caller may enter that
// project (the project guard's rules: a maintainer everywhere, anybody else as
// an approved member of an active project). Everything else — an unknown kind,
// a uid nothing holds, a project the caller cannot enter, somebody else's
// notification — is one and the same 404 with one body, so it is no oracle of
// another project's contents. It is rate-limited per caller on top, so it
// cannot be swept either.
//
// Inside a project the caller may enter it says nothing the page itself would
// not: the page then applies its own visibility rules to the entity.
package resolveapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
)

// notFoundMessage is the body of every refusal; see the package comment.
const notFoundMessage = "not found"

// Resolver finds the project of an entity for a caller. Store satisfies it.
type Resolver interface {
	// ProjectSlug returns the slug of the project holding the kind entity uid
	// when caller may enter it, ErrNotFound otherwise.
	ProjectSlug(ctx context.Context, kind Kind, uid string, caller Caller) (string, error)
}

// Config bundles the dependencies of NewAPI.
type Config struct {
	// Resolver looks the entity up (required).
	Resolver Resolver
	// RequireSignedIn guards the route: any signed-in account, whatever project
	// it belongs to — the route itself names none (required).
	RequireSignedIn func(http.Handler) http.Handler
	// Throttle rate-limits the route; it is mounted after the guard, so a
	// keyed limiter sees the caller. Nil throttles nothing.
	Throttle func(http.Handler) http.Handler
	// Logger records the failures the handler hides behind a 500; nil uses
	// slog.Default().
	Logger *slog.Logger
}

// API serves the resolver.
type API struct {
	resolver        Resolver
	requireSignedIn func(http.Handler) http.Handler
	throttle        func(http.Handler) http.Handler
	log             *slog.Logger
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	a := &API{resolver: cfg.Resolver, requireSignedIn: cfg.RequireSignedIn, throttle: cfg.Throttle, log: cfg.Logger}
	if a.throttle == nil {
		a.throttle = func(next http.Handler) http.Handler { return next }
	}
	if a.log == nil {
		a.log = slog.Default()
	}
	return a
}

// RegisterRoutes mounts the instance-level route onto r (the API base path,
// /api/v1):
//
//	GET /resolve/{kind}/{uid}  RequireSignedIn  → {"slug": "<project>"} or the one 404
//
// kind is one of photo, album, label, task, subject, notification.
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireSignedIn, a.throttle).Get("/resolve/{kind}/{uid}", a.handleResolve)
}

// resolveResponse is the body of a successful resolve.
type resolveResponse struct {
	Slug string `json:"slug"`
}

// handleResolve answers which project the entity of the path lives in.
func (a *API) handleResolve(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	kind := Kind(chi.URLParam(r, "kind"))
	uid := chi.URLParam(r, "uid")
	caller := Caller{UserUID: user.UID, Maintainer: user.Role.CanMaintain()}
	slug, err := a.resolver.ProjectSlug(r.Context(), kind, uid, caller)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, notFoundMessage)
		return
	}
	if err != nil {
		a.log.ErrorContext(r.Context(), "resolveapi: resolving an old link", "kind", kind, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, resolveResponse{Slug: slug})
}

// writeJSON writes payload as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Warn("resolveapi: encoding JSON response", "error", err)
	}
}

// writeError writes the standard error envelope with the given status code.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
