//go:build integration

package photoapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/photos"
)

// apiComment is the wire shape of one comment, decoding only what the assertions
// need.
type apiComment struct {
	UID        string  `json:"uid"`
	PhotoUID   string  `json:"photo_uid"`
	AuthorUID  string  `json:"author_uid"`
	AuthorName string  `json:"author_name"`
	Body       string  `json:"body"`
	EditedAt   *string `json:"edited_at"`
}

// apiCommentList is the thread endpoint's body.
type apiCommentList struct {
	Comments []apiComment `json:"comments"`
}

// commentsURL builds a photo's thread endpoint URL.
func commentsURL(base, photoUID string) string {
	return base + "/api/v1/photos/" + photoUID + "/comments"
}

// commentURL builds the URL of one comment inside a photo's thread.
func commentURL(base, photoUID, commentUID string) string {
	return commentsURL(base, photoUID) + "/" + commentUID
}

// commentBody marshals a comment request body.
func commentBody(t *testing.T, body string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		t.Fatalf("marshal comment body: %v", err)
	}
	return raw
}

// postComment writes a comment as the given client and asserts the status,
// returning the created record (zero-valued when the request was rejected).
func postComment(t *testing.T, client *http.Client, base, photoUID, body string, want int) apiComment {
	t.Helper()
	resp := mustDo(t, client, http.MethodPost, commentsURL(base, photoUID), commentBody(t, body))
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != want {
		t.Fatalf("POST comment status = %d, want %d", resp.StatusCode, want)
	}
	if want != http.StatusCreated {
		return apiComment{}
	}
	var out apiComment
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode created comment: %v", err)
	}
	return out
}

// getComments fetches a photo's thread and asserts a 200.
func getComments(t *testing.T, client *http.Client, base, photoUID string) []apiComment {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, commentsURL(base, photoUID), nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET comments status = %d, want 200", resp.StatusCode)
	}
	var out apiCommentList
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode comments: %v", err)
	}
	return out.Comments
}

