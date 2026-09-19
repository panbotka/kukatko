package phototaskapi

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/phototask"
)

// participantListResponse is the JSON body of the participants endpoint.
// Participants is always an array (never null) so a client can render "nobody
// yet" without a guard.
type participantListResponse struct {
	Participants []phototask.Participant `json:"participants"`
}

// assignRequest is the JSON body of the assign endpoint: whom to put on the
// task.
type assignRequest struct {
	UserUID string `json:"user_uid"`
}

// handleListParticipants returns the people on a task, oldest membership first.
// Every authenticated role may read it — knowing who else is on a question is
// part of deciding whether to answer it.
func (a *API) handleListParticipants(w http.ResponseWriter, r *http.Request) {
	people, err := a.store.Participants(r.Context(), taskUID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing participants failed")
		return
	}
	writeJSON(w, http.StatusOK, participantListResponse{Participants: people})
}

// handleAssign puts somebody on a task on purpose — asking a particular person.
// Joining by acting needs no endpoint: the writes do it themselves.
//
// It needs write access, unlike the thread: adding your own answer is a viewer's
// business, but putting somebody else's name against a question is curation.
func (a *API) handleAssign(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	var body assignRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.UserUID == "" {
		writeError(w, http.StatusBadRequest, "user_uid is required")
		return
	}
	uid := taskUID(r)
	if err := a.store.Assign(r.Context(), uid, body.UserUID,
		taskEntry(r, user.UID, audit.ActionTaskAssign, uid)); err != nil {
		writeTaskError(w, err, "assigning failed")
		return
	}
	a.writeParticipants(w, r, uid)
}

// handleUnassign takes somebody off a task. Somebody who was not on it is not an
// error: the caller asked for an end state and that is the end state.
func (a *API) handleUnassign(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	uid := taskUID(r)
	target := chi.URLParam(r, "userUID")
	if _, err := a.store.Unassign(r.Context(), uid, target,
		taskEntry(r, user.UID, audit.ActionTaskUnassign, uid)); err != nil {
		writeTaskError(w, err, "unassigning failed")
		return
	}
	a.writeParticipants(w, r, uid)
}

// writeParticipants answers a membership change with the list as it now stands,
// so the caller never has to follow a write with a read to redraw.
func (a *API) writeParticipants(w http.ResponseWriter, r *http.Request, uid string) {
	people, err := a.store.Participants(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading participants failed")
		return
	}
	writeJSON(w, http.StatusOK, participantListResponse{Participants: people})
}

// joinAuthor puts the writer of a comment on the task, which is how answering a
// question makes somebody a participant in it.
//
// It runs after the comment has landed rather than with it: the thread belongs
// to internal/comments, which knows nothing about tasks, and threading a task
// store through it to save one statement would couple the two for no gain. A
// failure here is therefore logged and swallowed — the answer is written and
// must not be reported as failed over its bookkeeping, and the next thing the
// author does to the task puts them on it anyway.
func (a *API) joinAuthor(r *http.Request, taskUID, authorUID string) {
	if err := a.store.Join(r.Context(), taskUID, authorUID); err != nil {
		logJoinFailure(taskUID, authorUID, err)
	}
}

// logJoinFailure records that somebody's participation could not be written.
// Separated from joinAuthor so the swallowing is in one place and obvious.
func logJoinFailure(taskUID, userUID string, err error) {
	log.Printf("phototaskapi: joining %s to task %s: %v", userUID, taskUID, err)
}
