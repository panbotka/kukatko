package auth

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// sessionCookieName is the name of the HttpOnly cookie carrying the opaque
// session token.
const sessionCookieName = "kukatko_session"

// API exposes the auth domain over HTTP: it registers the /auth and admin
// /users routes and provides the RBAC middleware. It bundles the service, the
// login rate limiter, and cookie settings.
type API struct {
	svc           *Service
	limiter       *Limiter
	secureCookies bool
	now           func() time.Time
}

// APIConfig configures NewAPI.
type APIConfig struct {
	// Service is the auth domain service (required).
	Service *Service
	// Limiter throttles login attempts (required).
	Limiter *Limiter
	// SecureCookies marks the session cookie Secure (HTTPS-only).
	SecureCookies bool
}

// NewAPI returns an API from cfg, using time.Now as its clock.
func NewAPI(cfg APIConfig) *API {
	return &API{
		svc:           cfg.Service,
		limiter:       cfg.Limiter,
		secureCookies: cfg.SecureCookies,
		now:           time.Now,
	}
}

// RunMaintenance periodically prunes the login rate limiter's stale keys until
// ctx is canceled. It is meant to run in its own goroutine alongside the
// service's session cleanup.
func (a *API) RunMaintenance(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.limiter.Cleanup(a.now())
		}
	}
}

// principal is the authenticated identity attached to a request's context by the
// RBAC middleware.
type principal struct {
	user    User
	session Session
}

// contextKey is an unexported type for context keys defined in this package, so
// they cannot collide with keys from other packages.
type contextKey int

const principalContextKey contextKey = iota

// withPrincipal returns a copy of ctx carrying the authenticated principal.
func withPrincipal(ctx context.Context, p principal) context.Context {
	return context.WithValue(ctx, principalContextKey, p)
}

// principalFromContext extracts the authenticated principal placed by the RBAC
// middleware, reporting whether one was present.
func principalFromContext(ctx context.Context) (principal, bool) {
	p, ok := ctx.Value(principalContextKey).(principal)
	return p, ok
}

// UserFromContext returns the authenticated user attached to ctx by RequireAuth
// (or a stricter middleware), reporting whether one was present. Downstream
// handlers use it to identify the caller.
func UserFromContext(ctx context.Context) (User, bool) {
	p, ok := principalFromContext(ctx)
	return p.user, ok
}

// SessionFromContext returns the authenticated session attached to ctx,
// reporting whether one was present.
func SessionFromContext(ctx context.Context) (Session, bool) {
	p, ok := principalFromContext(ctx)
	return p.session, ok
}

// errorResponse is the JSON body returned for error responses.
type errorResponse struct {
	Error string `json:"error"`
}

// writeJSON writes payload as JSON with the given status code. An encoding
// failure can only be logged because the status line is already flushed.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("auth: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}
