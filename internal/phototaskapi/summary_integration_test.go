//go:build integration

package phototaskapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/phototask"
)

// TestSummary_isPerCaller reads GET /tasks/summary as the editor who asks and as
// the viewer who was asked: the queue's shape is the same for both, whose move
// it is differs, and an anonymous request gets nothing.
func TestSummary_isPerCaller(t *testing.T) {
	e := newEnv(t)
	editor := e.login(t, "editor", auth.RoleEditor)
	viewer := e.login(t, "pametnik", auth.RoleViewer)

	asked := e.openTask(t, editor, "V kterém roce?")
	ask, _ := json.Marshal(map[string]string{"user_uid": e.uids["pametnik"]})
	e.do(t, editor, http.MethodPost, "/api/v1/tasks/"+asked.UID+"/participants", ask, http.StatusOK, nil)

	answered := e.openTask(t, editor, "Kdo je vlevo?")
	time.Sleep(10 * time.Millisecond)
	answer, _ := json.Marshal(map[string]string{"body": "Anna."})
	e.do(t, viewer, http.MethodPost, "/api/v1/tasks/"+answered.UID+"/comments", answer, http.StatusCreated, nil)

	closed := e.openTask(t, editor, "Hotovo")
	finish, _ := json.Marshal(map[string]string{"state": "done", "resolution": "Opraveno."})
	e.do(t, editor, http.MethodPatch, "/api/v1/tasks/"+closed.UID, finish, http.StatusOK, nil)

	summary := func(c *http.Client) phototask.Summary {
		t.Helper()
		var got phototask.Summary
		e.do(t, c, http.MethodGet, "/api/v1/tasks/summary", nil, http.StatusOK, &got)
		return got
	}

	forEditor, forViewer := summary(editor), summary(viewer)
	for name, got := range map[string]phototask.Summary{"editor": forEditor, "viewer": forViewer} {
		if got.ByState["question"] != 2 || got.ByState["done"] != 1 || got.Open != 2 {
			t.Errorf("%s: by_state = %v, open = %d; want 2 questions, 1 done, 2 open",
				name, got.ByState, got.Open)
		}
		if _, ok := got.ByState["rejected"]; !ok {
			t.Errorf("%s: by_state omits an empty state: %v", name, got.ByState)
		}
	}
	if forEditor.WaitingOnMe != 1 || forEditor.Answered != 1 {
		t.Errorf("editor: waiting_on_me = %d, answered = %d; want 1 and 1 (the viewer's answer)",
			forEditor.WaitingOnMe, forEditor.Answered)
	}
	if forViewer.WaitingOnMe != 1 || forViewer.Answered != 0 {
		t.Errorf("viewer: waiting_on_me = %d, answered = %d; want 1 (the question they were asked) and 0",
			forViewer.WaitingOnMe, forViewer.Answered)
	}

	resp := e.mustDo(t, &http.Client{}, http.MethodGet, "/api/v1/tasks/summary", nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous summary status = %d, want 401", resp.StatusCode)
	}
}
