package auth

import (
	"errors"
	"log"
	"net/http"

	"github.com/panbotka/kukatko/internal/audit"
)

// registerRequest is the JSON body of POST /auth/register. display_name is
// optional and defaults to empty; everything else is required, secret included —
// it is what separates somebody the community told from a stranger who found the
// address. Its field order and types mirror RegisterInput so the handler can
// convert directly; keep the two in step.
type registerRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	Secret      string `json:"secret"`
}

// registerResponse is the body of a successful registration. It echoes the
// stored (normalized) values so the client can show what was really saved, and
// says out loud that the account is waiting — there is no session and no cookie,
// and signing in now answers "waiting for approval" rather than success.
//
// It is deliberately not the User payload: an anonymous caller has no business
// learning the account's UID or role, and the three fields it does get are the
// three it just sent.
type registerResponse struct {
	Username        string `json:"username"`
	DisplayName     string `json:"display_name"`
	Email           string `json:"email"`
	PendingApproval bool   `json:"pending_approval"`
}

// handleRegister creates an account for somebody who knows the instance's shared
// registration secret. It is the one write in this package an anonymous caller
// may perform, which is why it is rate-limited per client address (see
// registerLimiterFor) and why every refusal below says as little as it can.
//
// It responds 201 with the pending account, 400 for a bad body or input the
// account store will not take (over-long username, malformed address, weak
// password), 403 when registration is not open or the secret is wrong, 409 for a
// username somebody already holds, 429 when the address has spent its budget, or
// 500.
func (a *API) handleRegister(w http.ResponseWriter, r *http.Request) {
	if a.registration == nil {
		writeError(w, http.StatusForbidden, ErrRegistrationClosed.Error())
		return
	}
	var req registerRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// The actor and the target are stamped by Register once the account has a
	// UID: the account registers itself, so there is nobody else to attribute it
	// to. The address and User-Agent come from here, where the request is.
	entry := audit.FromRequest(r, "").Entry(audit.ActionUserRegister, userTargetType, "", nil)
	user, err := a.registration.Register(r.Context(), RegisterInput(req), entry)
	if err != nil {
		writeRegisterError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, registerResponse{
		Username:        user.Username,
		DisplayName:     user.DisplayName,
		Email:           user.Email,
		PendingApproval: true,
	})
}

// writeRegisterError maps a failed registration onto its response. The two
// refusals that belong to registration alone are answered here; the rest are the
// validation and uniqueness errors of any account creation, which
// writeCreateUserError already words (its role and subject branches are
// unreachable from here — a registration names neither).
//
// A wrong secret and a closed instance are both 403 and both say only what the
// caller needs: whether registration is open is public knowledge (the anonymous
// settings endpoint publishes it), while the secret's contents never leak from
// either message.
func writeRegisterError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrRegistrationClosed):
		writeError(w, http.StatusForbidden, ErrRegistrationClosed.Error())
	case errors.Is(err, ErrRegistrationSecret):
		writeError(w, http.StatusForbidden, ErrRegistrationSecret.Error())
	default:
		if !isCreateUserInputError(err) {
			log.Printf("auth: registration failed unexpectedly: %v", err)
		}
		writeCreateUserError(w, err)
	}
}

// isCreateUserInputError reports whether err is one of the refusals a caller
// caused and can fix, so only the rest — a database or mail failure — is logged
// as a server fault.
func isCreateUserInputError(err error) bool {
	return errors.Is(err, ErrUsernameTaken) ||
		errors.Is(err, ErrUsernameTooLong) ||
		errors.Is(err, ErrInvalidEmail) ||
		errors.Is(err, ErrPasswordTooShort) ||
		errors.Is(err, ErrNoteTooLong)
}
