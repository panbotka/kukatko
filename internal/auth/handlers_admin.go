package auth

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// userTargetType names the audited entity kind for user accounts.
const userTargetType = "users"

// adminAuditMeta builds the audit envelope for an admin user-management mutation
// from r, attributing it to the acting system administrator. The /admin/users
// routes are gated by RequireMaintainer, so a principal is always present in
// production; if one is
// somehow absent the actor is recorded empty rather than blocking the change.
func adminAuditMeta(r *http.Request) audit.Meta {
	user, _ := UserFromContext(r.Context())
	return audit.FromRequest(r, user.UID)
}

// createUserRequest is the JSON body of POST /admin/users: the account record,
// nothing project-shaped. display_name and note are optional and default to
// empty, instance_role to "user". Its shape mirrors NewAccountInput so the
// handler can convert directly.
type createUserRequest struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	DisplayName  string `json:"display_name"`
	Email        string `json:"email"`
	InstanceRole Role   `json:"instance_role"`
	Note         string `json:"note"`
}

// updateUserRequest is the JSON body of PATCH /admin/users/{uid}; it replaces the
// user's mutable profile fields. note is a pointer so an omitted key leaves the
// stored note untouched while an explicit "" clears it. Its shape mirrors
// UpdateUserInput so the handler can convert directly.
type updateUserRequest struct {
	DisplayName  string  `json:"display_name"`
	Email        string  `json:"email"`
	InstanceRole Role    `json:"instance_role"`
	Disabled     bool    `json:"disabled"`
	Note         *string `json:"note"`
}

// adminUserResponse is the admin-only JSON view of a user: every field User
// exposes, plus the administrative note that User itself withholds (json:"-") so
// it cannot leak through the login and /auth/me payloads. The embedded User's
// fields are promoted, so the wire shape is the old one plus "note".
type adminUserResponse struct {
	User
	Note string `json:"note"`
}

// adminUser builds the admin-only view of u.
func adminUser(u User) adminUserResponse {
	return adminUserResponse{User: u, Note: u.Note}
}

// adminUserList builds the admin-only view of every user in users, preserving
// order. The result is empty (not nil) so it encodes as [] rather than null.
func adminUserList(users []User) []adminUserResponse {
	out := make([]adminUserResponse, 0, len(users))
	for _, u := range users {
		out = append(out, adminUser(u))
	}
	return out
}

// renameUserRequest is the JSON body of PUT /admin/users/{uid}/username.
type renameUserRequest struct {
	Username string `json:"username"`
}

// resetPasswordRequest is the JSON body of POST /admin/users/{uid}/password.
type resetPasswordRequest struct {
	NewPassword string `json:"new_password"`
}

// handleListUsers returns the users (admin only), optionally narrowed by
// approval state. `?pending=true` lists only the accounts waiting for an
// administrator, so a self-service registration can be found without reading
// every account; `?pending=false` lists only the ones already let in, and an
// absent parameter everybody. It responds 400 for a `pending` that is not a
// boolean, rather than silently listing something else than what was asked for.
func (a *API) handleListUsers(w http.ResponseWriter, r *http.Request) {
	pending, err := pendingParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	users, err := a.svc.ListUsers(r.Context(), UserFilter{Pending: pending})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list users")
		return
	}
	writeJSON(w, http.StatusOK, adminUserList(users))
}

// pendingParam parses the `pending` query parameter of the user listing: nil
// when it is absent (no filter) and an error when it is present but not a
// boolean.
func pendingParam(r *http.Request) (*bool, error) {
	raw := r.URL.Query().Get("pending")
	if raw == "" {
		// An absent optional filter legitimately yields no value and no error.
		return nil, nil //nolint:nilnil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, errors.New("pending must be true or false")
	}
	return &value, nil
}

// handleCreateUser creates an account with no membership (system administrator
// only): it joins a project when that project's admin adds it, or by an
// invitation. It responds 201 with the created account, 400 for a bad body,
// weak password, invalid instance role, missing or malformed
// e-mail address, over-length username or note, 409 for a duplicate username,
// or 500.
func (a *API) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	actor, _ := UserFromContext(r.Context())
	entry := adminAuditMeta(r).Entry(audit.ActionUserCreate, userTargetType, "", nil)
	user, err := a.svc.CreateUserAudited(r.Context(), NewAccountInput(req), actor.Role, entry)
	if err != nil {
		writeCreateUserError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, adminUser(user))
}

// writeCreateUserError maps a CreateUser error onto the appropriate HTTP status.
func writeCreateUserError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUsernameTaken):
		writeError(w, http.StatusConflict, "username already taken")
	case errors.Is(err, ErrMaintainerRequired):
		writeError(w, http.StatusForbidden, ErrMaintainerRequired.Error())
	case errors.Is(err, ErrInvalidRole):
		writeError(w, http.StatusBadRequest, invalidInstanceRoleMessage)
	default:
		if verbatim, ok := verbatimInputError(err); ok {
			writeError(w, http.StatusBadRequest, verbatim.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "could not create user")
	}
}

// invalidInstanceRoleMessage is the 400 answer to an instance role outside
// user/maintainer.
const invalidInstanceRoleMessage = "invalid instance role (want user or maintainer)"

// verbatimInputErrors are the refusals of account input whose message already
// names the offending field, so a handler answers them with a 400 carrying that
// message unchanged and the client can put it under the right field.
var verbatimInputErrors = []error{
	ErrPasswordTooShort, ErrNoteTooLong, ErrUsernameTooLong, ErrUsernameRequired,
	ErrInvalidEmail, ErrSubjectNotFound,
}

