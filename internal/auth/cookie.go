package auth

import (
	"errors"
	"net/http"
	"time"
)

// setSessionCookie writes the session token as an HttpOnly, SameSite=Strict
// cookie scoped to the whole site. Expires mirrors the session expiry so the
// browser drops the cookie when the session can no longer be valid; the Secure
// flag follows the configured policy.
func (a *API) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}

// clearSessionCookie overwrites the session cookie with an expired, empty value
// so the browser deletes it (used on logout).
func (a *API) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}

// clearExpiredCookie deletes the client's session cookie when the failure was an
// expired or disabled session, so a stale cookie does not linger; for other
// errors (missing cookie, unknown token) it leaves the request untouched.
func (a *API) clearExpiredCookie(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrSessionExpired) || errors.Is(err, ErrUserDisabled) {
		a.clearSessionCookie(w)
	}
}
