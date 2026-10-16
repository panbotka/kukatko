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
	"github.com/panbotka/kukatko/internal/notifystanding"
	"github.com/panbotka/kukatko/internal/project"
)

// statusAccepted is the one answer of POST /members, whatever happened.
const statusAccepted = "accepted"

// memberResponse is one member as the members page shows it. It deliberately
// carries no e-mail address: a project admin manages memberships, not the
// accounts behind them. A membership whose identity is withheld never becomes
// one (visibleMember), so there is no field telling one apart either. Nor does
// it carry the account's instance role: a system administrator who is a member
// is listed with their stored project role like anybody else, so a project
// never learns that the role exists.
type memberResponse struct {
	UserUID     string       `json:"user_uid"`
	Username    string       `json:"username"`
	DisplayName string       `json:"display_name"`
	Role        project.Role `json:"role"`
	ApprovedAt  *time.Time   `json:"approved_at"`
	// LastSeenAt is the member's last visit to this project — never the
	// account's last sign-in, which would tell the project's admin when the
	// account was active in other projects.
	LastSeenAt *time.Time `json:"last_seen_at"`
	HasPicture bool       `json:"has_picture"`
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
		ApprovedAt: m.ApprovedAt, LastSeenAt: m.LastSeenAt, HasPicture: m.HasPicture,
	}
}

// handleList writes the project's members, by display name — those the
// project may see: a membership whose identity is withheld is not among them
// (project.Store.Members), so an add that found an account lists exactly what
// an add that found nobody lists.
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

// withdrawRequest is the body of POST /members/withdraw.
type withdrawRequest struct {
	Identifier string `json:"identifier"`
}

// handleWithdraw undoes an exact-match add: it removes the membership of the
// account the body's identifier names while its identity is still withheld
// (project.WithdrawByIdentifier, which audits the real outcome where only the
// system administrator reads it). It answers 202 {"status":"accepted"} whether
// a membership was removed, the account holds none (or one the project already
// sees, which this never touches) or does not exist — the add's own answer, for
// the add's own reason; only a malformed request is refused: 400 invalid_body
// and 422 invalid_identifier (blank).
func (a *API) handleWithdraw(w http.ResponseWriter, r *http.Request) {
	var req withdrawRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidBody, err.Error())
		return
	}
	if _, err := a.members.WithdrawByIdentifier(r.Context(), auditMeta(r),
		auth.ProjectUIDFromContext(r.Context()), req.Identifier); err != nil {
		writeStoreError(w, err, "withdrawing an add")
		return
	}
	writeJSON(w, http.StatusAccepted, acceptedResponse{Status: statusAccepted})
}

// visibleMember checks that userUID holds a membership of the request's project
// the project may see: project.ErrNotMember when there is none — or when its
// identity is withheld.
// Every route addressing a member by uid goes through it first: the project
// cannot see such a membership, so an admin who knows the account's uid from
// elsewhere (another project they share) must not learn from a 200 or a 204
// that the address they typed into the blind add was that account's. Should
// the account enter between this read and the change, it is visible by then.
func (a *API) visibleMember(r *http.Request, userUID string) error {
	member, err := a.members.Membership(r.Context(), auth.ProjectUIDFromContext(r.Context()), userUID)
	if err != nil {
		return fmt.Errorf("reading member %s: %w", userUID, err)
	}
	if member.IdentityWithheld {
		return project.ErrNotMember
	}
	return nil
}

// handleSetRole changes a member's role. It answers 200 with the member, 404
// not_member for a membership whose identity is withheld (visibleMember), 409
// self for the caller's own membership (another admin must do it), 409
// last_admin for demoting the project's only admin, 404 not_member, 422
// invalid_role and 400 invalid_body. A system administrator's membership is
// changed like any other: their access to the instance never depended on it.
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
	if err := a.visibleMember(r, userUID); err != nil {
		writeStoreError(w, err, "reading a member")
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
// not_member for an account that is not a member, or whose identity is
// withheld (visibleMember).
func (a *API) handleApprove(w http.ResponseWriter, r *http.Request) {
	userUID := chi.URLParam(r, "user_uid")
	if err := a.visibleMember(r, userUID); err != nil {
		writeStoreError(w, err, "reading a member")
		return
	}
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
// and 404 not_member, for a membership whose identity is withheld too
// (visibleMember; POST /members/withdraw is its way out).
func (a *API) handleRemove(w http.ResponseWriter, r *http.Request) {
	userUID := chi.URLParam(r, "user_uid")
	if err := a.visibleMember(r, userUID); err != nil {
		writeStoreError(w, err, "reading a member")
		return
	}
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
		// Addressed to the membership, so the mail is checked again when it is
		// sent: a member removed (or a project deactivated, an account disabled)
		// before then is not told about a project it can no longer open.
		approved := mailjob.AccountApproved(user.Email, data).Addressed(notifystanding.Addressee{
			UserUID: member.UserUID, ProjectUID: member.ProjectUID,
		})
		if err := a.mail.Enqueue(ctx, tx, approved); err != nil {
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
