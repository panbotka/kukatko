package auth

import (
	"net/http"
)

// RequireAuth wraps next so it runs only for requests carrying a valid session
// cookie; the authenticated user and session are placed in the request context.
// Unauthenticated requests get 401.
func (a *API) RequireAuth(next http.Handler) http.Handler {
	return a.requireRole(requireAuth, next)
}

// RequireWrite wraps next so it runs only for authenticated users with write
// access (editor or admin). Viewers get 403; unauthenticated requests get 401.
func (a *API) RequireWrite(next http.Handler) http.Handler {
	return a.requireRole(requireWrite, next)
}

// RequireAdmin wraps next so it runs only for authenticated admins. Non-admins
// get 403; unauthenticated requests get 401.
func (a *API) RequireAdmin(next http.Handler) http.Handler {
	return a.requireRole(requireAdmin, next)
}

// requireRole returns a middleware that authenticates the request, enforces req
// against the user's role, attaches the principal to the context, and calls
// next. It writes 401 when authentication fails and 403 when the role is
// insufficient.
func (a *API) requireRole(req requirement, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := a.authenticateRequest(r)
		if err != nil {
			a.clearExpiredCookie(w, err)
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if !authorize(p.user.Role, req) {
			writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
		next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), p)))
	})
}

// authenticateRequest reads the session cookie and validates it through the
// service, returning the authenticated principal. A missing cookie is reported
// as ErrSessionNotFound; other validation failures propagate from the service.
func (a *API) authenticateRequest(r *http.Request) (principal, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return principal{}, ErrSessionNotFound
	}
	user, sess, err := a.svc.Authenticate(r.Context(), cookie.Value)
	if err != nil {
		return principal{}, err
	}
	return principal{user: user, session: sess}, nil
}
