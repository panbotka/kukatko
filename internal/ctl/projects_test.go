package ctl

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// projectsBody is a realistic GET /projects answer: the bootstrap project in
// the storage root without limits, and a created one with both limits.
const projectsBody = `[
	{"uid":"pj0000000000000000veselice","slug":"v","name":"Veselice","description":"","active":true,
	 "storage_prefix":"","max_file_bytes":null,"max_total_bytes":null,"members_count":12,
	 "photos_count":4000,"originals_bytes":5368709120,"created_at":"2026-10-11T10:00:00Z"},
	{"uid":"pjhasici","slug":"hasici","name":"Hasiči","description":"Sbor","active":false,
	 "storage_prefix":"projects/pjhasici/","max_file_bytes":1048576,"max_total_bytes":10737418240,
	 "members_count":1,"photos_count":0,"originals_bytes":0,"created_at":"2026-10-12T10:00:00Z"}
]`

// accountsBody is a realistic GET /users answer.
const accountsBody = `[
	{"uid":"us1","username":"anna","display_name":"Anna","email":"anna@example.test",
	 "instance_role":"maintainer","disabled":false,
	 "memberships":[{"project_slug":"hasici","role":"admin","approved":true},
	                {"project_slug":"v","role":"viewer","approved":false}]},
	{"uid":"us2","username":"bob","display_name":"","email":"bob@example.test",
	 "instance_role":"user","disabled":true,"memberships":[]}
]`

// TestWriteProjects verifies the table shows counts, sizes and limits, with
// "unlimited" for a missing limit.
func TestWriteProjects(t *testing.T) {
	t.Parallel()
	list, err := DecodeProjects([]byte(projectsBody))
	if err != nil || len(list) != 2 {
		t.Fatalf("DecodeProjects = %+v, %v", list, err)
	}
	var buf bytes.Buffer
	if err := WriteProjects(&buf, list); err != nil {
		t.Fatalf("WriteProjects: %v", err)
	}
	for _, want := range []string{"SLUG", "QUOTA", "Veselice", "unlimited", "5.0 GiB", "1.0 MiB", "10.0 GiB",
		"inactive", "4000"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("project table does not contain %q:\n%s", want, buf.String())
		}
	}
	buf.Reset()
	if err := WriteProjects(&buf, nil); err != nil || !strings.Contains(buf.String(), "no projects") {
		t.Errorf("empty project table = %q, %v", buf.String(), err)
	}
}

// TestWriteProject verifies the detail names the storage root for an empty
// prefix.
func TestWriteProject(t *testing.T) {
	t.Parallel()
	list, _ := DecodeProjects([]byte(projectsBody))
	var buf bytes.Buffer
	if err := WriteProject(&buf, list[0]); err != nil {
		t.Fatalf("WriteProject: %v", err)
	}
	for _, want := range []string{"STORAGE PREFIX", "(storage root)", "MEMBERS", "12", "2026-10-11"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("project detail does not contain %q:\n%s", want, buf.String())
		}
	}
}

// TestWriteAccounts verifies the account table shows the instance role, the
// disabled flag and the memberships, a pending one marked.
func TestWriteAccounts(t *testing.T) {
	t.Parallel()
	list, err := DecodeAccounts([]byte(accountsBody))
	if err != nil || len(list) != 2 {
		t.Fatalf("DecodeAccounts = %+v, %v", list, err)
	}
	var buf bytes.Buffer
	if err := WriteAccounts(&buf, list); err != nil {
		t.Fatalf("WriteAccounts: %v", err)
	}
	for _, want := range []string{"INSTANCE ROLE", "hasici (admin), v (viewer, pending)", "user, disabled", "anna"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("account table does not contain %q:\n%s", want, buf.String())
		}
	}
	buf.Reset()
	if err := WriteAccount(&buf, list[1]); err != nil || !strings.Contains(buf.String(), "PROJECTS") {
		t.Errorf("account detail = %q, %v", buf.String(), err)
	}
}

