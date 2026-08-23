package auth

import (
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// passwordResetRequest is the JSON body of POST /auth/password-reset/{token}:
// the password the person chose. Nothing else — the account is named by the
// link, never by the body, so a caller cannot point a token at somebody else.
type passwordResetRequest struct {
	Password string `json:"password"`
}

// handleIssuePasswordReset starts a password reset for one account (admin only):
// it mints a one-time link, invalidates any earlier unused one, enqueues the mail
// carrying it and answers with the link itself, so an administrator can also send
// it by hand.
//
// It responds 200 with `{reset_url, expires_at, email}`, 403 when the maintainer
// boundary forbids the actor this account (setting a maintainer's password —
// which is what the link does — is maintainer-only), 404 for an account that does
// not exist, 409 for a blocked one (a link it could never use would only make
// somebody wait), or 500.
func (a *API) handleIssuePasswordReset(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	actor, _ := UserFromContext(r.Context())
	entry := adminAuditMeta(r).Entry(audit.ActionUserPasswordReset, userTargetType, uid, nil)
	issued, err := a.passwordReset.Issue(r.Context(), uid, actor.Role, entry)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, issued)
	case errors.Is(err, ErrMaintainerRequired):
		writeError(w, http.StatusForbidden, ErrMaintainerRequired.Error())
	case errors.Is(err, ErrUserDisabled):
		writeError(w, http.StatusConflict, ErrUserDisabled.Error())
	case errors.Is(err, ErrUserNotFound):
		writeError(w, http.StatusNotFound, "user not found")
	default:
		log.Printf("auth: issuing a password reset failed: %v", err)
		writeError(w, http.StatusInternalServerError, "could not start a password reset")
	}
}

// handlePasswordResetStatus reports whether a reset link is still usable (no
// authentication, rate-limited per client address). It always responds 200: an
// unknown, expired, spent or blocked link is `{"valid": false}` and nothing
// else, so the page can say the link has expired instead of showing a form that
// is going to fail — and so a caller cannot tell those four apart. A usable link
// additionally carries the display name to greet the person with and when it
// stops working. It responds 500 only when the database cannot be read.
func (a *API) handlePasswordResetStatus(w http.ResponseWriter, r *http.Request) {
	status, err := a.passwordReset.Status(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		log.Printf("auth: reading a password-reset link failed: %v", err)
		writeError(w, http.StatusInternalServerError, "could not read the password-reset link")
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// handlePasswordResetConsume sets the new password a person chose behind a reset
// link (no authentication, rate-limited per client address). The link is spent,
// so a second attempt fails, and every session of that account is invalidated.
//
// It responds 204, 400 for a bad body or a password the rules refuse, 404 with
// one unspecific message for a link that is unknown, already used, expired or
// whose account has been blocked, or 500.
func (a *API) handlePasswordResetConsume(w http.ResponseWriter, r *http.Request) {
	var req passwordResetRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	entry := audit.FromRequest(r, "").Entry(audit.ActionUserPasswordResetUse, userTargetType, "", nil)
	_, err := a.passwordReset.Consume(r.Context(), chi.URLParam(r, "token"), req.Password, entry)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, ErrPasswordTooShort):
		writeError(w, http.StatusBadRequest, ErrPasswordTooShort.Error())
	case errors.Is(err, ErrPasswordResetInvalid):
		writeError(w, http.StatusNotFound, ErrPasswordResetInvalid.Error())
	default:
		log.Printf("auth: consuming a password reset failed: %v", err)
		writeError(w, http.StatusInternalServerError, "could not set the new password")
	}
}
