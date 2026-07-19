package organizeapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/organize"
)

// labelsResponse is the JSON body returned by the label-list endpoint.
type labelsResponse struct {
	Labels []organize.LabelCount `json:"labels"`
}

// handleLabelList returns every label with its photo count, ordered by priority.
// It answers 500 if the store fails.
func (a *API) handleLabelList(w http.ResponseWriter, r *http.Request) {
	labels, err := a.labels.ListLabels(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing labels failed")
		return
	}
	writeJSON(w, http.StatusOK, labelsResponse{Labels: labels})
}

// handleLabelCreate creates a label from the request body and returns it with its
// generated UID and unique slug. A malformed body or empty name answers 400.
func (a *API) handleLabelCreate(w http.ResponseWriter, r *http.Request) {
	in, err := decodeLabelInput(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	label := in.toLabel()
	entry := a.auditEntry(r, audit.ActionLabelCreate, "labels", "",
		map[string]any{"name": label.Name, "priority": label.Priority})
	created, err := a.labels.CreateLabelAudited(r.Context(), label, entry)
	if err != nil {
		status, msg := labelStatus(err)
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// handleLabelGet returns the label identified by the path UID, or 404 if missing.
func (a *API) handleLabelGet(w http.ResponseWriter, r *http.Request) {
	label, err := a.labels.GetLabelByUID(r.Context(), chi.URLParam(r, "uid"))
	if err != nil {
		status, msg := labelStatus(err)
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusOK, label)
}

// handleLabelUpdate rewrites a label's editable fields (name, priority) and
// returns the refreshed label. A malformed body or empty name answers 400; a
// missing label answers 404.
func (a *API) handleLabelUpdate(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	existing, err := a.labels.GetLabelByUID(r.Context(), uid)
	if err != nil {
		status, msg := labelStatus(err)
		writeError(w, status, msg)
		return
	}
	in, err := decodeLabelInput(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	upd := in.toUpdate()
	details := map[string]any{"name": in.Name, "priority": in.Priority}
	labelChanges(existing, upd).StampInto(details)
	entry := a.auditEntry(r, audit.ActionLabelUpdate, "labels", uid, details)
	label, err := a.labels.UpdateLabelAudited(r.Context(), uid, upd, entry)
	if err != nil {
		status, msg := labelStatus(err)
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusOK, label)
}

// labelChanges builds the old→new diff for a label edit, comparing the label
// before the edit (before) against the update the store will apply (after) and
// recording only the editable fields (name, priority) whose value changed. The
// result is stamped under the audit "changes" key (see internal/audit ChangeSet).
func labelChanges(before organize.Label, after organize.LabelUpdate) *audit.ChangeSet {
	changes := audit.NewChangeSet()
	changes.Add("name", before.Name, after.Name)
	changes.Add("priority", before.Priority, after.Priority)
	return changes
}

// handleLabelDelete removes the label identified by the path UID, answering 204
// on success or 404 if no such label exists.
func (a *API) handleLabelDelete(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	entry := a.auditEntry(r, audit.ActionLabelDelete, "labels", uid, nil)
	if err := a.labels.DeleteLabelAudited(r.Context(), uid, entry); err != nil {
		status, msg := labelStatus(err)
		writeError(w, status, msg)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleLabelAttach attaches the label identified by the path UID to the photo in
// the request body (with an optional source and uncertainty) and answers 204. A
// malformed body or missing photo UID answers 400; an invalid source answers 400;
// a missing label or photo answers 404.
func (a *API) handleLabelAttach(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	in, err := decodeLabelPhoto(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	entry := a.auditEntry(r, audit.ActionLabelAttach, "labels", uid, map[string]any{
		"photo_uid": in.PhotoUID, "source": string(in.Source), "uncertainty": in.Uncertainty,
	})
	err = a.labels.AttachLabelAudited(r.Context(), in.PhotoUID, uid, in.Source, in.Uncertainty, entry)
	if err != nil {
		status, msg := labelStatus(err)
		writeError(w, status, msg)
		return
	}
	a.enqueueSidecar(r.Context(), in.PhotoUID)
	w.WriteHeader(http.StatusNoContent)
}

// handleLabelDetach removes the label identified by the path UID from the photo
// in the request body and answers 204. Detaching a label that is not attached is
// a no-op. A malformed body or missing photo UID answers 400; a missing label
// answers 404.
func (a *API) handleLabelDetach(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	in, err := decodeLabelPhoto(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := a.labels.GetLabelByUID(r.Context(), uid); err != nil {
		status, msg := labelStatus(err)
		writeError(w, status, msg)
		return
	}
	entry := a.auditEntry(r, audit.ActionLabelDetach, "labels", uid, map[string]any{"photo_uid": in.PhotoUID})
	if err := a.labels.DetachLabelAudited(r.Context(), in.PhotoUID, uid, entry); err != nil {
		writeError(w, http.StatusInternalServerError, "detaching label failed")
		return
	}
	a.enqueueSidecar(r.Context(), in.PhotoUID)
	w.WriteHeader(http.StatusNoContent)
}