// verbatimInputError returns the member of verbatimInputErrors that err is (or
// wraps), and false when it is none of them.
func verbatimInputError(err error) (error, bool) {
	for _, known := range verbatimInputErrors {
		if errors.Is(err, known) {
			return known, true
		}
	}
	return nil, false
}

// handleUpdateUser replaces an account's profile fields — the account record,
// never a membership — (system administrator only). It responds 200 with the
// updated account, 400 for a bad body, invalid instance role, missing or
// malformed e-mail address, or over-length note,
// 404 if the user does not exist, 409 when the change would demote or disable the
// last enabled maintainer, or 500.
func (a *API) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	var req updateUserRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	actor, _ := UserFromContext(r.Context())
	entry := adminAuditMeta(r).Entry(audit.ActionUserUpdate, userTargetType, uid,
		map[string]any{
			"instance_role": string(req.InstanceRole),
			"disabled":      req.Disabled,
		})
	user, err := a.svc.UpdateUserAudited(r.Context(), uid, UpdateUserInput(req), actor.Role, entry)
	if err != nil {
		writeUserMutationError(w, err, "could not update user")
		return
	}
	writeJSON(w, http.StatusOK, adminUser(user))
}

// handleRenameUser gives an account a new username (admin only), normalized and
// validated as on creation, and mails the person the new name when mail is on
// and their address is real.
//
// It is its own route rather than a field of PATCH /admin/users/{uid}: that
// endpoint replaces the profile and is resent whole by every save, while a
// rename has its own audit action, its own mail and its own conflict, and a
// client should have to mean it.
//
// It responds 200 with the account (unchanged when the name is the one it
// already has), 400 for a bad body or an empty or over-long username, 403 when
// the maintainer boundary forbids the actor this account, 404 if the account
// does not exist, 409 when another account holds the name, or 500.
func (a *API) handleRenameUser(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	var req renameUserRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	actor, _ := UserFromContext(r.Context())
	entry := adminAuditMeta(r).Entry(audit.ActionUserRename, userTargetType, uid, nil)
	user, err := a.rename.Rename(r.Context(), uid, req.Username, actor.Role, entry)
	if err != nil {
		writeRenameUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, adminUser(user))
}

// writeRenameUserError maps a failed rename onto its response: the name's own
// refusals here, the boundary and the missing account through
// writeUserMutationError.
func writeRenameUserError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUsernameTaken):
		writeError(w, http.StatusConflict, "username already taken")
	case errors.Is(err, ErrUsernameRequired):
		writeError(w, http.StatusBadRequest, ErrUsernameRequired.Error())
	case errors.Is(err, ErrUsernameTooLong):
		writeError(w, http.StatusBadRequest, ErrUsernameTooLong.Error())
	default:
		writeUserMutationError(w, err, "could not rename user")
	}
}

// handleDisableUser disables a user and invalidates their sessions (admin only).
// It responds 409 when the target is the instance's last enabled maintainer,
// whose loss would make every operations surface permanently unreachable.
func (a *API) handleDisableUser(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	actor, _ := UserFromContext(r.Context())
	entry := adminAuditMeta(r).Entry(audit.ActionUserDisable, userTargetType, uid,
		map[string]any{"disabled": true})
	user, err := a.svc.SetUserDisabledAudited(r.Context(), uid, true, actor.Role, entry)
	if err != nil {
		writeUserMutationError(w, err, "could not disable user")
		return
	}
	writeJSON(w, http.StatusOK, adminUser(user))
}

// handleResetPassword sets a new password for a user and invalidates all their
// sessions (admin only). It responds 204, 400 for a bad body or weak password,
// 404 if the user does not exist, or 500.
func (a *API) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	var req resetPasswordRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	actor, _ := UserFromContext(r.Context())
	entry := adminAuditMeta(r).Entry(audit.ActionUserPassword, userTargetType, uid, nil)
	err := a.svc.ResetPasswordAudited(r.Context(), uid, req.NewPassword, actor.Role, entry)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, ErrMaintainerRequired):
		writeError(w, http.StatusForbidden, ErrMaintainerRequired.Error())
	case errors.Is(err, ErrPasswordTooShort):
		writeError(w, http.StatusBadRequest, ErrPasswordTooShort.Error())
	case errors.Is(err, ErrUserNotFound):
		writeError(w, http.StatusNotFound, "user not found")
	default:
		writeError(w, http.StatusInternalServerError, "could not reset password")
	}
}

// writeUserMutationError maps the common user-mutation errors (invalid instance
// role,
// invalid e-mail address, over-length note, not found, last maintainer) onto HTTP
// statuses, falling back to fallback as a 500 message.
//
// ErrLastMaintainer is a 409, not a 403: the caller is allowed to make the
// change, it is the instance's current state that forbids it, and promoting
// someone else to maintainer first makes the very same request succeed.
func writeUserMutationError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, ErrMaintainerRequired):
		writeError(w, http.StatusForbidden, ErrMaintainerRequired.Error())
	case errors.Is(err, ErrLastMaintainer):
		writeError(w, http.StatusConflict, ErrLastMaintainer.Error())
	case errors.Is(err, ErrInvalidRole):
		writeError(w, http.StatusBadRequest, invalidInstanceRoleMessage)
	case errors.Is(err, ErrNoteTooLong):
		writeError(w, http.StatusBadRequest, ErrNoteTooLong.Error())
	case errors.Is(err, ErrInvalidEmail):
		writeError(w, http.StatusBadRequest, ErrInvalidEmail.Error())
	case errors.Is(err, ErrSubjectNotFound):
		writeError(w, http.StatusBadRequest, ErrSubjectNotFound.Error())
	case errors.Is(err, ErrUserNotFound):
		writeError(w, http.StatusNotFound, "user not found")
	default:
		writeError(w, http.StatusInternalServerError, fallback)
	}
}
