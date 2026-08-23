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
// from r, attributing it to the acting admin. The /admin/users routes are gated
// by RequireAdmin, so a principal is always present in production; if one is
// somehow absent the actor is recorded empty rather than blocking the change.
func adminAuditMeta(r *http.Request) audit.Meta {
	user, _ := UserFromContext(r.Context())
	return audit.FromRequest(r, user.UID)
}

// createUserRequest is the JSON body of POST /admin/users. display_name and note
// are optional and default to empty. Its shape mirrors CreateUserInput so the
// handler can convert directly.
type createUserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Role        Role   `json:"role"`
	Note        string `json:"note"`
	// SubjectUID optionally says, at creation time, which person of the library
	// the account is; omitted or null leaves it unlinked.
	SubjectUID *string `json:"subject_uid"`
}

// updateUserRequest is the JSON body of PATCH /admin/users/{uid}; it replaces the
// user's mutable profile fields. note is a pointer so an omitted key leaves the
// stored note untouched while an explicit "" clears it. Its shape mirrors
// UpdateUserInput so the handler can convert directly.
type updateUserRequest struct {
	DisplayName string  `json:"display_name"`
	Email       string  `json:"email"`
	Role        Role    `json:"role"`
	Disabled    bool    `json:"disabled"`
	Note        *string `json:"note"`
	// SubjectUID is part of the replaced profile, not a partial update like the
	// note: an omitted or null value clears the account's link to a person.
	SubjectUID *string `json:"subject_uid"`
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

// handleCreateUser creates a user (admin only). It responds 201 with the created
// user, 400 for a bad body, weak password, invalid role, missing or malformed
// e-mail address, over-length username or note, 409 for a duplicate username, or
// 500.
func (a *API) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	actor, _ := UserFromContext(r.Context())
	entry := adminAuditMeta(r).Entry(audit.ActionUserCreate, userTargetType, "", nil)
	user, err := a.svc.CreateUserAudited(r.Context(), CreateUserInput(req), actor.Role, entry)
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
		writeError(w, http.StatusBadRequest, "invalid role (want viewer, editor, admin, or maintainer)")
	case errors.Is(err, ErrPasswordTooShort):
		writeError(w, http.StatusBadRequest, ErrPasswordTooShort.Error())
	case errors.Is(err, ErrNoteTooLong):
		writeError(w, http.StatusBadRequest, ErrNoteTooLong.Error())
	case errors.Is(err, ErrUsernameTooLong):
		writeError(w, http.StatusBadRequest, ErrUsernameTooLong.Error())
	case errors.Is(err, ErrInvalidEmail):
		writeError(w, http.StatusBadRequest, ErrInvalidEmail.Error())
	case errors.Is(err, ErrSubjectNotFound):
		writeError(w, http.StatusBadRequest, ErrSubjectNotFound.Error())
	default:
		writeError(w, http.StatusInternalServerError, "could not create user")
	}
}

// handleUpdateUser replaces a user's profile fields (admin only). It responds 200
// with the updated user, 400 for a bad body, invalid role, missing or malformed
// e-mail address, or over-length note,
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
	// The link is recorded as it will stand after the update — the UID, or an
	// explicit null when the administrator has just unlinked the account. Saying
	// who somebody else is (or is no longer) is a change to their account, so it
	// belongs in the trail beside the role and the disabled flag.
	entry := adminAuditMeta(r).Entry(audit.ActionUserUpdate, userTargetType, uid,
		map[string]any{
			"role":        string(req.Role),
			"disabled":    req.Disabled,
			"subject_uid": normalizeSubjectUID(req.SubjectUID),
		})
	user, err := a.svc.UpdateUserAudited(r.Context(), uid, UpdateUserInput(req), actor.Role, entry)
	if err != nil {
		writeUserMutationError(w, err, "could not update user")
		return
	}
	writeJSON(w, http.StatusOK, adminUser(user))
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

// handleApproveUser lets a waiting account in (admin only): it stamps the
// approval time, writes the decision to the audit trail and enqueues the mail
// telling the person they can sign in — all in one transaction.
//
// It responds 200 with the updated account, 403 when the maintainer boundary
// forbids the actor this account (the same rule as every other user-management
// action — approval must not become a way around it), 404 if the account does
// not exist, 409 for a blocked account (unblocking is its own action), or 500.
// Approving an already approved account is 200 with the account unchanged: an
// administrator clicking twice must not see a failure.
func (a *API) handleApproveUser(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	actor, _ := UserFromContext(r.Context())
	entry := adminAuditMeta(r).Entry(audit.ActionUserApprove, userTargetType, uid, nil)
	user, err := a.approval.Approve(r.Context(), uid, actor.Role, entry)
	if err != nil {
		writeApproveUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, adminUser(user))
}

// writeApproveUserError maps a failed approval onto its response. A blocked
// account is a 409 for the same reason ErrLastMaintainer is: the caller is
// allowed to make the change, it is the account's current state that forbids it,
// and re-enabling it first makes the identical request succeed.
func writeApproveUserError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrUserDisabled) {
		writeError(w, http.StatusConflict, ErrUserDisabled.Error())
		return
	}
	writeUserMutationError(w, err, "could not approve user")
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

// writeUserMutationError maps the common user-mutation errors (invalid role,
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
		writeError(w, http.StatusBadRequest, "invalid role (want viewer, editor, admin, or maintainer)")
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
