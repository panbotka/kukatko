package memberapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/project"
)

// statusAccepted is the one answer of POST /members, whatever happened.
const statusAccepted = "accepted"

// memberResponse is one member as the members page shows it. It deliberately
// carries no e-mail address: a project admin manages memberships, not the
// accounts behind them.
type memberResponse struct {
	UserUID     string       `json:"user_uid"`
	Username    string       `json:"username"`
	DisplayName string       `json:"display_name"`
	Role        project.Role `json:"role"`
	ApprovedAt  *time.Time   `json:"approved_at"`
	LastLoginAt *time.Time   `json:"last_login_at"`
	HasPicture  bool         `json:"has_picture"`
	// IdentityWithheld marks an exact-match add whose account has not entered
	// the project yet: its username and display name are empty, last_login_at
	// is null and has_picture false until it does (project.Member).
	IdentityWithheld bool `json:"identity_withheld"`
}

// addRequest is the body of POST /members.
type addRequest struct {
	Identifier string `json:"identifier"`
	Role       string `json:"role"`
}

// roleRequest is the body of PATCH /members/{user_uid}.
type roleRequest struct {
	Role string `json:"role"`
}

// acceptedResponse is the body of POST /members.
type acceptedResponse struct {
	Status string `json:"status"`
}

// toMemberResponse builds the wire shape of m.
func toMemberResponse(m project.Member) memberResponse {
	return memberResponse{
		UserUID: m.UserUID, Username: m.Username, DisplayName: m.DisplayName, Role: m.Role,
		ApprovedAt: m.ApprovedAt, LastLoginAt: m.LastLoginAt, HasPicture: m.HasPicture,
		IdentityWithheld: m.IdentityWithheld,
	}
}

// handleList writes the project's members, by display name.
func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	members, err := a.members.Members(r.Context(), auth.ProjectUIDFromContext(r.Context()))
	if err != nil {
		writeStoreError(w, err, "listing members")
		return
	}
	out := make([]memberResponse, 0, len(members))
	for _, m := range members {
		out = append(out, toMemberResponse(m))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAdd adds the account the body's identifier names — its exact username
// or e-mail address — as an approved member with the body's role, its identity
// withheld from the project until it enters it (project.AddByIdentifier, which
// also keeps the outcome out of the project's audit trail). It answers
// 202 {"status":"accepted"} whether the account was added, already was a
// member or does not exist, so the answer reveals nothing about which accounts
// exist; only a malformed request is refused: 400 invalid_body, 422
// invalid_role (anything but viewer/curator/editor/admin) and 422
// invalid_identifier (blank).
func (a *API) handleAdd(w http.ResponseWriter, r *http.Request) {
	var req addRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidBody, err.Error())
		return
	}
	role, err := project.ParseRole(req.Role)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, codeInvalidRole,
			`role must be "viewer", "curator", "editor" or "admin"`)
		return
	}
	if _, err := a.members.AddByIdentifier(r.Context(), auditMeta(r),
		auth.ProjectUIDFromContext(r.Context()), req.Identifier, role); err != nil {
		writeStoreError(w, err, "adding a member")
		return
	}
	writeJSON(w, http.StatusAccepted, acceptedResponse{Status: statusAccepted})
}

// handleSetRole changes a member's role. It answers 200 with the member, 409
// self for the caller's own membership (another admin must do it), 409
// last_admin for demoting the project's only admin, 404 not_member, 422
// invalid_role and 400 invalid_body.
func (a *API) handleSetRole(w http.ResponseWriter, r *http.Request) {
	var req roleRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidBody, err.Error())
		return
	}
	role, err := project.ParseRole(req.Role)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, codeInvalidRole,
			`role must be "viewer", "curator", "editor" or "admin"`)
		return
	}
	userUID := chi.URLParam(r, "user_uid")
	if userUID == actorUID(r) {
		writeError(w, http.StatusConflict, codeSelf, "an admin cannot change their own role")
		return
	}
	member, err := a.members.SetRole(r.Context(), auditMeta(r), auth.ProjectUIDFromContext(r.Context()), userUID, role)
	if err != nil {
		writeStoreError(w, err, "changing a member's role")
		return
	}
	writeJSON(w, http.StatusOK, toMemberResponse(*member))
}

// handleApprove lets a waiting member in and, in the same transaction, schedules
// the mail telling them which project they may now open. Approving an approved
// member is 200 with the member unchanged and no mail. It answers 404
// not_member for an account that is not a member.
func (a *API) handleApprove(w http.ResponseWriter, r *http.Request) {
	userUID := chi.URLParam(r, "user_uid")
	member, err := a.members.ApproveWith(r.Context(), auditMeta(r), auth.ProjectUIDFromContext(r.Context()), userUID,
		a.approvalMail(r))
	if err != nil {
		writeStoreError(w, err, "approving a member")
		return
	}
	writeJSON(w, http.StatusOK, toMemberResponse(*member))
}

// handleRemove removes a member from the project; the account itself stays.
// It answers 204, 409 last_admin for the project's only admin — 409 self when
// that admin is the caller, who may leave only while another admin remains —
// and 404 not_member.
func (a *API) handleRemove(w http.ResponseWriter, r *http.Request) {
	userUID := chi.URLParam(r, "user_uid")
	err := a.members.RemoveMember(r.Context(), auditMeta(r), auth.ProjectUIDFromContext(r.Context()), userUID)
	if err == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if userUID == actorUID(r) && errors.Is(err, project.ErrLastAdmin) {
		writeError(w, http.StatusConflict, codeSelf, "you are the project's only admin; make somebody else one first")
		return
	}
	writeStoreError(w, err, "removing a member")
}

// approvalMail returns the hook that schedules the approval mail on the
// approval's transaction, or nil when the API has no way to send one. The mail
// names the request's project and links to its page.
func (a *API) approvalMail(r *http.Request) project.ApproveHook {
	if a.mail == nil || a.accounts == nil {
		return nil
	}
	p, _ := auth.ProjectFromContext(r.Context())
	return func(ctx context.Context, tx pgx.Tx, member *project.Member) error {
		user, err := a.accounts.GetUserByUID(ctx, member.UserUID)
		if err != nil {
			return fmt.Errorf("reading the approved account: %w", err)
		}
		data := mailer.AccountApprovedData{DisplayName: user.DisplayName}
		if p != nil {
			data.ProjectName = p.Name
			data.ProjectURL = a.urlOf(p.Slug)
		}
		if err := a.mail.Enqueue(ctx, tx, mailjob.AccountApproved(user.Email, data)); err != nil {
			return fmt.Errorf("scheduling the approval mail: %w", err)
		}
		return nil
	}
}

// urlOf returns the public address of the project slug, or "" when the API
// knows no public address.
func (a *API) urlOf(slug string) string {
	if a.projectURL == nil {
		return ""
	}
	return a.projectURL(slug)
}
