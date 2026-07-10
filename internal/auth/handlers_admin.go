package auth

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
)

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

// handleListUsers returns all users (admin only).
func (a *API) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.svc.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list users")
		return
	}
	writeJSON(w, http.StatusOK, adminUserList(users))
}

// handleCreateUser creates a user (admin only). It responds 201 with the created
// user, 400 for a bad body, weak password, invalid role, or over-length note,
// 409 for a duplicate username, or 500.
func (a *API) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user, err := a.svc.CreateUser(r.Context(), CreateUserInput(req))
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
	case errors.Is(err, ErrInvalidRole):
		writeError(w, http.StatusBadRequest, "invalid role (want admin, editor, or viewer)")
	case errors.Is(err, ErrPasswordTooShort):
		writeError(w, http.StatusBadRequest, ErrPasswordTooShort.Error())
	case errors.Is(err, ErrNoteTooLong):
		writeError(w, http.StatusBadRequest, ErrNoteTooLong.Error())
	default:
		writeError(w, http.StatusInternalServerError, "could not create user")
	}
}

// handleUpdateUser replaces a user's profile fields (admin only). It responds 200
// with the updated user, 400 for a bad body, invalid role, or over-length note,
// 404 if the user does not exist, or 500.
func (a *API) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	var req updateUserRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user, err := a.svc.UpdateUser(r.Context(), uid, UpdateUserInput(req))
	if err != nil {
		writeUserMutationError(w, err, "could not update user")
		return
	}
	writeJSON(w, http.StatusOK, adminUser(user))
}

// handleDisableUser disables a user and invalidates their sessions (admin only).
func (a *API) handleDisableUser(w http.ResponseWriter, r *http.Request) {
	user, err := a.svc.SetUserDisabled(r.Context(), chi.URLParam(r, "uid"), true)
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
	err := a.svc.ResetPassword(r.Context(), uid, req.NewPassword)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, ErrPasswordTooShort):
		writeError(w, http.StatusBadRequest, ErrPasswordTooShort.Error())
	case errors.Is(err, ErrUserNotFound):
		writeError(w, http.StatusNotFound, "user not found")
	default:
		writeError(w, http.StatusInternalServerError, "could not reset password")
	}
}

// writeUserMutationError maps the common user-mutation errors (invalid role,
// over-length note, not found) onto HTTP statuses, falling back to fallback as a
// 500 message.
func writeUserMutationError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, ErrInvalidRole):
		writeError(w, http.StatusBadRequest, "invalid role (want admin, editor, or viewer)")
	case errors.Is(err, ErrNoteTooLong):
		writeError(w, http.StatusBadRequest, ErrNoteTooLong.Error())
	case errors.Is(err, ErrUserNotFound):
		writeError(w, http.StatusNotFound, "user not found")
	default:
		writeError(w, http.StatusInternalServerError, fallback)
	}
}
