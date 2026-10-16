package projectapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/project"
)

// createRequest is the body of POST /projects. A limit left out (or null) is
// no limit. Admin, when present, sets the project up for somebody else (see
// auth.ProjectSetup); without it the caller becomes the admin.
type createRequest struct {
	Slug          string        `json:"slug"`
	Name          string        `json:"name"`
	Description   string        `json:"description"`
	MaxFileBytes  *int64        `json:"max_file_bytes"`
	MaxTotalBytes *int64        `json:"max_total_bytes"`
	Admin         *adminRequest `json:"admin"`
}

// adminRequest is the first admin of a project set up for somebody: the e-mail
// address (required), and for a new account its display name and username
// (derived from the address when empty).
type adminRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Username    string `json:"username"`
}

// createResponse is the answer to POST /projects: the project, plus — for a
// project set up for somebody — what became of its first admin.
type createResponse struct {
	project.Overview
	FirstAdmin *auth.FirstAdmin `json:"first_admin,omitempty"`
}

// optionalLimit is a size limit in a PATCH body, where three things must be
// told apart: the key left out (keep the limit), null (clear it) and a number
// (set it).
type optionalLimit struct {
	// set is true when the key was present at all.
	set bool
	// value is the new limit, or nil for an explicit null.
	value *int64
}

// UnmarshalJSON records that the key was present and its value; encoding/json
// calls it for an explicit null too, which is what tells null from absent.
func (o *optionalLimit) UnmarshalJSON(data []byte) error {
	o.set = true
	if strings.TrimSpace(string(data)) == "null" {
		o.value = nil
		return nil
	}
	var value int64
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("a size limit must be a whole number of bytes or null: %w", err)
	}
	o.value = &value
	return nil
}

// applyLimit writes o into the patch's set and clear fields of one limit.
func applyLimit(o optionalLimit, set **int64, unset *bool) {
	if !o.set {
		return
	}
	if o.value == nil {
		*unset = true
		return
	}
	*set = o.value
}

// updateRequest is the body of PATCH /projects/{uid}: every field optional.
// Slug is accepted by the decoder only to be refused with slug_immutable — the
// slug is the project's URL and never changes.
type updateRequest struct {
	Slug          json.RawMessage `json:"slug"`
	Name          *string         `json:"name"`
	Description   *string         `json:"description"`
	Active        *bool           `json:"active"`
	MaxFileBytes  optionalLimit   `json:"max_file_bytes"`
	MaxTotalBytes optionalLimit   `json:"max_total_bytes"`
}

// patch converts the request into the store's patch.
func (req updateRequest) patch() project.UpdatePatch {
	patch := project.UpdatePatch{Name: req.Name, Description: req.Description, Active: req.Active}
	applyLimit(req.MaxFileBytes, &patch.MaxFileBytes, &patch.ClearMaxFileBytes)
	applyLimit(req.MaxTotalBytes, &patch.MaxTotalBytes, &patch.ClearMaxTotalBytes)
	return patch
}

// addMemberRequest is the body of POST /projects/{uid}/members.
type addMemberRequest struct {
	UserUID string `json:"user_uid"`
	Role    string `json:"role"`
}

// handleList writes every project with its member count and usage.
func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	list, err := a.projects.Overviews(r.Context())
	if err != nil {
		writeStoreError(w, err, "listing projects")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleGet writes one project, 404 not_found for an unknown uid.
func (a *API) handleGet(w http.ResponseWriter, r *http.Request) {
	overview, err := a.projects.OverviewOf(r.Context(), chi.URLParam(r, "uid"))
	if err != nil {
		writeStoreError(w, err, "reading a project")
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

// handleCreate creates a project. Without "admin" the caller becomes its first
// admin, so somebody can enter it; with it the project is set up for that
// person instead (handleCreateFor). It answers 201 with the project, 409
// slug_taken, 422 invalid_slug (with the rule as the message), invalid_name or
// invalid_limit, and 400 invalid_body.
func (a *API) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidBody, err.Error())
		return
	}
	params := project.CreateParams{
		Slug: req.Slug, Name: req.Name, Description: req.Description,
		MaxFileBytes: req.MaxFileBytes, MaxTotalBytes: req.MaxTotalBytes,
	}
	if req.Admin != nil {
		a.handleCreateFor(w, r, params, *req.Admin)
		return
	}
	actor, _ := auth.UserFromContext(r.Context())
	params.CreatorUID = actor.UID
	created, err := a.projects.Create(r.Context(), auditMeta(r), params)
	if err != nil {
		writeStoreError(w, err, "creating a project")
		return
	}
	// A new project holds nothing yet; its only member is the creator.
	writeJSON(w, http.StatusCreated, createResponse{Overview: project.Overview{Project: *created, MembersCount: 1}})
}