// TestProjectPatch_body verifies only the set keys are sent, a cleared limit
// as null, and that an empty or contradictory patch is refused.
func TestProjectPatch_body(t *testing.T) {
	t.Parallel()
	name, limit, active := "N", int64(5), false
	body, err := ProjectPatch{Name: &name, MaxTotalBytes: &limit, ClearMaxFileBytes: true, Active: &active}.body()
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	encoded, _ := json.Marshal(body)
	if string(encoded) != `{"active":false,"max_file_bytes":null,"max_total_bytes":5,"name":"N"}` {
		t.Errorf("body = %s", encoded)
	}
	if _, err := (ProjectPatch{}).body(); !errors.Is(err, ErrEmptyProjectPatch) {
		t.Errorf("empty patch error = %v, want ErrEmptyProjectPatch", err)
	}
	if _, err := (ProjectPatch{MaxFileBytes: &limit, ClearMaxFileBytes: true}).body(); !errors.Is(
		err, ErrConflictingLimit) {
		t.Errorf("contradictory patch error = %v, want ErrConflictingLimit", err)
	}
}

// TestClient_projectRoutes verifies each call's method, path and body.
func TestClient_projectRoutes(t *testing.T) {
	t.Parallel()
	var method, path, body string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r.Body)
		body = buf.String()
		_, _ = w.Write([]byte(`{}`))
	})
	ctx := t.Context()
	steps := []struct {
		call                   func() error
		wantMethod, wantPath   string
		wantBodyContainsSubstr string
	}{
		{func() error { _, err := client.GetProject(ctx, "pj1"); return err }, "GET", "/api/v1/projects/pj1", ""},
		{func() error {
			_, err := client.CreateProject(ctx, ProjectInput{Slug: "s", Name: "N"})
			return err
		}, "POST", "/api/v1/projects", `"slug":"s"`},
		{func() error {
			on := true
			_, err := client.UpdateProject(ctx, "pj1", ProjectPatch{Active: &on})
			return err
		}, "PATCH", "/api/v1/projects/pj1", `"active":true`},
		{func() error { _, err := client.AddProjectMember(ctx, "pj1", "us1", "editor"); return err },
			"POST", "/api/v1/projects/pj1/members", `"role":"editor"`},
		{func() error { _, err := client.SetInstanceRole(ctx, "us1", "maintainer"); return err },
			"PATCH", "/api/v1/users/us1", `"instance_role":"maintainer"`},
	}
	for _, step := range steps {
		if err := step.call(); err != nil {
			t.Fatalf("%s %s: %v", step.wantMethod, step.wantPath, err)
		}
		if method != step.wantMethod || path != step.wantPath || !strings.Contains(body, step.wantBodyContainsSubstr) {
			t.Errorf("request = %s %s %s, want %s %s with %s", method, path, body,
				step.wantMethod, step.wantPath, step.wantBodyContainsSubstr)
		}
	}
	if _, err := client.AddProjectMember(ctx, "pj1", "", "viewer"); !errors.Is(err, ErrNoUser) {
		t.Errorf("AddProjectMember without a user = %v, want ErrNoUser", err)
	}
}

// TestClient_resolve verifies a project is found by slug or uid and an account
// by username or uid, and a miss is a typed error.
func TestClient_resolve(t *testing.T) {
	t.Parallel()
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/users" {
			_, _ = w.Write([]byte(accountsBody))
			return
		}
		_, _ = w.Write([]byte(projectsBody))
	})
	ctx := t.Context()
	if p, err := client.ResolveProject(ctx, "hasici"); err != nil || p.UID != "pjhasici" {
		t.Errorf("ResolveProject(hasici) = %+v, %v", p, err)
	}
	if p, err := client.ResolveProject(ctx, "pjhasici"); err != nil || p.Slug != "hasici" {
		t.Errorf("ResolveProject(uid) = %+v, %v", p, err)
	}
	if _, err := client.ResolveProject(ctx, "nope"); !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("ResolveProject(nope) = %v, want ErrProjectNotFound", err)
	}
	if a, err := client.ResolveAccount(ctx, "bob"); err != nil || a.UID != "us2" {
		t.Errorf("ResolveAccount(bob) = %+v, %v", a, err)
	}
	if _, err := client.ResolveAccount(ctx, "carl"); !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("ResolveAccount(carl) = %v, want ErrAccountNotFound", err)
	}
}

