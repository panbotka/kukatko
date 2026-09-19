//go:build integration

package phototaskapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/phototask"
)

// participantsBody is the JSON envelope the participant endpoints answer with.
type participantsBody struct {
	Participants []phototask.Participant `json:"participants"`
}

// tasksBody is the listing envelope, enough to assert which tasks came back.
type tasksBody struct {
	Tasks []phototask.Task `json:"tasks"`
	Total int              `json:"total"`
}

// uidOf returns the signed-in account's own uid, as /auth/me reports it.
func (e *env) uidOf(t *testing.T, c *http.Client) string {
	t.Helper()
	var me struct {
		User struct {
			UID string `json:"uid"`
		} `json:"user"`
	}
	e.do(t, c, http.MethodGet, "/api/v1/auth/me", nil, http.StatusOK, &me)
	if me.User.UID == "" {
		t.Fatal("/auth/me returned no uid")
	}
	return me.User.UID
}

// TestParticipants_answeringPutsAViewerOnTheTask is the whole point of the
// feature from a viewer's side: the only thing they can do to a task is answer
// it, so answering has to be what puts them on it.
func TestParticipants_answeringPutsAViewerOnTheTask(t *testing.T) {
	e := newEnv(t)
	editor := e.login(t, "editor", auth.RoleEditor)
	viewer := e.login(t, "pametnik", auth.RoleViewer)
	viewerUID := e.uidOf(t, viewer)

	task := e.openTask(t, editor, "V kterém roce?")

	var before participantsBody
	e.do(t, editor, http.MethodGet, "/api/v1/tasks/"+task.UID+"/participants", nil,
		http.StatusOK, &before)
	if len(before.Participants) != 1 {
		t.Fatalf("participants before the answer = %d, want just the author", len(before.Participants))
	}

	answer, _ := json.Marshal(map[string]string{"body": "1987, podle kroniky."})
	e.do(t, viewer, http.MethodPost, "/api/v1/tasks/"+task.UID+"/comments", answer,
		http.StatusCreated, nil)

	var after participantsBody
	e.do(t, viewer, http.MethodGet, "/api/v1/tasks/"+task.UID+"/participants", nil,
		http.StatusOK, &after)
	found := false
	for _, p := range after.Participants {
		if p.UserUID == viewerUID {
			found = true
			if p.AddedByUID != "" {
				t.Errorf("the answerer was recorded as added by %q, want joined by acting", p.AddedByUID)
			}
		}
	}
	if !found {
		t.Errorf("after answering, participants = %+v, want the answerer among them", after.Participants)
	}

	// And they can now find that question again as one of their own.
	var mine tasksBody
	e.do(t, viewer, http.MethodGet, "/api/v1/tasks?participant=me", nil, http.StatusOK, &mine)
	if mine.Total != 1 || len(mine.Tasks) != 1 || mine.Tasks[0].UID != task.UID {
		t.Errorf("participant=me = %+v, want the one they answered", mine.Tasks)
	}
}

// TestParticipants_assignIsForWriters verifies putting somebody else's name
// against a question is curation, while answering one is not.
func TestParticipants_assignIsForWriters(t *testing.T) {
	e := newEnv(t)
	editor := e.login(t, "editor", auth.RoleEditor)
	viewer := e.login(t, "pametnik", auth.RoleViewer)
	viewerUID := e.uidOf(t, viewer)
	task := e.openTask(t, editor, "Kdo je ta žena vlevo?")

	body, _ := json.Marshal(map[string]string{"user_uid": viewerUID})
	// A viewer may not put anybody on a task, including themselves.
	e.do(t, viewer, http.MethodPost, "/api/v1/tasks/"+task.UID+"/participants", body,
		http.StatusForbidden, nil)

	var after participantsBody
	e.do(t, editor, http.MethodPost, "/api/v1/tasks/"+task.UID+"/participants", body,
		http.StatusOK, &after)
	var asked *phototask.Participant
	for i := range after.Participants {
		if after.Participants[i].UserUID == viewerUID {
			asked = &after.Participants[i]
		}
	}
	if asked == nil {
		t.Fatalf("participants = %+v, want the asked person among them", after.Participants)
	}
	if asked.AddedByUID == "" {
		t.Error("an assigned participant has no added_by, so the UI cannot tell being asked from acting")
	}

	// The person can see it as theirs before doing anything at all.
	var mine tasksBody
	e.do(t, viewer, http.MethodGet, "/api/v1/tasks?participant=me", nil, http.StatusOK, &mine)
	if mine.Total != 1 {
		t.Errorf("participant=me for the asked person = %d tasks, want 1", mine.Total)
	}

	var left participantsBody
	e.do(t, editor, http.MethodDelete, "/api/v1/tasks/"+task.UID+"/participants/"+viewerUID, nil,
		http.StatusOK, &left)
	for _, p := range left.Participants {
		if p.UserUID == viewerUID {
			t.Errorf("after unassigning, participants still contain %s", viewerUID)
		}
	}
}

// TestParticipants_refusesAnUnknownAccount verifies a uid that names nobody is a
// 404 rather than a 500 from the foreign key.
func TestParticipants_refusesAnUnknownAccount(t *testing.T) {
	e := newEnv(t)
	editor := e.login(t, "editor", auth.RoleEditor)
	task := e.openTask(t, editor, "Kdy?")

	body, _ := json.Marshal(map[string]string{"user_uid": "us-nobody"})
	e.do(t, editor, http.MethodPost, "/api/v1/tasks/"+task.UID+"/participants", body,
		http.StatusNotFound, nil)

	empty, _ := json.Marshal(map[string]string{"user_uid": ""})
	e.do(t, editor, http.MethodPost, "/api/v1/tasks/"+task.UID+"/participants", empty,
		http.StatusBadRequest, nil)
}

// TestDirectory_namesEverybodyToASignedInReader verifies the picker's source:
// every signed-in role may read the people of the library, and the reply carries
// names and uids but nothing administrative.
func TestDirectory_namesEverybodyToASignedInReader(t *testing.T) {
	e := newEnv(t)
	e.login(t, "editor", auth.RoleEditor)
	viewer := e.login(t, "pametnik", auth.RoleViewer)

	var body struct {
		Users []map[string]any `json:"users"`
	}
	e.do(t, viewer, http.MethodGet, "/api/v1/users", nil, http.StatusOK, &body)
	if len(body.Users) < 2 {
		t.Fatalf("directory = %v, want both accounts", body.Users)
	}
	for _, entry := range body.Users {
		if entry["uid"] == "" || entry["name"] == "" {
			t.Errorf("directory entry %v is missing a uid or a name", entry)
		}
		for _, leaked := range []string{"email", "role", "disabled", "password_hash", "approved_at"} {
			if _, present := entry[leaked]; present {
				t.Errorf("directory entry leaks %q: %v", leaked, entry)
			}
		}
	}

	// Anonymous callers get nothing.
	anon := &http.Client{}
	resp := e.mustDo(t, anon, http.MethodGet, "/api/v1/users", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous directory status = %d, want 401", resp.StatusCode)
	}
}
