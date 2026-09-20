//go:build integration

package phototaskapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/phototask"
)

// auditRows returns the audit rows carrying the given action, newest first.
func (e *env) auditRows(t *testing.T, action string) []audit.Record {
	t.Helper()
	rows, err := audit.NewStore(e.db.Pool()).List(context.Background(), audit.Filter{Action: action})
	if err != nil {
		t.Fatalf("listing audit rows: %v", err)
	}
	return rows
}

// deref reads an optional audit column, an absent one as the empty string.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// TestCreate_handsTheTaskToSomebody verifies the one-request hand-over: the
// people named in `participants` are on the new task as asked by its creator,
// each audited as an assign, and the creator is on it by acting.
func TestCreate_handsTheTaskToSomebody(t *testing.T) {
	e := newEnv(t)
	editor := e.login(t, "editor", auth.RoleEditor)
	editorUID := e.uidOf(t, editor)
	e.login(t, "agent", auth.RoleEditor)
	e.login(t, "anna", auth.RoleViewer)
	photo := e.makePhoto(t, "a")

	body, _ := json.Marshal(map[string]any{
		"title": "Do kterých alb patří nováčci?", "state": "working",
		"photo_uids":   []string{photo.UID},
		"participants": []string{e.uids["agent"], e.uids["anna"]},
	})
	var task phototask.Task
	e.do(t, editor, http.MethodPost, "/api/v1/tasks", body, http.StatusCreated, &task)

	if task.State != phototask.StateWorking || task.PhotoCount != 1 {
		t.Errorf("task = state %q, %d photos; want working over one photo", task.State, task.PhotoCount)
	}
	addedBy := make(map[string]string, len(task.Participants))
	for _, p := range task.Participants {
		addedBy[p.UserUID] = p.AddedByUID
	}
	want := map[string]string{editorUID: "", e.uids["agent"]: editorUID, e.uids["anna"]: editorUID}
	if len(addedBy) != len(want) {
		t.Fatalf("participants = %+v, want the creator and the two asked", task.Participants)
	}
	for uid, by := range want {
		if addedBy[uid] != by {
			t.Errorf("participant %s added_by = %q, want %q", uid, addedBy[uid], by)
		}
	}

	rows := e.auditRows(t, audit.ActionTaskAssign)
	if len(rows) != 2 {
		t.Fatalf("assign audit rows = %d, want one per person asked", len(rows))
	}
	for _, row := range rows {
		if deref(row.ActorUID) != editorUID || deref(row.TargetUID) != task.UID {
			t.Errorf("assign row actor %q target %q, want the creator and the task",
				deref(row.ActorUID), deref(row.TargetUID))
		}
		if _, ok := row.Details["user_uid"]; !ok {
			t.Errorf("assign row details = %v, want user_uid", row.Details)
		}
	}

	// And the person asked is found under "what is Anna on?".
	var mine tasksBody
	e.do(t, editor, http.MethodGet, "/api/v1/tasks?participant="+e.uids["anna"], nil, http.StatusOK, &mine)
	if mine.Total != 1 {
		t.Errorf("participant filter for the asked person = %d tasks, want 1", mine.Total)
	}
}

// TestCreate_unknownParticipantIs404 verifies a hand-over to nobody fails as an
// unknown user does everywhere else, and opens no task at all.
func TestCreate_unknownParticipantIs404(t *testing.T) {
	e := newEnv(t)
	editor := e.login(t, "editor", auth.RoleEditor)

	body, _ := json.Marshal(map[string]any{
		"title": "Komu?", "participants": []string{"us-nobody"},
	})
	e.do(t, editor, http.MethodPost, "/api/v1/tasks", body, http.StatusNotFound, nil)

	var all tasksBody
	e.do(t, editor, http.MethodGet, "/api/v1/tasks", nil, http.StatusOK, &all)
	if all.Total != 0 {
		t.Errorf("after the refusal the queue holds %d tasks, want none", all.Total)
	}
}

// TestCreate_withoutPhotos verifies a task may be about the library rather than
// about particular photographs: absent and empty `photo_uids` both open it, it
// reads back with no cover and no count, and it lists like any other.
func TestCreate_withoutPhotos(t *testing.T) {
	e := newEnv(t)
	editor := e.login(t, "editor", auth.RoleEditor)

	for name, body := range map[string]map[string]any{
		"absent": {"title": "Přejmenovat wf: štítky"},
		"empty":  {"title": "Do kterých alb patří nováčci?", "photo_uids": []string{}},
	} {
		t.Run(name, func(t *testing.T) {
			payload, _ := json.Marshal(body)
			var task phototask.Task
			e.do(t, editor, http.MethodPost, "/api/v1/tasks", payload, http.StatusCreated, &task)
			if task.PhotoCount != 0 || task.CoverPhotoUID != "" {
				t.Errorf("created = %d photos, cover %q; want none", task.PhotoCount, task.CoverPhotoUID)
			}
			var read phototask.Task
			e.do(t, editor, http.MethodGet, "/api/v1/tasks/"+task.UID, nil, http.StatusOK, &read)
			if read.PhotoCount != 0 || read.CoverPhotoUID != "" || read.Title != body["title"] {
				t.Errorf("read back = %+v, want the same task with no photographs", read)
			}
		})
	}

	var all tasksBody
	e.do(t, editor, http.MethodGet, "/api/v1/tasks", nil, http.StatusOK, &all)
	if all.Total != 2 {
		t.Errorf("listing = %d tasks, want both photo-less ones", all.Total)
	}
}