// getCommentCount fetches a photo's detail and returns its comment_count.
func getCommentCount(t *testing.T, client *http.Client, base, photoUID string) int {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, base+"/api/v1/photos/"+photoUID, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		CommentCount int `json:"comment_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	return out.CommentCount
}

// countAuditAction returns how many audit rows exist for the given action.
func (e *env) countAuditAction(t *testing.T, action string) int {
	t.Helper()
	n, err := audit.NewStore(e.db.Pool()).Count(t.Context(), audit.Filter{Action: action})
	if err != nil {
		t.Fatalf("counting audit %q: %v", action, err)
	}
	return n
}

// TestCommentsAPI_readAndWriteRBAC exercises the role matrix: every authenticated
// role may read a thread and write into it — a viewer included, which is the
// deliberate exception to the read-only rule — while editing is the author's
// alone and deleting is the author's or an admin's.
func TestCommentsAPI_readAndWriteRBAC(t *testing.T) {
	e := newEnv(t)
	viewer, _ := e.login(t, "cm_viewer", auth.RoleViewer)
	editor, _ := e.login(t, "cm_editor", auth.RoleEditor)
	admin, _ := e.login(t, "cm_admin", auth.RoleAdmin)
	photo := e.seedPhoto(t, photos.Photo{Title: "one"}, "1.jpg", 10, 20, 30)

	t.Run("a viewer may write a comment", func(t *testing.T) {
		created := postComment(t, viewer, e.server.URL, photo.UID, "  Kdo je vlevo?  ", http.StatusCreated)
		if created.Body != "Kdo je vlevo?" {
			t.Errorf("body = %q, want it trimmed", created.Body)
		}
		if created.AuthorName != "cm_viewer" {
			t.Errorf("author_name = %q, want the username fallback", created.AuthorName)
		}
		if created.PhotoUID != photo.UID {
			t.Errorf("photo_uid = %q, want %q", created.PhotoUID, photo.UID)
		}
		if created.EditedAt != nil {
			t.Errorf("edited_at = %v on a fresh comment, want null", *created.EditedAt)
		}
	})

	viewerComment := getComments(t, viewer, e.server.URL, photo.UID)[0]

	t.Run("every role may read the thread", func(t *testing.T) {
		for name, client := range map[string]*http.Client{"viewer": viewer, "editor": editor, "admin": admin} {
			list := getComments(t, client, e.server.URL, photo.UID)
			if len(list) != 1 || list[0].UID != viewerComment.UID {
				t.Errorf("%s reads %d comments, want the viewer's one", name, len(list))
			}
		}
	})

	t.Run("only the author may edit", func(t *testing.T) {
		for name, client := range map[string]*http.Client{"editor": editor, "admin": admin} {
			resp := mustDo(t, client, http.MethodPatch,
				commentURL(e.server.URL, photo.UID, viewerComment.UID), commentBody(t, "přepsáno"))
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("%s editing someone else's comment = %d, want 403", name, resp.StatusCode)
			}
		}
		resp := mustDo(t, viewer, http.MethodPatch,
			commentURL(e.server.URL, photo.UID, viewerComment.UID), commentBody(t, "Kdo je ten kluk vlevo?"))
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("the author editing own comment = %d, want 200", resp.StatusCode)
		}
		var edited apiComment
		if err := json.NewDecoder(resp.Body).Decode(&edited); err != nil {
			t.Fatalf("decode edited comment: %v", err)
		}
		if edited.Body != "Kdo je ten kluk vlevo?" {
			t.Errorf("body = %q after the edit", edited.Body)
		}
		if edited.EditedAt == nil {
			t.Error("edited_at = null after an edit, want a timestamp")
		}
	})

	t.Run("an editor may not delete someone else's comment", func(t *testing.T) {
		mustStatus(t, editor, http.MethodDelete,
			commentURL(e.server.URL, photo.UID, viewerComment.UID), http.StatusForbidden)
	})

	t.Run("an admin may delete anyone's comment", func(t *testing.T) {
		mustStatus(t, admin, http.MethodDelete,
			commentURL(e.server.URL, photo.UID, viewerComment.UID), http.StatusNoContent)
		if list := getComments(t, viewer, e.server.URL, photo.UID); len(list) != 0 {
			t.Errorf("thread after the admin delete = %d comments, want 0", len(list))
		}
	})

	t.Run("the author may delete their own comment", func(t *testing.T) {
		own := postComment(t, editor, e.server.URL, photo.UID, "moje", http.StatusCreated)
		mustStatus(t, editor, http.MethodDelete,
			commentURL(e.server.URL, photo.UID, own.UID), http.StatusNoContent)
		// Deleting twice is a not-found, not a silent success.
		mustStatus(t, editor, http.MethodDelete,
			commentURL(e.server.URL, photo.UID, own.UID), http.StatusNotFound)
	})

	t.Run("an anonymous caller is turned away", func(t *testing.T) {
		anon := &http.Client{}
		mustStatus(t, anon, http.MethodGet, commentsURL(e.server.URL, photo.UID), http.StatusUnauthorized)
		resp := mustDo(t, anon, http.MethodPost, commentsURL(e.server.URL, photo.UID), commentBody(t, "hi"))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("anonymous POST = %d, want 401", resp.StatusCode)
		}
	})
}

// TestCommentsAPI_ordering verifies a thread reads oldest first, so a
// conversation reads forwards.
func TestCommentsAPI_ordering(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.login(t, "cm_order_a", auth.RoleViewer)
	bob, _ := e.login(t, "cm_order_b", auth.RoleEditor)
	photo := e.seedPhoto(t, photos.Photo{Title: "one"}, "1.jpg", 10, 20, 30)

	first := postComment(t, alice, e.server.URL, photo.UID, "první", http.StatusCreated)
	second := postComment(t, bob, e.server.URL, photo.UID, "druhý", http.StatusCreated)
	third := postComment(t, alice, e.server.URL, photo.UID, "třetí", http.StatusCreated)

	list := getComments(t, bob, e.server.URL, photo.UID)
	want := []string{first.UID, second.UID, third.UID}
	if len(list) != len(want) {
		t.Fatalf("thread = %d comments, want %d", len(list), len(want))
	}
	for i, uid := range want {
		if list[i].UID != uid {
			t.Errorf("comment %d = %s, want %s (oldest first)", i, list[i].UID, uid)
		}
	}
}

// TestCommentsAPI_validation verifies the body rules and the addressing rules: a
// blank or over-long body is a 400, a comment reached through the wrong photo (or
// one that does not exist) is a 404, and a comment on a missing photo is a 404.
func TestCommentsAPI_validation(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.login(t, "cm_val", auth.RoleViewer)
	photo := e.seedPhoto(t, photos.Photo{Title: "one"}, "1.jpg", 10, 20, 30)
	other := e.seedPhoto(t, photos.Photo{Title: "two"}, "2.jpg", 40, 50, 60)

	t.Run("a blank body is rejected", func(t *testing.T) {
		postComment(t, alice, e.server.URL, photo.UID, "   \n ", http.StatusBadRequest)
	})

	t.Run("an over-long body is rejected", func(t *testing.T) {
		postComment(t, alice, e.server.URL, photo.UID,
			strings.Repeat("a", comments.MaxBodyLen+1), http.StatusBadRequest)
	})

	t.Run("a body at the limit is accepted", func(t *testing.T) {
		postComment(t, alice, e.server.URL, photo.UID,
			strings.Repeat("ě", comments.MaxBodyLen), http.StatusCreated)
	})

	t.Run("an unknown field is rejected", func(t *testing.T) {
		resp := mustDo(t, alice, http.MethodPost, commentsURL(e.server.URL, photo.UID),
			[]byte(`{"body":"hi","author_uid":"us_x"}`))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("POST with an unknown field = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("commenting on a missing photo is 404", func(t *testing.T) {
		postComment(t, alice, e.server.URL, "ph_missing", "hello", http.StatusNotFound)
	})

	t.Run("a missing comment is 404", func(t *testing.T) {
		mustStatus(t, alice, http.MethodDelete,
			commentURL(e.server.URL, photo.UID, "cm_missing"), http.StatusNotFound)
	})

	t.Run("a comment reached through the wrong photo is 404", func(t *testing.T) {
		created := postComment(t, alice, e.server.URL, photo.UID, "patří k jedničce", http.StatusCreated)
		mustStatus(t, alice, http.MethodDelete,
			commentURL(e.server.URL, other.UID, created.UID), http.StatusNotFound)
		resp := mustDo(t, alice, http.MethodPatch,
			commentURL(e.server.URL, other.UID, created.UID), commentBody(t, "změna"))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("PATCH through the wrong photo = %d, want 404", resp.StatusCode)
		}
		// The comment is untouched on its own photo.
		if list := getComments(t, alice, e.server.URL, photo.UID); len(list) != 2 {
			t.Errorf("thread = %d comments, want the 2 that were written", len(list))
		}
	})

	t.Run("an edit is validated like a create", func(t *testing.T) {
		created := postComment(t, alice, e.server.URL, photo.UID, "text", http.StatusCreated)
		resp := mustDo(t, alice, http.MethodPatch,
			commentURL(e.server.URL, photo.UID, created.UID), commentBody(t, "  "))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("PATCH with a blank body = %d, want 400", resp.StatusCode)
		}
	})
}

// TestCommentsAPI_countAndSoftDelete verifies the detail's comment_count tracks
// the live thread, and that a delete is soft: the row survives in the table while
// disappearing from every read and from the count.
func TestCommentsAPI_countAndSoftDelete(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.login(t, "cm_count", auth.RoleViewer)
	photo := e.seedPhoto(t, photos.Photo{Title: "one"}, "1.jpg", 10, 20, 30)
	quiet := e.seedPhoto(t, photos.Photo{Title: "two"}, "2.jpg", 40, 50, 60)

	if got := getCommentCount(t, alice, e.server.URL, photo.UID); got != 0 {
		t.Fatalf("comment_count on a fresh photo = %d, want 0", got)
	}
	first := postComment(t, alice, e.server.URL, photo.UID, "a", http.StatusCreated)
	postComment(t, alice, e.server.URL, photo.UID, "b", http.StatusCreated)
	if got := getCommentCount(t, alice, e.server.URL, photo.UID); got != 2 {
		t.Fatalf("comment_count = %d, want 2", got)
	}
	if got := getCommentCount(t, alice, e.server.URL, quiet.UID); got != 0 {
		t.Errorf("comment_count of the other photo = %d, want 0 (no leakage)", got)
	}

	mustStatus(t, alice, http.MethodDelete,
		commentURL(e.server.URL, photo.UID, first.UID), http.StatusNoContent)

	if got := getCommentCount(t, alice, e.server.URL, photo.UID); got != 1 {
		t.Errorf("comment_count after the delete = %d, want 1", got)
	}
	list := getComments(t, alice, e.server.URL, photo.UID)
	if len(list) != 1 || list[0].Body != "b" {
		t.Errorf("thread after the delete = %v, want only the second comment", list)
	}

	var kept int
	if err := e.db.Pool().QueryRow(t.Context(),
		"SELECT count(*) FROM photo_comments WHERE uid = $1 AND deleted_at IS NOT NULL",
		first.UID).Scan(&kept); err != nil {
		t.Fatalf("counting the deleted row: %v", err)
	}
	if kept != 1 {
		t.Errorf("the deleted comment's row is gone (%d rows), want it soft-deleted", kept)
	}

	// Editing a deleted comment finds nothing to edit.
	resp := mustDo(t, alice, http.MethodPatch,
		commentURL(e.server.URL, photo.UID, first.UID), commentBody(t, "zpět"))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("PATCH of a deleted comment = %d, want 404", resp.StatusCode)
	}
}

// TestCommentsAPI_auditTrail verifies every comment mutation lands in the audit
// log against the photo, with the comment's UID in the details.
func TestCommentsAPI_auditTrail(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.login(t, "cm_audit", auth.RoleViewer)
	photo := e.seedPhoto(t, photos.Photo{Title: "one"}, "1.jpg", 10, 20, 30)

	created := postComment(t, alice, e.server.URL, photo.UID, "text", http.StatusCreated)
	resp := mustDo(t, alice, http.MethodPatch,
		commentURL(e.server.URL, photo.UID, created.UID), commentBody(t, "jiný text"))
	_ = resp.Body.Close()
	mustStatus(t, alice, http.MethodDelete,
		commentURL(e.server.URL, photo.UID, created.UID), http.StatusNoContent)

	for _, action := range []string{
		audit.ActionCommentCreate, audit.ActionCommentUpdate, audit.ActionCommentDelete,
	} {
		if got := e.countAuditAction(t, action); got != 1 {
			t.Errorf("%s audit rows = %d, want 1", action, got)
		}
	}

	rows, err := audit.NewStore(e.db.Pool()).List(t.Context(),
		audit.Filter{Action: audit.ActionCommentCreate})
	if err != nil {
		t.Fatalf("listing audit: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("comment.create rows = %d, want 1", len(rows))
	}
	if rows[0].TargetType != "photos" || rows[0].TargetUID == nil || *rows[0].TargetUID != photo.UID {
		t.Errorf("audit target = %s/%v, want photos/%s", rows[0].TargetType, rows[0].TargetUID, photo.UID)
	}
	if rows[0].Details["comment_uid"] != created.UID {
		t.Errorf("audit details comment_uid = %v, want %s", rows[0].Details["comment_uid"], created.UID)
	}
	if rows[0].ActorUID == nil || *rows[0].ActorUID == "" {
		t.Error("audit entry has no actor")
	}
}

// TestCommentsAPI_cascadeOnPurge verifies purging a photo takes its thread with
// it: the deletion is permanent, so nothing is left to comment on.
func TestCommentsAPI_cascadeOnPurge(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.login(t, "cm_purge_a", auth.RoleViewer)
	admin, _ := e.login(t, "cm_purge_admin", auth.RoleAdmin)
	doomed := e.seedPhoto(t, photos.Photo{Title: "one"}, "1.jpg", 10, 20, 30)
	kept := e.seedPhoto(t, photos.Photo{Title: "two"}, "2.jpg", 40, 50, 60)

	postComment(t, alice, e.server.URL, doomed.UID, "a", http.StatusCreated)
	postComment(t, alice, e.server.URL, doomed.UID, "b", http.StatusCreated)
	survivor := postComment(t, alice, e.server.URL, kept.UID, "c", http.StatusCreated)

	// A purge only applies to an archived photo.
	mustStatus(t, admin, http.MethodPost,
		e.server.URL+"/api/v1/photos/"+doomed.UID+"/archive", http.StatusOK)
	mustStatus(t, admin, http.MethodPost,
		e.server.URL+"/api/v1/photos/"+doomed.UID+"/purge?confirm=true", http.StatusNoContent)

	counts, err := e.comments.CountsAmong(t.Context(), []string{doomed.UID, kept.UID})
	if err != nil {
		t.Fatalf("CountsAmong: %v", err)
	}
	if counts[doomed.UID] != 0 {
		t.Errorf("the purged photo still has %d comments, want 0 (ON DELETE CASCADE)", counts[doomed.UID])
	}
	if counts[kept.UID] != 1 {
		t.Errorf("the other photo has %d comments, want 1", counts[kept.UID])
	}
	if _, err := e.comments.Get(t.Context(), survivor.UID); err != nil {
		t.Errorf("the other photo's comment did not survive the purge: %v", err)
	}
}