// TestErrorMessage_withMessage verifies a refusal's message follows its code.
func TestErrorMessage_withMessage(t *testing.T) {
	t.Parallel()
	got := errorMessage([]byte(`{"error":"invalid_slug","message":"\"A\" must be lowercase"}`))
	if got != `invalid_slug: "A" must be lowercase` {
		t.Errorf("errorMessage = %q", got)
	}
	if got := errorMessage([]byte(`{"error":"slug_taken"}`)); got != "slug_taken" {
		t.Errorf("errorMessage without a message = %q", got)
	}
}

// TestClient_rekeyRoutes verifies each storage-move call's method and path, and
// that a call naming no project costs no round trip.
func TestClient_rekeyRoutes(t *testing.T) {
	t.Parallel()
	var method, path string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_, _ = w.Write([]byte(`{}`))
	})
	ctx := t.Context()
	steps := []struct {
		call                 func() error
		wantMethod, wantPath string
	}{
		{func() error { _, err := client.RekeyStatus(ctx, "pj1"); return err }, "GET", "/api/v1/projects/pj1/rekey"},
		{func() error { _, err := client.StartRekey(ctx, "pj1"); return err }, "POST", "/api/v1/projects/pj1/rekey"},
		{func() error { _, err := client.ChangeRekey(ctx, "pj1", RekeyPause); return err },
			"POST", "/api/v1/projects/pj1/rekey/pause"},
		{func() error { _, err := client.ChangeRekey(ctx, "pj1", RekeyResume); return err },
			"POST", "/api/v1/projects/pj1/rekey/resume"},
		{func() error { _, err := client.ChangeRekey(ctx, "pj1", RekeyRetry); return err },
			"POST", "/api/v1/projects/pj1/rekey/retry"},
	}
	for _, step := range steps {
		if err := step.call(); err != nil {
			t.Fatalf("%s %s: %v", step.wantMethod, step.wantPath, err)
		}
		if method != step.wantMethod || path != step.wantPath {
			t.Errorf("request = %s %s, want %s %s", method, path, step.wantMethod, step.wantPath)
		}
	}
	if _, err := client.StartRekey(ctx, ""); err == nil {
		t.Error("StartRekey without a project succeeded, want an error")
	}
}

// TestWriteRekeyRun verifies the run's table shows progress, sizes, the finish
// and only the most recent errors.
func TestWriteRekeyRun(t *testing.T) {
	t.Parallel()
	errorsJSON := make([]string, 0, 7)
	for i := range 7 {
		errorsJSON = append(errorsJSON, `{"key":"k`+string(rune('a'+i))+`","message":"m","at":"2026-10-13T10:00:00Z"}`)
	}
	run, err := DecodeRekeyRun([]byte(`{"uid":"rk1","state":"done","target_prefix":"projects/pj1/",
		"total_photos":4,"total_bytes":2048,"done_photos":4,"done_bytes":2048,"error_count":7,
		"errors":[` + strings.Join(errorsJSON, ",") + `],
		"started_at":"2026-10-13T09:00:00Z","updated_at":"2026-10-13T10:00:00Z","finished_at":"2026-10-13T10:00:00Z"}`))
	if err != nil {
		t.Fatalf("DecodeRekeyRun: %v", err)
	}
	var buf bytes.Buffer
	if err := WriteRekeyRun(&buf, run); err != nil {
		t.Fatalf("WriteRekeyRun: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"rk1", "done", "4 of 4 (100.0%)", "2.0 KiB of 2.0 KiB", "kg: m", "kc: m"} {
		if !strings.Contains(out, want) {
			t.Errorf("run table lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "kb: m") || strings.Contains(out, "FINISHED  -") {
		t.Errorf("run table lists an old error or no finish:\n%s", out)
	}
	if got := rekeyPercent(0, 0); got != "100%" {
		t.Errorf("rekeyPercent(0, 0) = %q, want 100%%", got)
	}
}
