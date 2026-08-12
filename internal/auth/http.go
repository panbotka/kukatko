package auth

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/panbotka/kukatko/internal/obs"
)

// sessionCookieName is the name of the HttpOnly cookie carrying the opaque
// session token.
const sessionCookieName = "kukatko_session"

// usernameLimitFactor scales the per-(username, IP) failure budget into the
// IP-independent per-username one when the caller does not supply its own
// limiter. The per-username counter is what survives an attacker moving between
// addresses, but it is also the one an attacker can aim at somebody else's
// account to lock them out, so it is deliberately looser than the per-IP budget
// rather than equal to it: a person mistyping their own password from one
// machine hits the per-IP limit first and never learns this one exists.
const usernameLimitFactor = 3

// API exposes the auth domain over HTTP: it registers the /auth and admin
// /users routes and provides the RBAC middleware. It bundles the service, the
// two login rate limiters, and cookie settings.
type API struct {
	svc             *Service
	limiter         *Limiter
	usernameLimiter *Limiter
	secureCookies   bool
	now             func() time.Time
}

// APIConfig configures NewAPI.
type APIConfig struct {
	// Service is the auth domain service (required).
	Service *Service
	// Limiter throttles login attempts per (username, client IP) — and, reused,
	// API-token minting per (user, client IP) (required).
	Limiter *Limiter
	// UsernameLimiter throttles login attempts per username regardless of where
	// they come from. Optional: when nil, NewAPI derives one from Limiter with a
	// budget usernameLimitFactor times as large over the same window.
	UsernameLimiter *Limiter
	// SecureCookies marks the session cookie Secure (HTTPS-only).
	SecureCookies bool
}

// NewAPI returns an API from cfg, using time.Now as its clock.
func NewAPI(cfg APIConfig) *API {
	return &API{
		svc:             cfg.Service,
		limiter:         cfg.Limiter,
		usernameLimiter: usernameLimiterFor(cfg),
		secureCookies:   cfg.SecureCookies,
		now:             time.Now,
	}
}

// usernameLimiterFor returns the per-username limiter cfg asks for, deriving one
// from the per-IP limiter when the caller supplied none.
func usernameLimiterFor(cfg APIConfig) *Limiter {
	if cfg.UsernameLimiter != nil {
		return cfg.UsernameLimiter
	}
	if cfg.Limiter == nil {
		return NewLimiter(usernameLimitFactor, time.Minute)
	}
	return NewLimiter(cfg.Limiter.max*usernameLimitFactor, cfg.Limiter.window)
}

// RunMaintenance periodically prunes both login rate limiters' stale keys until
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
			now := a.now()
			a.limiter.Cleanup(now)
			a.usernameLimiter.Cleanup(now)
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

// withPrincipal returns a copy of ctx carrying the authenticated principal and
// stamps the user's UID onto the request's observability fields so the
// access-log line can attribute the request to its caller.
func withPrincipal(ctx context.Context, p principal) context.Context {
	obs.SetUser(ctx, p.user.UID)
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
