//go:build integration

package organizeapi_test

import (
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/organize"
)

// curatorStep is one request of the curator's walk through the ten album and
// label writes, with the status the route answers on success.
type curatorStep struct {
	method, path, body string
	want               int
}

// TestRoleEnforcement_curatorOwnsAlbumsAndLabels proves all ten album and label
// writes hang on RequireCurator through the real auth stack: a curator creates,
// edits, fills, empties and deletes an album and a label — deletion included,
// since that throws away a stitching-together, not a photograph — while a viewer
// is refused every one of the same writes.
func TestRoleEnforcement_curatorOwnsAlbumsAndLabels(t *testing.T) {
	env := newEnv(t)
	curator := env.login(t, "curator", auth.RoleCurator)
	viewer := env.login(t, "viewer", auth.RoleViewer)
	photo := env.seedPhoto(t, "cur1")

	resp := env.mustDo(t, curator, http.MethodPost, "/api/v1/albums", []byte(`{"title":"Pouť"}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("curator POST /albums = %d, want 201", resp.StatusCode)
	}
	var album organize.Album
	decodeBody(t, resp, &album)
	resp = env.mustDo(t, curator, http.MethodPost, "/api/v1/labels", []byte(`{"name":"Průvod"}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("curator POST /labels = %d, want 201", resp.StatusCode)
	}
	var label organize.Label
	decodeBody(t, resp, &label)

	albumPath, labelPath := "/api/v1/albums/"+album.UID, "/api/v1/labels/"+label.UID
	steps := []curatorStep{
		{http.MethodPatch, albumPath, `{"title":"Pouť 2026"}`, http.StatusOK},
		{http.MethodPost, albumPath + "/photos", `{"photo_uids":["` + photo + `"]}`, http.StatusOK},
		{http.MethodDelete, albumPath + "/photos", `{"photo_uids":["` + photo + `"]}`, http.StatusOK},
		{http.MethodPatch, labelPath, `{"name":"Procesí"}`, http.StatusOK},
		{http.MethodPost, labelPath + "/photos", `{"photo_uid":"` + photo + `","source":"manual"}`, http.StatusNoContent},
		{http.MethodDelete, labelPath + "/photos", `{"photo_uid":"` + photo + `"}`, http.StatusNoContent},
		{http.MethodDelete, albumPath, ``, http.StatusNoContent},
		{http.MethodDelete, labelPath, ``, http.StatusNoContent},
	}
	// The viewer goes first, so each of its refusals hits a route whose target
	// still exists and a 403 cannot be a 404 in disguise.
	for _, step := range append([]curatorStep{
		{http.MethodPost, "/api/v1/albums", `{"title":"Trip"}`, 0},
		{http.MethodPost, "/api/v1/labels", `{"name":"Beach"}`, 0},
	}, steps...) {
		resp := env.mustDo(t, viewer, step.method, step.path, bodyOf(step.body))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("viewer %s %s = %d, want 403", step.method, step.path, resp.StatusCode)
		}
	}
	for _, step := range steps {
		resp := env.mustDo(t, curator, step.method, step.path, bodyOf(step.body))
		_ = resp.Body.Close()
		if resp.StatusCode != step.want {
			t.Errorf("curator %s %s = %d, want %d", step.method, step.path, resp.StatusCode, step.want)
		}
	}
}

// bodyOf returns body as a request payload, or nil for a bodiless request.
func bodyOf(body string) []byte {
	if body == "" {
		return nil
	}
	return []byte(body)
}
