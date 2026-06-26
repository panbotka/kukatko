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

// downloadTokenParam is the query parameter carrying a session's media download
// token on cookie-less media URLs (thumbnails, originals, video streams).
const downloadTokenParam = "t"

// RequireAuthOrDownloadToken wraps next so it runs for any authenticated caller,
// accepting either the session cookie or, failing that, a valid download token in
// the "t" query parameter. The authenticated principal is placed on the request
// context just as RequireAuth does. Requests with neither a valid cookie nor a
// valid token get 401. It guards media endpoints so a browser <img>/<video> tag
// can fetch protected thumbnails and originals without relying on the cookie.
func (a *API) RequireAuthOrDownloadToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := a.authenticateRequest(r)
		if err != nil {
			p, err = a.authenticateDownloadToken(r)
		}
		if err != nil {
			a.clearExpiredCookie(w, err)
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), p)))
	})
}

// authenticateDownloadToken reads the download token from the request's "t"
// query parameter and validates it through the service, returning the
// authenticated principal. A missing token is reported as ErrSessionNotFound.
func (a *API) authenticateDownloadToken(r *http.Request) (principal, error) {
	token := r.URL.Query().Get(downloadTokenParam)
	if token == "" {
		return principal{}, ErrSessionNotFound
	}
	user, sess, err := a.svc.AuthenticateDownloadToken(r.Context(), token)
	if err != nil {
		return principal{}, err
	}
	return principal{user: user, session: sess}, nil
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
