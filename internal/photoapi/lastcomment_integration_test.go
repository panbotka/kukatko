//go:build integration

package photoapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/phototask"
)

// rawListRow is one listing row decoded loosely, so the test can tell a field
// that is absent from one that is null.
type rawListRow struct {
	UID         string          `json:"uid"`
	LastComment json.RawMessage `json:"last_comment"`
}

// getRawList fetches a listing page and returns its rows undecoded.
func getRawList(t *testing.T, client *http.Client, base, query string) []rawListRow {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, base+"/api/v1/photos?"+query, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d for %q, want 200", resp.StatusCode, query)
	}
	var out struct {
		Photos []rawListRow `json:"photos"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return out.Photos
}

// TestList_taskScopeCarriesLastComment verifies the review ledger's column: a
// task-scoped page carries each photo's newest live comment (with the author's
// name), null for a photo without one, and a soft-deleted comment never counts —
// while the same photos listed without the scope carry no field at all.
func TestList_taskScopeCarriesLastComment(t *testing.T) {
	env := newEnv(t)
	editor, err := env.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: "editor", Email: "editor@example.test", Password: testPassword,
		Role: auth.RoleEditor, DisplayName: "Editor E.",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	client, _ := env.loginAs(t, "editor")
	base := env.server.URL

	commented := env.seedPhoto(t, photos.Photo{Title: "Commented"}, "commented.jpg", 200, 10, 10)
	silent := env.seedPhoto(t, photos.Photo{Title: "Silent"}, "silent.jpg", 10, 200, 10)
	outside := env.seedPhoto(t, photos.Photo{Title: "Outside"}, "outside.jpg", 10, 10, 200)

	task, err := phototask.NewStore(env.db.Pool()).Create(t.Context(), phototask.Task{Title: "Dávka"},
		phototask.Opening{PhotoUIDs: []string{commented.UID, silent.UID}},
		audit.Entry{ActorUID: editor.UID, Action: audit.ActionTaskCreate})
	if err != nil {
		t.Fatalf("creating the task: %v", err)
	}
	write := func(body string) comments.Comment {
		t.Helper()
		c, err := env.comments.Create(t.Context(), comments.PhotoSubject(commented.UID), editor.UID, body,
			audit.Entry{ActorUID: editor.UID, Action: audit.ActionCommentCreate, TargetType: "photos",
				TargetUID: commented.UID})
		if err != nil {
			t.Fatalf("Create(%q): %v", body, err)
		}
		return c
	}
	write("first pass")
	newest := write("moved to 1974 (manual)")
	gone := write("typo, ignore")
	if err := env.comments.Delete(t.Context(), gone.UID, audit.Entry{
		ActorUID: editor.UID, Action: audit.ActionCommentDelete, TargetType: "photos", TargetUID: commented.UID,
	}); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	byUID := func(rows []rawListRow) map[string]json.RawMessage {
		out := make(map[string]json.RawMessage, len(rows))
		for _, r := range rows {
			out[r.UID] = r.LastComment
		}
		return out
	}

	scoped := byUID(getRawList(t, client, base, "task="+task.UID))
	if len(scoped) != 2 {
		t.Fatalf("task scope returned %d rows, want the two members", len(scoped))
	}
	if _, ok := scoped[outside.UID]; ok {
		t.Errorf("a photo outside the task is on the page")
	}
	var got struct {
		UID        string `json:"uid"`
		Body       string `json:"body"`
		AuthorUID  string `json:"author_uid"`
		AuthorName string `json:"author_name"`
		CreatedAt  string `json:"created_at"`
	}
	if err := json.Unmarshal(scoped[commented.UID], &got); err != nil {
		t.Fatalf("decoding last_comment %s: %v", scoped[commented.UID], err)
	}
	if got.UID != newest.UID || got.Body != "moved to 1974 (manual)" {
		t.Errorf("last_comment = %+v, want the newest live comment %s", got, newest.UID)
	}
	if got.AuthorUID != editor.UID || got.AuthorName != "Editor E." || got.CreatedAt == "" {
		t.Errorf("last_comment author = %+v, want the editor by display name with a stamp", got)
	}
	if string(scoped[silent.UID]) != "null" {
		t.Errorf("last_comment of an uncommented member = %s, want an explicit null", scoped[silent.UID])
	}

	plain := byUID(getRawList(t, client, base, ""))
	if len(plain) != 3 {
		t.Fatalf("plain list returned %d rows, want 3", len(plain))
	}
	for uid, raw := range plain {
		if raw != nil {
			t.Errorf("last_comment of %s outside a task scope = %s, want the field absent", uid, raw)
		}
	}
}
