package inviteapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/invite"
	"github.com/panbotka/kukatko/internal/project"
)

// inviteView is one invite as the admin's list shows it: the stored fields plus
// its readable code, the URL to send and its state now.
type inviteView struct {
	invite.Invite
	Code  string       `json:"code"`
	URL   string       `json:"url"`
	State invite.State `json:"state"`
}

// listResponse is the body of GET /invites: the invites and the validity
// bounds the create form offers.
type listResponse struct {
	Invites     []inviteView `json:"invites"`
	DefaultDays int          `json:"default_days"`
	MaxDays     int          `json:"max_days"`
}

// createRequest is the body of POST /invites. days and max_uses are optional:
// no days is invite.DefaultDays, no max_uses is no cap.
type createRequest struct {
	Role    string `json:"role"`
	Days    *int   `json:"days"`
	MaxUses *int   `json:"max_uses"`
}

// publicResponse is the body of GET /invite/{code}: what the invite grants and
// whether the caller is signed in (accept) or not (register). registration_role
// is the role registering through it grants, capped at editor.
type publicResponse struct {
	ProjectName      string       `json:"project_name"`
	Role             project.Role `json:"role"`
	RegistrationRole project.Role `json:"registration_role"`
	SignedIn         bool         `json:"signed_in"`
}

// acceptResponse is the body of POST /invite/{code}/accept: where to go next.
type acceptResponse struct {
	Slug    string       `json:"slug"`
	Role    project.Role `json:"role"`
	Changed bool         `json:"changed"`
}

// view builds the wire shape of inv at now.
func (a *API) view(inv invite.Invite, now time.Time) inviteView {
	return inviteView{Invite: inv, Code: inv.Code, URL: a.inviteURL(inv.Code), State: inv.StateAt(now)}
}

// handleList writes the project's invites, newest first, with their codes.
func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	invites, err := a.store.List(r.Context(), auth.ProjectUIDFromContext(r.Context()))
	if err != nil {
		a.writeStoreError(w, r, err, "listing invites")
		return
	}
	now := time.Now()
	views := make([]inviteView, 0, len(invites))
	for _, inv := range invites {
		views = append(views, a.view(inv, now))
	}
	writeJSON(w, http.StatusOK, listResponse{Invites: views, DefaultDays: invite.DefaultDays, MaxDays: invite.MaxDays})
}

// handleCreate creates an invite in the request's project and answers 201 with
// it, its code and its URL. It answers 400 invalid_body, 422 invalid_role,
// invalid_days (outside 1..90) or invalid_max_uses (below 1).
func (a *API) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidBody, "")
		return
	}
	in := invite.NewInvite{Role: project.Role(req.Role), MaxUses: req.MaxUses}
	if req.Days != nil {
		if *req.Days < 1 {
			writeError(w, http.StatusUnprocessableEntity, codeInvalidDays, invite.ErrInvalidDays.Error())
			return
		}
		in.Days = *req.Days
	}
	meta := auditMeta(r)
	in.CreatedBy = meta.ActorUID
	inv, err := a.store.Create(r.Context(), auth.ProjectUIDFromContext(r.Context()), in, meta)
	if err != nil {
		a.writeStoreError(w, r, err, "creating an invite")
		return
	}
	writeJSON(w, http.StatusCreated, a.view(inv, time.Now()))
}

// handleRevoke revokes one of the project's invites for good and answers 204;
// another project's invite, or an unknown one, is 404.
func (a *API) handleRevoke(w http.ResponseWriter, r *http.Request) {
	_, err := a.store.Revoke(r.Context(), auth.ProjectUIDFromContext(r.Context()), chi.URLParam(r, "uid"),
		auditMeta(r))
	if err != nil {
		a.writeStoreError(w, r, err, "revoking an invite")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePublic describes the live invite the path's code names: its project's
// name, the role it grants and whether the caller is signed in. Every unusable
// code is the same 404.
func (a *API) handlePublic(w http.ResponseWriter, r *http.Request) {
	inv, err := a.store.Resolve(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		a.writeStoreError(w, r, err, "resolving an invite")
		return
	}
	_, signedIn := auth.UserFromContext(r.Context())
	writeJSON(w, http.StatusOK, publicResponse{
		ProjectName: inv.ProjectName, Role: inv.Role, RegistrationRole: invite.RegistrationRole(inv.Role),
		SignedIn: signedIn,
	})
}

// handleAccept lets the signed-in caller accept the invite the path's code
// names and answers the project's slug to navigate to, the caller's role there
// and whether accepting changed anything. Every unusable code is the same 404.
func (a *API) handleAccept(w http.ResponseWriter, r *http.Request) {
	meta := auditMeta(r)
	out, err := a.store.Accept(r.Context(), chi.URLParam(r, "code"), meta.ActorUID, meta)
	if err != nil {
		a.writeStoreError(w, r, err, "accepting an invite")
		return
	}
	writeJSON(w, http.StatusOK, acceptResponse{Slug: out.ProjectSlug, Role: out.Role, Changed: out.Changed})
}
