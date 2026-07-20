package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
)

// maxBodyBytes bounds the size of decoded JSON request bodies to guard against
// oversized payloads.
const maxBodyBytes = 1 << 20 // 1 MiB

// loginRequest is the JSON body of POST /auth/login.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// loginResponse is returned by login and /auth/me: the authenticated user plus
// the session's separate media download token.
type loginResponse struct {
	User          User   `json:"user"`
	DownloadToken string `json:"download_token"`
}

// changePasswordRequest is the JSON body of POST /auth/password.
type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// decodeJSON reads a JSON request body into dst, enforcing a size limit and
// rejecting unknown fields. It returns an error suitable for a 400 response.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("auth: decoding request body: %w", err)
	}
	return nil
}

// clientIP returns the request's client IP without the port, falling back to the
// raw RemoteAddr when it has no port. chi's RealIP middleware has already
// resolved proxy headers into RemoteAddr.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// handleLogin authenticates a username/password, enforces login rate limiting
// per username+IP, and on success sets the session cookie and returns the user
// plus download token. It responds 400 (bad body or over-long username), 429
// (rate limited), 401 (bad credentials), or 500 (server error).
//
// The username length is checked before it is used, so this public endpoint
// cannot be flooded with oversized usernames to grow the rate limiter's keys.
func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	username := normalizeUsername(req.Username)
	if err := validateUsername(username); err != nil {
		writeError(w, http.StatusBadRequest, ErrUsernameTooLong.Error())
		return
	}

	key := username + "|" + clientIP(r)
	if !a.limiter.Allow(key, a.now()) {
		writeError(w, http.StatusTooManyRequests, "too many login attempts; try again later")
		return
	}

	sess, user, err := a.svc.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "invalid username or password")
			return
		}
		log.Printf("auth: login failed unexpectedly: %v", err)
		writeError(w, http.StatusInternalServerError, "login failed")
		return
	}

	a.limiter.Reset(key)
	a.setSessionCookie(w, sess.Token, sess.ExpiresAt)
	writeJSON(w, http.StatusOK, loginResponse{User: user, DownloadToken: sess.DownloadToken})
}

// handleLogout deletes the current session and clears the cookie. It is
// idempotent and always responds 204, even without a valid session.
func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if err := a.svc.Logout(r.Context(), cookie.Value); err != nil {
			writeError(w, http.StatusInternalServerError, "logout failed")
			return
		}
	}
	a.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// handleMe returns the authenticated user and the session download token. It
// runs behind RequireAuth, so the principal is always present.
func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	writeJSON(w, http.StatusOK, loginResponse{User: p.user, DownloadToken: p.session.DownloadToken})
}

// handlePassword changes the authenticated user's password and invalidates their
// other sessions (the current session is kept). It responds 400 (bad body or
// weak new password), 401 (wrong current password), 204 (success), or 500.
func (a *API) handlePassword(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req changePasswordRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	err := a.svc.ChangePassword(r.Context(), p.user.UID, p.session.Token, req.CurrentPassword, req.NewPassword)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "current password is incorrect")
	case errors.Is(err, ErrPasswordTooShort):
		writeError(w, http.StatusBadRequest, ErrPasswordTooShort.Error())
	default:
		writeError(w, http.StatusInternalServerError, "password change failed")
	}
}