// handleCreateFor sets the project params up for the person admin: the project,
// their account (or the one the address already has) as its only admin, the
// set-password link and its mail, in one transaction — the caller does not
// become a member. Besides handleCreate's refusals it answers 422
// invalid_email or invalid_username, 409 username_taken, email_ambiguous
// (several accounts share the address) or user_disabled.
func (a *API) handleCreateFor(w http.ResponseWriter, r *http.Request, params project.CreateParams, admin adminRequest) {
	created, first, err := a.setup.Create(r.Context(), auditMeta(r), params, auth.FirstAdminInput{
		Email: admin.Email, DisplayName: admin.DisplayName, Username: admin.Username,
	})
	if err != nil {
		writeStoreError(w, err, "setting a project up")
		return
	}
	// Its only member is the first admin.
	writeJSON(w, http.StatusCreated, createResponse{
		Overview: project.Overview{Project: *created, MembersCount: 1}, FirstAdmin: &first,
	})
}

// handleAwaitingPassword lists the members of the project whose accounts have
// no password yet, each with its link's expiry. It answers 200 (an empty list
// for a project without such members) or 404 not_found for an unknown project.
func (a *API) handleAwaitingPassword(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if _, err := a.projects.OverviewOf(r.Context(), uid); err != nil {
		writeStoreError(w, err, "reading a project")
		return
	}
	list, err := a.setup.AwaitingPassword(r.Context(), uid)
	if err != nil {
		writeStoreError(w, err, "listing members awaiting a password")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleResendLink issues and mails a fresh set-password link to a member
// whose account has no password yet; the previous link stops working. It
// answers 200 with the first-admin result (the link included), 404 not_found
// or not_member, and 409 password_set or user_disabled.
func (a *API) handleResendLink(w http.ResponseWriter, r *http.Request) {
	first, err := a.setup.ResendLink(r.Context(), auditMeta(r), chi.URLParam(r, "uid"), chi.URLParam(r, "user_uid"))
	if err != nil {
		writeStoreError(w, err, "re-sending a set-password link")
		return
	}
	writeJSON(w, http.StatusOK, first)
}

// handleUpdate edits a project and writes it as it now stands. A slug in the
// body is 422 slug_immutable; otherwise the refusals of handleCreate, plus 404
// not_found.
func (a *API) handleUpdate(w http.ResponseWriter, r *http.Request) {
	var req updateRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidBody, err.Error())
		return
	}
	if req.Slug != nil {
		writeError(w, http.StatusUnprocessableEntity, codeSlugImmutable, "the slug of a project cannot be changed")
		return
	}
	uid := chi.URLParam(r, "uid")
	if _, err := a.projects.Update(r.Context(), auditMeta(r), uid, req.patch()); err != nil {
		writeStoreError(w, err, "updating a project")
		return
	}
	a.handleGet(w, r)
}

// handleAddMember adds the account user_uid to the project with role, approved
// at once — the system administrator vouches for it. It answers 201 with the
// membership, 404 not_found for an unknown project, 409 already_member, 422
// invalid_role or user_not_found, and 400 invalid_body.
func (a *API) handleAddMember(w http.ResponseWriter, r *http.Request) {
	var req addMemberRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidBody, err.Error())
		return
	}
	if strings.TrimSpace(req.UserUID) == "" {
		writeError(w, http.StatusBadRequest, codeInvalidBody, "user_uid is required")
		return
	}
	role, err := project.ParseRole(req.Role)
	if err != nil {
		writeStoreError(w, err, "parsing a member role")
		return
	}
	member, err := a.projects.AddMember(r.Context(), auditMeta(r), chi.URLParam(r, "uid"), req.UserUID, role, true)
	if err != nil {
		writeStoreError(w, err, "adding a member")
		return
	}
	writeJSON(w, http.StatusCreated, member)
}

// handleRemoveMember removes a membership. It answers 204, 404 not_found or
// not_member, and 409 last_admin when it is the project's only admin.
func (a *API) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	err := a.projects.RemoveMember(r.Context(), auditMeta(r), chi.URLParam(r, "uid"), chi.URLParam(r, "user_uid"))
	if err != nil {
		writeStoreError(w, err, "removing a member")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
