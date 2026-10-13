package projectapi

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/project"
)

// userTargetType is the audit target type of an account, the same one the
// account administration in internal/auth records.
const userTargetType = "users"

// membershipResponse is one project an account belongs to, as the account list
// shows it.
type membershipResponse struct {
	ProjectUID  string       `json:"project_uid"`
	ProjectSlug string       `json:"project_slug"`
	ProjectName string       `json:"project_name"`
	Active      bool         `json:"active"`
	Role        project.Role `json:"role"`
	Approved    bool         `json:"approved"`
}

// userResponse is one account of the instance as the system administrator
// sees it: who it is, its instance role, whether it is disabled, and every
// project it belongs to.
type userResponse struct {
	UID          string               `json:"uid"`
	Username     string               `json:"username"`
	DisplayName  string               `json:"display_name"`
	Email        string               `json:"email"`
	InstanceRole auth.Role            `json:"instance_role"`
	Disabled     bool                 `json:"disabled"`
	Memberships  []membershipResponse `json:"memberships"`
}

// setInstanceRoleRequest is the body of PATCH /users/{uid}.
type setInstanceRoleRequest struct {
	InstanceRole string `json:"instance_role"`
}

// toUserResponse builds the wire shape of u with its memberships. The list is
// never nil, so it encodes as [] for an account with none.
func toUserResponse(u auth.User, memberships []project.MembershipSummary) userResponse {
	out := userResponse{
		UID: u.UID, Username: u.Username, DisplayName: u.DisplayName, Email: u.Email,
		InstanceRole: u.Role, Disabled: u.Disabled,
		Memberships: make([]membershipResponse, 0, len(memberships)),
	}
	for _, ms := range memberships {
		out.Memberships = append(out.Memberships, membershipResponse{
			ProjectUID: ms.ProjectUID, ProjectSlug: ms.Slug, ProjectName: ms.Name, Active: ms.Active,
			Role: ms.Role, Approved: ms.Approved,
		})
	}
	return out
}

// handleListUsers writes every account of the instance, by username, each with
// its instance role and memberships.
func (a *API) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.accounts.ListUsers(r.Context(), auth.UserFilter{})
	if err != nil {
		writeStoreError(w, err, "listing accounts")
		return
	}
	memberships, err := a.projects.AllMemberships(r.Context())
	if err != nil {
		writeStoreError(w, err, "listing memberships")
		return
	}
	out := make([]userResponse, 0, len(users))
	for _, u := range users {
		out = append(out, toUserResponse(u, memberships[u.UID]))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSetInstanceRole promotes an account to a system administrator or
// demotes it to a plain user. Setting the role it already has changes and
// records nothing. It answers 200 with the account, 404 not_found, 409 self
// for a maintainer demoting themselves (somebody else must do it), 409
// last_maintainer, 422 invalid_role and 400 invalid_body.
func (a *API) handleSetInstanceRole(w http.ResponseWriter, r *http.Request) {
	var req setInstanceRoleRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidBody, err.Error())
		return
	}
	role := auth.Role(req.InstanceRole)
	if !role.Valid() {
		writeError(w, http.StatusUnprocessableEntity, codeInvalidRole, `instance_role must be "user" or "maintainer"`)
		return
	}
	uid := chi.URLParam(r, "uid")
	actor, _ := auth.UserFromContext(r.Context())
	if uid == actor.UID && !role.CanMaintain() {
		writeError(w, http.StatusConflict, codeSelf, "a system administrator cannot demote themselves")
		return
	}
	user, err := a.setInstanceRole(r, uid, role)
	if err != nil {
		writeStoreError(w, err, "setting an instance role")
		return
	}
	memberships, err := a.projects.MembershipsOf(r.Context(), user.UID)
	if err != nil {
		writeStoreError(w, err, "listing memberships")
		return
	}
	writeJSON(w, http.StatusOK, toUserResponse(user, memberships))
}

// setInstanceRole reads the account uid and, unless it already has role,
// changes it with an audit entry carrying the new and the previous role.
func (a *API) setInstanceRole(r *http.Request, uid string, role auth.Role) (auth.User, error) {
	current, err := a.accounts.GetUserByUID(r.Context(), uid)
	if err != nil {
		return auth.User{}, fmt.Errorf("reading account %s: %w", uid, err)
	}
	if current.Role == role {
		return current, nil
	}
	entry := auditMeta(r).Entry(audit.ActionUserInstanceRole, userTargetType, uid, map[string]any{
		"instance_role": string(role), "previous": string(current.Role),
	})
	user, err := a.accounts.SetInstanceRoleAudited(r.Context(), uid, role, entry)
	if err != nil {
		return auth.User{}, fmt.Errorf("setting the instance role of %s: %w", uid, err)
	}
	return user, nil
}
