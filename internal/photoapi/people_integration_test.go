//go:build integration

package photoapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They cover the two endpoints that record who is
// in a picture when the detector saw no face.

// peopleBody is the JSON both mutations — and the detail response — carry.
type peopleBody struct {
	People []people.PhotoSubject `json:"people"`
}

// decodePeople reads a peopleBody from resp and closes it.
func decodePeople(t *testing.T, resp *http.Response) peopleBody {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var body peopleBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding people body: %v", err)
	}
	return body
}

// seedSubject inserts a named subject and returns its uid.
func (e *env) seedSubject(t *testing.T, name string) string {
	t.Helper()
	subject, err := people.NewStore(e.db.Pool()).CreateSubject(t.Context(), people.Subject{Name: name})
	if err != nil {
		t.Fatalf("CreateSubject(%s): %v", name, err)
	}
	return subject.UID
}

// TestAttachPerson_roundTrip walks the whole feature over HTTP: attach, see the
// person on the detail, attach again (idempotent), then detach.
func TestAttachPerson_roundTrip(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "editor", auth.RoleEditor)
	// A video, deliberately: face detection does not run on footage at all, so
	// this endpoint is the only way a clip names anybody.
	photo := env.seedPhoto(t, photos.Photo{Title: "Clip", MediaType: photos.MediaVideo}, "clip.jpg", 10, 20, 30)
	subjectUID := env.seedSubject(t, "Ludmila")
	base := env.server.URL + "/api/v1/photos/" + photo.UID + "/people"

	body, _ := json.Marshal(map[string]string{"subject_uid": subjectUID})
	resp := mustDo(t, client, http.MethodPost, base, body)
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("attach status = %d, want 200", resp.StatusCode)
	}
	attached := decodePeople(t, resp)
	if len(attached.People) != 1 || attached.People[0].Name != "Ludmila" ||
		attached.People[0].Type != people.SubjectPerson {
		t.Fatalf("attach body = %+v, want Ludmila", attached.People)
	}

	detail := getDetailPeople(t, client, env.server.URL, photo.UID)
	if len(detail) != 1 || detail[0].SubjectUID != subjectUID {
		t.Fatalf("detail people = %+v, want the attached subject", detail)
	}

	resp = mustDo(t, client, http.MethodPost, base, body)
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("second attach status = %d, want 200 (idempotent)", resp.StatusCode)
	}
	again := decodePeople(t, resp)
	if len(again.People) != 1 || again.People[0].MarkerUID != attached.People[0].MarkerUID {
		t.Fatalf("second attach = %+v, want the same single link", again.People)
	}

	resp = mustDo(t, client, http.MethodDelete, base+"/"+subjectUID, nil)
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("detach status = %d, want 200", resp.StatusCode)
	}
	if remaining := decodePeople(t, resp); len(remaining.People) != 0 {
		t.Fatalf("detach body = %+v, want nobody left", remaining.People)
	}
	if detail := getDetailPeople(t, client, env.server.URL, photo.UID); len(detail) != 0 {
		t.Fatalf("detail people after detach = %+v, want empty", detail)
	}
}

// getDetailPeople fetches a photo's detail and returns its hand-attached people.
func getDetailPeople(t *testing.T, client *http.Client, base, uid string) []people.PhotoSubject {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, base+"/api/v1/photos/"+uid, nil)
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("detail status = %d, want 200", resp.StatusCode)
	}
	return decodePeople(t, resp).People
}

// TestAttachPerson_guards verifies the two endpoints answer the codes the API
// promises: 403 without write access, 404 for a photo or subject that does not
// exist, 400 for a body naming nobody.
func TestAttachPerson_guards(t *testing.T) {
	env := newEnv(t)
	editor, _ := env.login(t, "editor", auth.RoleEditor)
	viewer, _ := env.login(t, "viewer", auth.RoleViewer)
	photo := env.seedPhoto(t, photos.Photo{Title: "Still"}, "still.jpg", 40, 50, 60)
	subjectUID := env.seedSubject(t, "Bohumil")
	body, _ := json.Marshal(map[string]string{"subject_uid": subjectUID})

	cases := []struct {
		name   string
		client *http.Client
		method string
		url    string
		body   []byte
		want   int
	}{
		{
			name: "viewer may not attach", client: viewer, method: http.MethodPost,
			url:  env.server.URL + "/api/v1/photos/" + photo.UID + "/people",
			body: body, want: http.StatusForbidden,
		},
		{
			name: "viewer may not detach", client: viewer, method: http.MethodDelete,
			url:  env.server.URL + "/api/v1/photos/" + photo.UID + "/people/" + subjectUID,
			want: http.StatusForbidden,
		},
		{
			name: "unknown photo", client: editor, method: http.MethodPost,
			url:  env.server.URL + "/api/v1/photos/ph_nope/people",
			body: body, want: http.StatusNotFound,
		},
		{
			name: "unknown subject", client: editor, method: http.MethodPost,
			url:  env.server.URL + "/api/v1/photos/" + photo.UID + "/people",
			body: []byte(`{"subject_uid":"su_nope"}`), want: http.StatusNotFound,
		},
		{
			name: "no subject named", client: editor, method: http.MethodPost,
			url:  env.server.URL + "/api/v1/photos/" + photo.UID + "/people",
			body: []byte(`{}`), want: http.StatusBadRequest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := mustDo(t, tc.client, tc.method, tc.url, tc.body)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
}

// TestAttachPerson_schedulesSidecar verifies attaching queues the metadata
// sidecar rewrite, which is what makes the link survive losing the database.
func TestAttachPerson_schedulesSidecar(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "editor", auth.RoleEditor)
	photo := env.seedPhoto(t, photos.Photo{Title: "Still"}, "still.jpg", 70, 80, 90)
	subjectUID := env.seedSubject(t, "Anežka")

	body, _ := json.Marshal(map[string]string{"subject_uid": subjectUID})
	resp := mustDo(t, client, http.MethodPost,
		env.server.URL+"/api/v1/photos/"+photo.UID+"/people", body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("attach status = %d, want 200", resp.StatusCode)
	}

	var queued int
	if err := env.db.Pool().QueryRow(t.Context(),
		`SELECT count(*) FROM jobs WHERE type = 'sidecar' AND payload->>'photo_uid' = $1`,
		photo.UID).Scan(&queued); err != nil {
		t.Fatalf("counting sidecar jobs: %v", err)
	}
	if queued == 0 {
		t.Error("attaching a person scheduled no sidecar rewrite")
	}
}
