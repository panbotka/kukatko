package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/panbotka/kukatko/internal/clientip"
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

// subjectRequest is the JSON body of PUT /auth/subject: which person of the
// library the caller is. A null (or empty) subject_uid clears the link, which is
// how a user says "never mind, that is not me".
type subjectRequest struct {
	SubjectUID *string `json:"subject_uid"`
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

// clientIP returns the address the request is attributed to: the socket peer,
// unless a *trusted* proxy named a different client in a forwarding header (see
// internal/clientip). Keying the login limiter on this rather than on a header
// anyone can set is what makes the per-IP half of the key mean anything.
func clientIP(r *http.Request) string {
	return clientip.FromRequest(r)
}

// handleLogin authenticates a username/password, enforces login rate limiting
// both per username+IP and per username alone, and on success sets the session
// cookie and returns the user plus download token. It responds 400 (bad body or
// over-long username), 429 (rate limited), 401 (bad credentials), 403 (the
// account is waiting for approval), or 500 (server error).
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

	key := loginLimitKey(username, r)
	if !a.allowLoginAttempt(key, username) {
		writeError(w, http.StatusTooManyRequests, "too many login attempts; try again later")
		return
	}

	sess, user, err := a.svc.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		writeLoginError(w, err)
		return
	}

	a.limiter.Reset(key)
	a.usernameLimiter.Reset(username)
	a.setSessionCookie(w, sess.Token, sess.ExpiresAt)
	writeJSON(w, http.StatusOK, loginResponse{User: user, DownloadToken: sess.DownloadToken})
}

// writeLoginError maps a failed sign-in onto its response. The credentials being
// wrong is a 401; an account that exists, whose password was right and that
// nobody has approved yet is a 403 — its own outcome, so the sign-in screen can
// say "you are waiting for an administrator" instead of blaming the password.
// Anything else is a server fault and is logged.
func writeLoginError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid username or password")
	case errors.Is(err, ErrNotApproved):
		writeError(w, http.StatusForbidden, ErrNotApproved.Error())
	default:
		log.Printf("auth: login failed unexpectedly: %v", err)
		writeError(w, http.StatusInternalServerError, "login failed")
	}
}

// loginLimitKey is the bucket key of the per-(username, address) login budget.
// The address half comes from clientIP, so a caller that rotates a forwarding
// header keeps hitting the same bucket instead of minting a fresh one per
// request (SEC-001).
func loginLimitKey(username string, r *http.Request) string {
	return username + "|" + clientIP(r)
}

// allowLoginAttempt records one login attempt against both budgets and reports
// whether it may proceed. The per-(username, IP) budget is the narrow one; the
// per-username budget is what an attacker cannot escape by changing address,
// which — now that a forwarding header no longer decides that address (SEC-001)
// — means it cannot be escaped at all.
//
// Both budgets are charged whenever the request gets that far, so an attacker
// spreading over many addresses still burns the account's budget, and a
// successful login clears both.
func (a *API) allowLoginAttempt(key, username string) bool {
	now := a.now()
	perAddress := a.limiter.Allow(key, now)
	perUsername := a.usernameLimiter.Allow(username, now)
	return perAddress && perUsername
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

// handleSubject records which person of the library the caller is, or clears
// that link when the body carries no subject. It is self-service — a user may
// only ever say this about their own account, and the acting account is taken
// from the session rather than from the body, so there is nothing to point at
// somebody else.
//
// It responds 200 with the refreshed user (the client re-renders the "my photos"
// entry and the avatar from it), 400 for a bad body or a subject that does not
// exist, 404 if the account has since been deleted, or 500.
//
// The change is deliberately not audited, following POST /auth/password: the
// audit trail records what was done *to* an account by somebody else, and an
// administrator setting this for a user does write an entry.
func (a *API) handleSubject(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req subjectRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user, err := a.svc.SetUserSubject(r.Context(), p.user.UID, req.SubjectUID)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, user)
	case errors.Is(err, ErrSubjectNotFound):
		writeError(w, http.StatusBadRequest, ErrSubjectNotFound.Error())
	case errors.Is(err, ErrUserNotFound):
		writeError(w, http.StatusNotFound, "user not found")
	default:
		writeError(w, http.StatusInternalServerError, "could not save the linked person")
	}
}

// handleWelcomeSeen records that the caller has seen the first-run welcome. It
// is self-service and self-scoped: the account written to is the session's, so
// it needs no role beyond being signed in and there is nothing to point at
// somebody else.
//
// It responds 200 with the refreshed user, so the client can read the stamp back
// without a second round trip to /auth/me. Sending it twice is harmless — the
// second call returns the first call's timestamp unchanged — which is what lets
// a client fire it without tracking whether it already has. It answers 404 if
// the account has since been deleted, or 500.
func (a *API) handleWelcomeSeen(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	user, err := a.svc.MarkWelcomeSeen(r.Context(), p.user.UID)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, user)
	case errors.Is(err, ErrUserNotFound):
		writeError(w, http.StatusNotFound, "user not found")
	default:
		writeError(w, http.StatusInternalServerError, "could not record the welcome")
	}
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
