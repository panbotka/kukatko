package auth

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// passkeyTargetType names the audited entity kind for passkey credentials.
//
//nolint:gosec // G101: the name of a table, not a credential.
const passkeyTargetType = "passkey_credentials"

// passkeyCeremonyCookieName is the HttpOnly cookie carrying the opaque id of the
// ceremony begun by the matching begin endpoint.
//
// A cookie rather than a field of the response body, for the same reason the
// session token is one: the id must not be readable by script running in the
// page, so that a cross-site scripting bug cannot lift an in-flight challenge and
// finish somebody's ceremony from elsewhere. It is scoped to the whole site
// because the two halves of a ceremony sit under different paths.
//
//nolint:gosec // G101: a cookie's name, not its value.
const passkeyCeremonyCookieName = "kukatko_passkey_ceremony"

// beginPasskeyCeremonyResponse is the body of both begin endpoints: the options
// object the browser passes straight to navigator.credentials, unreshaped — it
// nests under "publicKey" exactly as the WebAuthn API expects, so a client hands
// it on rather than rebuilding it. The ceremony id is not in it; that travels in
// the cookie.
type beginPasskeyCeremonyResponse struct {
	Options any `json:"options"`
}

// finishPasskeyRegistrationRequest is the JSON body of POST
// /auth/passkeys/register/finish: the authenticator's answer, plus the name the
// owner wants this key listed under. The credential is kept as raw JSON and
// handed to the WebAuthn parser unchanged — reshaping it would break the
// signature it is verified by.
type finishPasskeyRegistrationRequest struct {
	Name       string          `json:"name"`
	Credential json.RawMessage `json:"credential"`
}

// finishPasskeyLoginRequest is the JSON body of POST
// /auth/passkeys/login/finish, shaped like its registration counterpart so a
// client builds both the same way.
type finishPasskeyLoginRequest struct {
	Credential json.RawMessage `json:"credential"`
}

// listPasskeysResponse is returned by GET /auth/passkeys.
type listPasskeysResponse struct {
	Passkeys []PasskeyView `json:"passkeys"`
}

// passkeyLoginLimitKey namespaces the anonymous passkey sign-in inside the login
// rate limiter, which is reused here rather than standing up a second one, so
// the budget is exactly the password login's. The key is the client address
// alone: a discoverable login names no account until it has already succeeded,
// so there is nothing else to attribute an attempt to.
func passkeyLoginLimitKey(ip string) string {
	return "passkey:" + ip
}

// passkeyBeginLimitKey is the separate budget of the ceremony-opening half. It
// is its own key so that opening ceremonies cannot spend the budget that guards
// credential verification, and so that a person who starts a sign-in, changes
// their mind and starts another is not charged for the one they finish.
func passkeyBeginLimitKey(ip string) string {
	return "passkey-begin:" + ip
}

// passkeys returns the configured flow, or writes the "not available" response
// and reports false. Every passkey endpoint is mounted on every instance and
// answers this way when none is wired: a client must be able to tell an instance
// that does not offer passkeys from a build that does not know them.
func (a *API) passkeysOrError(w http.ResponseWriter) (*Passkeys, bool) {
	if a.passkeys == nil {
		writeError(w, http.StatusNotImplemented, ErrPasskeysUnavailable.Error())
		return nil, false
	}
	return a.passkeys, true
}

// setCeremonyCookie writes the ceremony id as an HttpOnly cookie that lives
// exactly as long as the ceremony it names.
func (a *API) setCeremonyCookie(w http.ResponseWriter, id string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     passkeyCeremonyCookieName,
		Value:    id,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}

// clearCeremonyCookie deletes the ceremony cookie, so a spent or refused
// ceremony does not linger in the browser to be sent with the next attempt.
func (a *API) clearCeremonyCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     passkeyCeremonyCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}

// ceremonyID returns the id of the ceremony this request claims to be finishing,
// or the empty string when the cookie is absent — which the flow refuses exactly
// as it refuses an unknown one.
func ceremonyID(r *http.Request) string {
	cookie, err := r.Cookie(passkeyCeremonyCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// handleBeginPasskeyRegistration starts adding a passkey to the authenticated
// caller's account and returns the creation options. It responds 200, 501 (the
// instance offers no passkeys), or 500.
func (a *API) handleBeginPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	passkeys, ok := a.passkeysOrError(w)
	if !ok {
		return
	}
	options, id, err := passkeys.BeginRegistration(r.Context(), p.user)
	if err != nil {
		log.Printf("auth: beginning passkey registration: %v", err)
		writeError(w, http.StatusInternalServerError, "could not start the passkey registration")
		return
	}
	a.setCeremonyCookie(w, id, passkeys.CeremonyTTL())
	writeJSON(w, http.StatusOK, beginPasskeyCeremonyResponse{Options: options})
}

// handleFinishPasskeyRegistration verifies the authenticator's answer and stores
// the credential for the authenticated caller. It responds 201 with the stored
// passkey, 400 (bad body, over-long name, or an answer that did not verify), 409
// (this credential is already registered), 501, or 500.
func (a *API) handleFinishPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	passkeys, ok := a.passkeysOrError(w)
	if !ok {
		return
	}
	var req finishPasskeyRegistrationRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	entry := audit.FromRequest(r, p.user.UID).Entry(audit.ActionPasskeyRegister, passkeyTargetType, "", nil)
	pk, err := passkeys.FinishRegistration(r.Context(), p.user, ceremonyID(r), req.Name, req.Credential, entry)
	a.clearCeremonyCookie(w)
	if err != nil {
		writePasskeyRegistrationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, pk.View())
}

