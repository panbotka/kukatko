package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// projectsListBody is a two-project GET /projects answer.
const projectsListBody = `[{"uid":"pj0000000000000000veselice","slug":"v","name":"Veselice","active":true,` +
	`"storage_prefix":"","max_file_bytes":null,"max_total_bytes":null,"members_count":3,"photos_count":10,` +
	`"originals_bytes":2048,"created_at":"2026-10-11T10:00:00Z"},` +
	`{"uid":"pjclub","slug":"club","name":"Club","active":true,"storage_prefix":"projects/pjclub/",` +
	`"max_file_bytes":1024,"max_total_bytes":null,"members_count":1,"photos_count":0,"originals_bytes":0,` +
	`"created_at":"2026-10-12T10:00:00Z"}]`

// accountsListBody is a two-account GET /users answer.
const accountsListBody = `[{"uid":"us1","username":"anna","display_name":"Anna","email":"a@example.test",` +
	`"instance_role":"maintainer","disabled":false,"memberships":[{"project_slug":"club","role":"admin",` +
	`"approved":true}]},{"uid":"us2","username":"bob","display_name":"Bob","email":"b@example.test",` +
	`"instance_role":"user","disabled":false,"memberships":[]}]`

// projectRequest is one request the fake system-administrator server saw.
type projectRequest struct {
	method, path, body string
}

// projectServer starts a fake of the /projects and /users routes, recording
// every request, and returns the ctl config path and the recorded requests.
func projectServer(t *testing.T) (string, func() []projectRequest) {
	t.Helper()
	var (
		mu   sync.Mutex
		seen []projectRequest
	)
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, projectRequest{r.Method, r.URL.Path, string(body)})
		mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects":
			_, _ = w.Write([]byte(projectsListBody))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/users":
			_, _ = w.Write([]byte(accountsListBody))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects" && strings.Contains(string(body), `"taken"`):
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"slug_taken"}`))
		case strings.HasPrefix(r.URL.Path, "/api/v1/users/"):
			_, _ = w.Write([]byte(`{"uid":"us2","username":"bob","instance_role":"maintainer","memberships":[]}`))
		case strings.HasSuffix(r.URL.Path, "/members"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"project_uid":"pjclub","user_uid":"us2","role":"editor"}`))
		default:
			_, _ = w.Write([]byte(`{"uid":"pjclub","slug":"club","name":"Club","active":false,` +
				`"storage_prefix":"projects/pjclub/","members_count":1,"created_at":"2026-10-12T10:00:00Z"}`))
		}
	})
	return configPath, func() []projectRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]projectRequest(nil), seen...)
	}
}

// lastRequest returns the last recorded request.
func lastRequest(t *testing.T, seen func() []projectRequest) projectRequest {
	t.Helper()
	all := seen()
	if len(all) == 0 {
		t.Fatal("the server saw no request")
	}
	return all[len(all)-1]
}

// TestCtlProjects_listAndGet verifies the list table, -o json passthrough and
// the slug resolved to a uid for get.
func TestCtlProjects_listAndGet(t *testing.T) {
	configPath, seen := projectServer(t)

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "projects", "list")
	if err != nil {
		t.Fatalf("projects list: %v", err)
	}
	for _, want := range []string{"SLUG", "Veselice", "club", "unlimited", "1.0 KiB", "2.0 KiB"} {
		if !strings.Contains(out, want) {
			t.Errorf("project table does not contain %q:\n%s", want, out)
		}
	}
	out, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "-o", "json", "projects", "list")
	if err != nil || out != projectsListBody+"\n" {
		t.Errorf("projects list -o json = %q, %v; want the server's bytes", out, err)
	}

	out, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "projects", "get", "club")
	if err != nil {
		t.Fatalf("projects get: %v", err)
	}
	if got := lastRequest(t, seen); got.method != http.MethodGet || got.path != "/api/v1/projects/pjclub" {
		t.Errorf("get request = %+v, want GET of the resolved uid", got)
	}
	if !strings.Contains(out, "STORAGE PREFIX") || !strings.Contains(out, "projects/pjclub/") {
		t.Errorf("project detail:\n%s", out)
	}
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "projects", "get", "nope"); err == nil ||
		!strings.Contains(err.Error(), "no such project") {
		t.Errorf("projects get nope = %v, want a no-such-project error", err)
	}
}

