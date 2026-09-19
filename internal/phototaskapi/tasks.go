package phototaskapi

import (
	"context"
	"net/http"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/phototask"
)

// handleList writes one page of tasks as {tasks, total, limit, offset}. Every
// authenticated role may read the queue: a person who was sent a link is here to
// see the question, and seeing the rest of the queue does them no harm.
func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	filter, err := parseFilter(r.URL.Query(), user.UID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tasks, total, err := a.store.List(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing tasks failed")
		return
	}
	limit, offset := filter.Page()
	writeJSON(w, http.StatusOK, listResponse{
		Tasks: tasks, Total: total, Limit: limit, Offset: offset,
	})
}

// handleGet writes one task, or 404.
func (a *API) handleGet(w http.ResponseWriter, r *http.Request) {
	task, err := a.store.Get(r.Context(), taskUID(r))
	if err != nil {
		writeTaskError(w, err, "reading task failed")
		return
	}
	writeJSON(w, http.StatusOK, task)
}

// handleCreate opens a task over the given photographs and writes 201 with the
// task as stored.
func (a *API) handleCreate(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	var body createRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(body.PhotoUIDs) > maxPhotoUIDs {
		writeError(w, http.StatusBadRequest, "photo_uids is over the limit")
		return
	}
	created, err := a.store.Create(r.Context(), phototask.Task{
		Title: body.Title, Body: body.Body, Query: body.Query,
		State: phototask.State(body.State),
	}, body.PhotoUIDs, taskEntry(r, user.UID, audit.ActionTaskCreate, ""))
	if err != nil {
		writeTaskError(w, err, "creating task failed")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// handleUpdate folds a partial change onto a task and writes it back. Advancing
// the state is an ordinary edit here — the store owns the rules it drags along,
// including refusing to close a task that does not say how it ended.
func (a *API) handleUpdate(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	var body updateRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	uid := taskUID(r)
	updated, err := a.store.Update(r.Context(), uid, body.toUpdate(),
		taskEntry(r, user.UID, audit.ActionTaskUpdate, uid))
	if err != nil {
		writeTaskError(w, err, "updating task failed")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// handleDelete removes a task and writes 204.
func (a *API) handleDelete(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	uid := taskUID(r)
	if err := a.store.Delete(r.Context(), uid, taskEntry(r, user.UID, audit.ActionTaskDelete, uid)); err != nil {
		writeTaskError(w, err, "deleting task failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAddPhotos adds photographs to a task and writes how many were new
// alongside the task as it now stands.
func (a *API) handleAddPhotos(w http.ResponseWriter, r *http.Request) {
	a.changeMembership(w, r, audit.ActionTaskAddPhotos, a.store.AddPhotos, "adding photos to task failed")
}

// handleRemovePhotos drops photographs from a task and writes how many were
// removed alongside the task as it now stands.
func (a *API) handleRemovePhotos(w http.ResponseWriter, r *http.Request) {
	a.changeMembership(w, r, audit.ActionTaskRemovePhotos, a.store.RemovePhotos,
		"removing photos from task failed")
}

// membershipFunc is the shape both membership store methods share.
type membershipFunc func(
	ctx context.Context, uid string, photoUIDs []string, entry audit.Entry,
) (int, error)

// changeMembership runs one of the two membership mutations and writes the
// common response. The two endpoints differ only in the verb and the audit
// action, so they share everything else rather than being written twice.
func (a *API) changeMembership(
	w http.ResponseWriter, r *http.Request, action string, change membershipFunc, failMsg string,
) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	photoUIDs, err := decodeMembership(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	uid := taskUID(r)
	changed, err := change(r.Context(), uid, photoUIDs, taskEntry(r, user.UID, action, uid))
	if err != nil {
		writeTaskError(w, err, failMsg)
		return
	}
	task, err := a.store.Get(r.Context(), uid)
	if err != nil {
		writeTaskError(w, err, failMsg)
		return
	}
	writeJSON(w, http.StatusOK, membershipResponse{Changed: changed, Task: task})
}