// writePasskeyRegistrationError maps a failed registration onto its response.
// Everything the caller could fix is a 400 apart from the one case that is not a
// mistake at all — the same authenticator offered twice — which is a 409 so the
// interface can say "you already added this one".
func writePasskeyRegistrationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrPasskeyAlreadyRegistered):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrPasskeyCeremony), errors.Is(err, ErrPasskeyRejected),
		errors.Is(err, ErrPasskeyNameTooLong):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		log.Printf("auth: finishing passkey registration: %v", err)
		writeError(w, http.StatusInternalServerError, "could not save the passkey")
	}
}

// handleBeginPasskeyLogin starts an anonymous, discoverable sign-in ceremony and
// returns the assertion options. It is rate-limited per client address on its
// own budget (see passkeyBeginLimitKey) and responds 200, 429, 501, or 500.
func (a *API) handleBeginPasskeyLogin(w http.ResponseWriter, r *http.Request) {
	passkeys, ok := a.passkeysOrError(w)
	if !ok {
		return
	}
	if !a.limiter.Allow(passkeyBeginLimitKey(clientIP(r)), a.now()) {
		writeError(w, http.StatusTooManyRequests, "too many login attempts; try again later")
		return
	}
	options, id, err := passkeys.BeginLogin()
	if err != nil {
		log.Printf("auth: beginning passkey login: %v", err)
		writeError(w, http.StatusInternalServerError, "could not start the passkey sign-in")
		return
	}
	a.setCeremonyCookie(w, id, passkeys.CeremonyTTL())
	writeJSON(w, http.StatusOK, beginPasskeyCeremonyResponse{Options: options})
}

// handleFinishPasskeyLogin verifies the authenticator's assertion and, on
// success, sets the same session cookie a password login sets. It is rate-limited
// per client address on the password login's budget and responds 200 with the
// user and download token, 400 (bad body), 401 (the answer was refused), 403 (the
// account is waiting for approval), 429, 501, or 500.
func (a *API) handleFinishPasskeyLogin(w http.ResponseWriter, r *http.Request) {
	passkeys, ok := a.passkeysOrError(w)
	if !ok {
		return
	}
	key := passkeyLoginLimitKey(clientIP(r))
	if !a.limiter.Allow(key, a.now()) {
		writeError(w, http.StatusTooManyRequests, "too many login attempts; try again later")
		return
	}
	var req finishPasskeyLoginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	entry := audit.FromRequest(r, "").Entry(audit.ActionPasskeyLogin, passkeyTargetType, "", nil)
	sess, user, err := passkeys.FinishLogin(r.Context(), ceremonyID(r), req.Credential, entry)
	a.clearCeremonyCookie(w)
	if err != nil {
		writePasskeyLoginError(w, err)
		return
	}
	a.limiter.Reset(key)
	a.setSessionCookie(w, sess.Token, sess.ExpiresAt)
	writeJSON(w, http.StatusOK, loginResponse{User: user, DownloadToken: sess.DownloadToken})
}

// writePasskeyLoginError maps a failed passkey sign-in onto its response, along
// the same lines as writeLoginError: a refused answer is a 401 and says nothing
// about why, while an account waiting for an administrator is a 403 — only ever
// reached by somebody whose authenticator already signed the challenge.
func writePasskeyLoginError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrPasskeyRejected), errors.Is(err, ErrPasskeyCeremony):
		writeError(w, http.StatusUnauthorized, ErrPasskeyRejected.Error())
	case errors.Is(err, ErrNotApproved):
		writeError(w, http.StatusForbidden, ErrNotApproved.Error())
	default:
		log.Printf("auth: passkey login failed unexpectedly: %v", err)
		writeError(w, http.StatusInternalServerError, "sign-in failed")
	}
}

// handleListPasskeys returns the authenticated caller's own passkeys, newest
// first. It responds 200, 501, or 500.
func (a *API) handleListPasskeys(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	passkeys, ok := a.passkeysOrError(w)
	if !ok {
		return
	}
	stored, err := passkeys.List(r.Context(), p.user.UID)
	if err != nil {
		log.Printf("auth: listing passkeys: %v", err)
		writeError(w, http.StatusInternalServerError, "could not list passkeys")
		return
	}
	views := make([]PasskeyView, 0, len(stored))
	for _, pk := range stored {
		views = append(views, pk.View())
	}
	writeJSON(w, http.StatusOK, listPasskeysResponse{Passkeys: views})
}

// handleDeletePasskey removes one of the caller's own passkeys. Somebody else's
// is a 404, not a 403, so a caller cannot probe which ids exist; removing the
// last one is allowed, because the password is always still there. It responds
// 204, 404, 501, or 500.
func (a *API) handleDeletePasskey(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	passkeys, ok := a.passkeysOrError(w)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	entry := audit.FromRequest(r, p.user.UID).Entry(audit.ActionPasskeyDelete, passkeyTargetType, id, nil)
	err := passkeys.Delete(r.Context(), id, p.user, entry)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, ErrPasskeyNotFound):
		writeError(w, http.StatusNotFound, "passkey not found")
	default:
		log.Printf("auth: deleting passkey: %v", err)
		writeError(w, http.StatusInternalServerError, "could not delete the passkey")
	}
}
