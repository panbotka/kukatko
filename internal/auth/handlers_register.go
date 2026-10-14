package auth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/project"
)

// registerRequest is the JSON body of POST /auth/register. display_name is
// optional and defaults to empty; everything else is required, secret included —
// it is what separates somebody the community told from a stranger who found the
// address. Its field order and types mirror RegisterInput so the handler can
// convert directly — all but Project, a slug the handler resolves into
// RegisterInput.ProjectUID; keep the rest in step.
type registerRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	Secret      string `json:"secret"`
	// UploadLink is the short code of an upload link the person just uploaded
	// through; when set it replaces Secret (see RegisterWithLink).
	UploadLink string `json:"upload_link"`
	// InviteCode is the short code of an invitation link; when set it replaces
	// Secret and the approval (see RegisterWithInvite).
	InviteCode string `json:"invite_code"`
	// Project is the slug of the project whose registration page the form was
	// on: its switch and its secret decide, and the account becomes a member of
	// it. Empty names no project, so the secret path is closed. A link or an
	// invite names its own project, so this is read only on the secret path.
	Project string `json:"project"`
}

// registerResponse is the body of a successful registration. It echoes the
// stored (normalized) values so the client can show what was really saved, and
// says whether the account is waiting — there is no session and no cookie, and
// signing in answers "waiting for approval" until an administrator approves it.
// An account registered through an invitation is approved at once
// (pending_approval false) and may sign in straight away.
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
// registration secret, holds a live upload link or holds a usable invitation.
// It is the one write in this package an anonymous caller may perform, which is
// why it is rate-limited per client address (see registerLimiterFor) and why
// every refusal below says as little as it can.
//
// It responds 201 with the account (pending, or approved through an invite), 400 for a bad body or input the
// account store will not take (over-long username, malformed address, weak
// password), 403 when registration is not open or the secret is wrong (one
// message for both, see writeRegisterError), the upload link standing in for
// it is not live or the invitation is not usable, 409 for a username somebody
// already holds, 429 when the address has spent its budget, or
// 500.
func (a *API) handleRegister(w http.ResponseWriter, r *http.Request) {
	if a.registration == nil {
		writeError(w, http.StatusForbidden, registrationRefused)
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
	in := RegisterInput{
		Username: req.Username, DisplayName: req.DisplayName, Email: req.Email, Password: req.Password,
		Secret: req.Secret, UploadLink: req.UploadLink, InviteCode: req.InviteCode,
	}
	user, err := a.register(r, in, req.Project, entry)
	if err != nil {
		writeRegisterError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, registerResponse{
		Username:        user.Username,
		DisplayName:     user.DisplayName,
		Email:           user.Email,
		PendingApproval: user.ApprovedAt == nil,
	})
}

// register runs the registration path in asks for: through an invitation when
// it names one, through an upload link when it names one — the gate resolves
// the link and the session cookie on r — and through the shared secret of the
// project slug names otherwise. A link or an invite on an instance that wires
// no gate is ErrRegistrationLink or ErrRegistrationInvite, the same answer an
// unknown one gets.
func (a *API) register(r *http.Request, in RegisterInput, slug string, entry audit.Entry) (User, error) {
	if in.InviteCode != "" {
		return a.registerWithInvite(r, in, entry)
	}
	if in.UploadLink == "" {
		projectUID, err := a.registrationProject(r.Context(), slug)
		if err != nil {
			return User{}, err
		}
		in.ProjectUID = projectUID
		return a.registration.Register(r.Context(), in, entry)
	}
	if a.uploadLinks == nil {
		return User{}, ErrRegistrationLink
	}
	grant, err := a.uploadLinks.AdmitRegistration(r.Context(), r, in.UploadLink)
	if err != nil {
		return User{}, fmt.Errorf("auth: admitting the upload link: %w", err)
	}
	return a.registration.RegisterWithLink(r.Context(), in, grant, entry)
}

// registrationProject resolves the slug a secret registration names into the
// project's uid. An empty slug, a slug naming no project and a deactivated
// project are all ErrRegistrationClosed — a door that does not exist is as shut as one whose
// admin closed it, and the anonymous GET /p/{project}/settings/public answers
// the two identically too.
func (a *API) registrationProject(ctx context.Context, slug string) (string, error) {
	if slug == "" || a.projects == nil {
		return "", ErrRegistrationClosed
	}
	p, err := a.projects.GetBySlug(ctx, slug)
	if errors.Is(err, project.ErrNotFound) {
		return "", ErrRegistrationClosed
	}
	if err != nil {
		return "", fmt.Errorf("auth: resolving the registration's project: %w", err)
	}
	if !p.Active {
		return "", ErrRegistrationClosed
	}
	return p.UID, nil
}

// registerWithInvite admits in's invitation through the invite gate and
// registers the account into the invite's project, approved.
func (a *API) registerWithInvite(r *http.Request, in RegisterInput, entry audit.Entry) (User, error) {
	if a.invites == nil {
		return User{}, ErrRegistrationInvite
	}
	grant, err := a.invites.AdmitInvite(r.Context(), in.InviteCode)
	if err != nil {
		return User{}, fmt.Errorf("auth: admitting the invitation: %w", err)
	}
	return a.registration.RegisterWithInvite(r.Context(), in, grant, entry)
}

// writeRegisterError maps a failed registration onto its response. The two
// refusals that belong to registration alone are answered here; the rest are the
// validation and uniqueness errors of any account creation, which
// writeCreateUserError already words (its role and subject branches are
// unreachable from here — a registration names neither).
//
// A wrong secret and a closed door are one answer: the same 403 with the same
// message (registrationRefused). Two messages would tell a stranger which
// slugs name a project with registration open — "wrong secret" is only ever
// said by an open door — and the anonymous settings endpoint takes care not to
// say that (it answers a closed project exactly like a missing one). The
// person typing the word loses nothing they could act on: either way the fix
// is to ask whoever sent them the link.
func writeRegisterError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrRegistrationClosed), errors.Is(err, ErrRegistrationSecret):
		writeError(w, http.StatusForbidden, registrationRefused)
	case errors.Is(err, ErrRegistrationLink):
		writeError(w, http.StatusForbidden, ErrRegistrationLink.Error())
	case errors.Is(err, ErrRegistrationInvite):
		writeError(w, http.StatusForbidden, ErrRegistrationInvite.Error())
	default:
		if !isCreateUserInputError(err) {
			log.Printf("auth: registration failed unexpectedly: %v", err)
		}
		writeCreateUserError(w, err)
	}
}

// registrationRefused is the one message a refused secret registration gets,
// whether the door was closed (or names no project) or the secret was wrong.
const registrationRefused = "auth: registration is closed or the secret is wrong"

// isCreateUserInputError reports whether err is one of the refusals a caller
// caused and can fix, so only the rest — a database or mail failure — is logged
// as a server fault.
func isCreateUserInputError(err error) bool {
	return errors.Is(err, ErrUsernameTaken) ||
		errors.Is(err, ErrUsernameTooLong) ||
		errors.Is(err, ErrUsernameRequired) ||
		errors.Is(err, ErrInvalidEmail) ||
		errors.Is(err, ErrPasswordTooShort) ||
		errors.Is(err, ErrNoteTooLong)
}
