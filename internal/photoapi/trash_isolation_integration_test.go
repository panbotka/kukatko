//go:build integration

package photoapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/trash"
)

// These are the HTTP-level tenant-isolation tests of the trash: B's member sees
// only B's archived photos, cannot purge A's (the answer is the one a missing
// photo gets) and empties only B's trash.

// archiveBoth puts both seeded photos into their project's trash.
func archiveBoth(t *testing.T, e *env, f *projecttest.Fixture) {
	t.Helper()
	if _, err := e.db.Pool().Exec(t.Context(),
		`UPDATE photos SET archived_at = now() - interval '3 days' WHERE uid = ANY($1)`,
		[]string{f.A.PhotoUID, f.B.PhotoUID}); err != nil {
		t.Fatalf("archiving the seeded photos: %v", err)
	}
}

// TestTrash_listingAndPurgeStayInTheProject lists B's trash (A's archived photo
// never appears), purges A's photo from B (404, the same answer a missing uid
// gets, and A's photo survives) and empties B's trash (only B's photo goes).
func TestTrash_listingAndPurgeStayInTheProject(t *testing.T) {
	e := newEnv(t)
	f := projecttest.Seed(t, e.db.Pool())
	archiveBoth(t, e, f)
	client := f.ClientFor(t, e.server, projecttest.SideB)

	f.AssertListExcludes(t, client, f.PathFor(projecttest.SideB, "photos?archived=only"), f.A.UIDs()...)

	purge := func(uid string) *http.Response {
		return f.Send(t, client, http.MethodPost, f.PathFor(projecttest.SideB, "photos/"+uid+"/purge?confirm=true"), nil)
	}
	foreign := purge(f.A.PhotoUID)
	if foreign.StatusCode != http.StatusNotFound {
		t.Errorf("purging A's photo from B = %d, want 404", foreign.StatusCode)
	}
	projecttest.AssertSameShape(t, foreign, purge(projecttest.MissingUID("ph")))
	assertExists(t, e, f.A.ProjectUID, f.A.PhotoUID)

	empty := f.Send(t, client, http.MethodPost, f.PathFor(projecttest.SideB, "trash/empty?confirm=true"), nil)
	body, err := io.ReadAll(empty.Body)
	if err != nil {
		t.Fatalf("reading the empty-trash answer: %v", err)
	}
	if empty.StatusCode != http.StatusOK {
		t.Fatalf("emptying B's trash = %d, want 200; body %s", empty.StatusCode, body)
	}
	var res trash.Result
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("decoding %s: %v", body, err)
	}
	if res.Purged != 1 || res.Failed != 0 {
		t.Errorf("emptying B's trash = %+v, want exactly B's photo purged", res)
	}
	assertExists(t, e, f.A.ProjectUID, f.A.PhotoUID)
	if _, err := e.store.GetForJob(t.Context(), f.B.PhotoUID); err == nil {
		t.Errorf("B's archived photo survived emptying B's trash")
	}
}

// assertExists fails t unless the project still holds the photo.
func assertExists(t *testing.T, e *env, projectUID, uid string) {
	t.Helper()
	if _, err := e.store.GetByUIDInProject(t.Context(), projectUID, uid); err != nil {
		t.Errorf("photo %s is gone from its project: %v", uid, err)
	}
}
