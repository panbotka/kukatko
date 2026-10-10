//go:build integration

package phototaskapi_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/phototask"
)

// threadOf reads a task's thread as the given client.
func (e *env) threadOf(t *testing.T, c *http.Client, taskUID string) []comments.Comment {
	t.Helper()
	var thread struct {
		Comments []comments.Comment `json:"comments"`
	}
	e.do(t, c, http.MethodGet, "/api/v1/tasks/"+taskUID+"/comments", nil, http.StatusOK, &thread)
	return thread.Comments
}

// taskOf reads one task as the given client.
func (e *env) taskOf(t *testing.T, c *http.Client, taskUID string) phototask.Task {
	t.Helper()
	var task phototask.Task
	e.do(t, c, http.MethodGet, "/api/v1/tasks/"+taskUID, nil, http.StatusOK, &task)
	return task
}

// TestAnswer_handsTheTaskOn verifies "send and hand to the agent": one POST
// writes the comment and moves the task, both audited, and puts the writer on
// the task.
func TestAnswer_handsTheTaskOn(t *testing.T) {
	e := newEnv(t)
	agent := e.login(t, "agent", auth.RoleEditor)
	person := e.login(t, "person", auth.RoleEditor)
	task := e.openTask(t, agent, "Kdo je na fotce?", e.makePhoto(t, "a").UID)

	body, _ := json.Marshal(map[string]any{"body": "Strýc Karel, oprav jméno.", "state": "working"})
	var created comments.Comment
	e.do(t, person, http.MethodPost, "/api/v1/tasks/"+task.UID+"/comments", body,
		http.StatusCreated, &created)
	if created.Body != "Strýc Karel, oprav jméno." || created.TaskUID != task.UID {
		t.Errorf("created = %+v, want the answer on the task", created)
	}

	moved := e.taskOf(t, person, task.UID)
	if moved.State != phototask.StateWorking {
		t.Errorf("state = %q, want working", moved.State)
	}
	personUID := e.uids["person"]
	if moved.StateByUID != personUID {
		t.Errorf("state_by = %q, want the person who handed it over (%q)", moved.StateByUID, personUID)
	}
	if !hasParticipant(moved.Participants, personUID) {
		t.Errorf("participants = %+v, want the writer on the task", moved.Participants)
	}
	if n := len(e.threadOf(t, person, task.UID)); n != 1 {
		t.Errorf("thread has %d comments, want 1", n)
	}
	if n := len(e.auditRows(t, audit.ActionCommentCreate)); n != 1 {
		t.Errorf("comment audit rows = %d, want 1", n)
	}
	// One update row from the hand-over; opening the task audits a create.
	updates := e.auditRows(t, audit.ActionTaskUpdate)
	if len(updates) != 1 || deref(updates[0].TargetUID) != task.UID {
		t.Errorf("task update audit rows = %+v, want one on the task", updates)
	}
}

// TestAnswer_refusesAndWritesNothing verifies the three refusals of an answer
// that carries a state — a viewer moving the task, an unknown state and a close
// without a resolution — each leaving the thread, the task and the audit trail
// exactly as they were. The last one fails inside the transaction, after the
// comment row was inserted, which is what proves the two are one commit.
func TestAnswer_refusesAndWritesNothing(t *testing.T) {
	e := newEnv(t)
	editor := e.login(t, "editor", auth.RoleEditor)
	viewer := e.login(t, "viewer", auth.RoleViewer)
	task := e.openTask(t, editor, "Který rok?", e.makePhoto(t, "a").UID)

	cases := []struct {
		name   string
		client *http.Client
		state  string
		want   int
	}{
		{name: "viewer moving the task", client: viewer, state: "working", want: http.StatusForbidden},
		{name: "unknown state", client: editor, state: "finished", want: http.StatusBadRequest},
		{name: "closing without a resolution", client: editor, state: "done", want: http.StatusBadRequest},
	}
	for _, tc := range cases {
		body, _ := json.Marshal(map[string]any{"body": "1987", "state": tc.state})
		e.do(t, tc.client, http.MethodPost, "/api/v1/tasks/"+task.UID+"/comments", body, tc.want, nil)
		if n := len(e.threadOf(t, editor, task.UID)); n != 0 {
			t.Errorf("%s: thread has %d comments, want none", tc.name, n)
		}
		if got := e.taskOf(t, editor, task.UID).State; got != phototask.StateQuestion {
			t.Errorf("%s: state = %q, want it untouched", tc.name, got)
		}
	}
	if n := len(e.auditRows(t, audit.ActionCommentCreate)); n != 0 {
		t.Errorf("comment audit rows = %d, want none", n)
	}
	if n := len(e.auditRows(t, audit.ActionTaskUpdate)); n != 0 {
		t.Errorf("task update audit rows = %d, want none", n)
	}
	task = e.taskOf(t, editor, task.UID)
	if hasParticipant(task.Participants, e.uids["viewer"]) {
		t.Errorf("participants = %+v, want the refused viewer kept off", task.Participants)
	}

	// The same viewer answering without a state is still welcome.
	plain, _ := json.Marshal(map[string]any{"body": "1987"})
	e.do(t, viewer, http.MethodPost, "/api/v1/tasks/"+task.UID+"/comments", plain, http.StatusCreated, nil)
}

// hasParticipant reports whether userUID is among the people on a task.
func hasParticipant(people []phototask.Participant, userUID string) bool {
	return slices.ContainsFunc(people, func(p phototask.Participant) bool { return p.UserUID == userUID })
}