// TestCtlProjects_create verifies the flags become the body, an unset limit is
// left out, and a refusal reports the server's code.
func TestCtlProjects_create(t *testing.T) {
	configPath, seen := projectServer(t)

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "projects", "create",
		"--slug", "club", "--name", "Club", "--max-file-bytes", "1024")
	if err != nil {
		t.Fatalf("projects create: %v", err)
	}
	got := lastRequest(t, seen)
	var body map[string]any
	if err := json.Unmarshal([]byte(got.body), &body); err != nil {
		t.Fatalf("decoding the create body %q: %v", got.body, err)
	}
	if got.method != http.MethodPost || got.path != "/api/v1/projects" || body["slug"] != "club" ||
		body["max_file_bytes"] != float64(1024) {
		t.Errorf("create request = %+v", got)
	}
	if _, ok := body["max_total_bytes"]; ok {
		t.Errorf("create body %s carries an unset limit", got.body)
	}

	_, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "projects", "create", "--slug", "taken", "--name", "T")
	if err == nil || !strings.Contains(err.Error(), "slug_taken") {
		t.Errorf("create of a taken slug = %v, want slug_taken", err)
	}
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "projects", "create", "--name", "T"); err == nil {
		t.Error("create without --slug succeeded")
	}
}

// TestCtlProjects_updateActivateAddMember verifies update sends only the
// changed fields (a cleared limit as null), activate/deactivate flip active,
// and add-member resolves both names.
func TestCtlProjects_updateActivateAddMember(t *testing.T) {
	configPath, seen := projectServer(t)

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "projects", "update", "club",
		"--name", "Klub", "--clear-max-file-bytes")
	if err != nil {
		t.Fatalf("projects update: %v", err)
	}
	if got := lastRequest(t, seen); got.method != http.MethodPatch || got.path != "/api/v1/projects/pjclub" ||
		got.body != `{"max_file_bytes":null,"name":"Klub"}` {
		t.Errorf("update request = %+v", got)
	}
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "projects", "update", "club"); err == nil ||
		!strings.Contains(err.Error(), "nothing to change") {
		t.Errorf("an empty update = %v, want nothing to change", err)
	}

	for cmd, want := range map[string]string{"deactivate": `{"active":false}`, "activate": `{"active":true}`} {
		out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "projects", cmd, "club")
		if err != nil {
			t.Fatalf("projects %s: %v", cmd, err)
		}
		if got := lastRequest(t, seen); got.body != want || !strings.Contains(out, "club") {
			t.Errorf("%s request = %+v, output %q", cmd, got, out)
		}
	}

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "projects", "add-member", "club", "bob",
		"--role", "editor")
	if err != nil {
		t.Fatalf("projects add-member: %v", err)
	}
	got := lastRequest(t, seen)
	if got.path != "/api/v1/projects/pjclub/members" || got.body != `{"role":"editor","user_uid":"us2"}` ||
		!strings.Contains(out, "added bob to club as editor") {
		t.Errorf("add-member request = %+v, output %q", got, out)
	}
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "projects", "add-member", "club", "bob"); err == nil {
		t.Error("add-member without --role succeeded")
	}
}

// TestCtlUsers verifies the account list and that promote/demote send the
// instance role for the resolved uid.
func TestCtlUsers(t *testing.T) {
	configPath, seen := projectServer(t)

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "users", "list")
	if err != nil {
		t.Fatalf("users list: %v", err)
	}
	for _, want := range []string{"USERNAME", "anna", "maintainer", "club (admin)"} {
		if !strings.Contains(out, want) {
			t.Errorf("account table does not contain %q:\n%s", want, out)
		}
	}
	for cmd, role := range map[string]string{"promote": "maintainer", "demote": "user"} {
		if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "users", cmd, "bob"); err != nil {
			t.Fatalf("users %s: %v", cmd, err)
		}
		if got := lastRequest(t, seen); got.method != http.MethodPatch || got.path != "/api/v1/users/us2" ||
			got.body != `{"instance_role":"`+role+`"}` {
			t.Errorf("%s request = %+v", cmd, got)
		}
	}
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "users", "promote", "carl"); err == nil ||
		!strings.Contains(err.Error(), "no such account") {
		t.Errorf("promote of an unknown account = %v", err)
	}
}
