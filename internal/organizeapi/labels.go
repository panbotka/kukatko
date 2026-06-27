package organizeapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

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
	label, err := a.labels.CreateLabel(r.Context(), in.toLabel())
	if err != nil {
		status, msg := labelStatus(err)
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusCreated, label)
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
	in, err := decodeLabelInput(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	label, err := a.labels.UpdateLabel(r.Context(), chi.URLParam(r, "uid"), in.toUpdate())
	if err != nil {
		status, msg := labelStatus(err)
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusOK, label)
}

// handleLabelDelete removes the label identified by the path UID, answering 204
// on success or 404 if no such label exists.
func (a *API) handleLabelDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.labels.DeleteLabel(r.Context(), chi.URLParam(r, "uid")); err != nil {
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
	in, err := decodeLabelPhoto(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	err = a.labels.AttachLabel(r.Context(), in.PhotoUID, chi.URLParam(r, "uid"), in.Source, in.Uncertainty)
	if err != nil {
		status, msg := labelStatus(err)
		writeError(w, status, msg)
		return
	}
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
	if err := a.labels.DetachLabel(r.Context(), in.PhotoUID, uid); err != nil {
		writeError(w, http.StatusInternalServerError, "detaching label failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
