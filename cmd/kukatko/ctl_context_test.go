package main

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/panbotka/kukatko/internal/ctl"
)

// pathRecorder starts a fake server answering every request with an empty
// photo page and returns the config path (context working in ctlTestProject)
// and an accessor for the paths it saw.
func pathRecorder(t *testing.T, body string) (string, func() []string) {
	t.Helper()
	var (
		mu   sync.Mutex
		seen []string
	)
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Path)
		mu.Unlock()
		_, _ = w.Write([]byte(body))
	})
	return configPath, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

// emptyPage is an empty /photos envelope.
const emptyPage = `{"photos":[],"total":0,"limit":100,"offset":0,"next_offset":null}`

// TestCtl_projectPrecedence pins where a project-scoped command goes: the
// context's project, KUKATKO_PROJECT over it, and the persistent --project over
// both — given before or after the subcommand.
func TestCtl_projectPrecedence(t *testing.T) {
	configPath, seen := pathRecorder(t, emptyPage)

	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "list"); err != nil {
		t.Fatalf("photos list: %v", err)
	}
	t.Setenv(ctl.EnvProject, "klub")
	cmd := newRootCmd("kukatko")
	cmd.SetArgs([]string{"ctl", "--ctl-config", configPath, "photos", "list"})
	cmd.SetOut(&strings.Builder{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("photos list with %s: %v", ctl.EnvProject, err)
	}
	for _, args := range [][]string{
		{"ctl", "--ctl-config", configPath, "--project", "skola", "photos", "list"},
		{"ctl", "--ctl-config", configPath, "photos", "list", "--project", "skola"},
	} {
		cmd := newRootCmd("kukatko")
		cmd.SetArgs(args)
		cmd.SetOut(&strings.Builder{})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	want := []string{
		"/api/v1/p/fam/photos", "/api/v1/p/klub/photos", "/api/v1/p/skola/photos", "/api/v1/p/skola/photos",
	}
	if got := seen(); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("paths = %v, want %v", got, want)
	}
}

// TestCtl_noProjectFailsBeforeAnyRequest verifies a project-scoped command
// without a project fails with exactly ctl.ErrNoProject's text and contacts
// nothing, while an instance-level command runs without one.
func TestCtl_noProjectFailsBeforeAnyRequest(t *testing.T) {
	configPath, seen := pathRecorder(t, `{"user":{"uid":"us1"},"instance_role":"user","projects":[]}`)
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "context", "set", "--project", ""); err != nil {
		t.Fatalf("clearing the project: %v", err)
	}

	for _, args := range [][]string{
		{"photos", "list"}, {"albums", "list"}, {"members", "list"}, {"tasks", "list"},
	} {
		_, err := runCtl(t, "", append([]string{"ctl", "--ctl-config", configPath}, args...)...)
		if err == nil || err.Error() != "no project selected: run 'kukatko ctl projects use <slug>' or pass --project" {
			t.Errorf("%v without a project = %v, want exactly the no-project message", args, err)
		}
	}
	if got := seen(); len(got) != 0 {
		t.Fatalf("a project-less scoped command contacted the server: %v", got)
	}

	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "projects", "list"); err != nil {
		t.Fatalf("projects list without a project: %v", err)
	}
	if got := seen(); len(got) != 1 || got[0] != "/api/v1/auth/me" {
		t.Errorf("projects list asked %v, want only /api/v1/auth/me", got)
	}
}

// TestCtl_instanceCommandsIgnoreTheProject verifies the instance-level trees
// talk to the bare /api/v1 even with a project selected.
func TestCtl_instanceCommandsIgnoreTheProject(t *testing.T) {
	configPath, seen := projectServer(t)

	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "--project", "club", "users", "list"); err != nil {
		t.Fatalf("users list: %v", err)
	}
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "projects", "get", "club"); err != nil {
		t.Fatalf("projects get: %v", err)
	}
	for _, r := range seen() {
		if strings.HasPrefix(r.path, "/api/v1/p/") {
			t.Errorf("instance command sent %s %s under a project prefix", r.method, r.path)
		}
	}
}

// TestCtlProjects_listMemberships verifies a non-maintainer's project list is
// its memberships from /auth/me — the full list would refuse its token — in a
// table, and the server's own array for -o json.
func TestCtlProjects_listMemberships(t *testing.T) {
	const memberships = `[{"uid":"pjfam","slug":"fam","name":"Rodina","active":true,"role":"editor",` +
		`"approved":true},{"uid":"pjclub","slug":"club","name":"Club","active":true,"role":"viewer","approved":false}]`
	configPath, seen := pathRecorder(t, `{"user":{"uid":"us2"},"instance_role":"user","projects":`+memberships+`}`)

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "projects", "list")
	if err != nil {
		t.Fatalf("projects list: %v", err)
	}
	for _, want := range []string{"SLUG", "ROLE", "fam", "Rodina", "editor", "club", "pending"} {
		if !strings.Contains(out, want) {
			t.Errorf("membership table does not contain %q:\n%s", want, out)
		}
	}
	out, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "-o", "json", "projects", "list")
	if err != nil || out != memberships+"\n" {
		t.Errorf("projects list -o json = %q, %v; want the memberships array", out, err)
	}
	for _, path := range seen() {
		if path != "/api/v1/auth/me" {
			t.Errorf("a non-maintainer's list asked %s", path)
		}
	}
}

// TestCtlProjects_use verifies "projects use" stores the slug on the current
// context (or the --context one) without contacting the server, refuses a
// malformed slug, and that "context current" reports it.
func TestCtlProjects_use(t *testing.T) {
	configPath, seen := pathRecorder(t, emptyPage)

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "projects", "use", "klub")
	if err != nil {
		t.Fatalf("projects use: %v", err)
	}
	if !strings.Contains(out, `context "test" now works in project "klub"`) {
		t.Errorf("projects use output = %q", out)
	}
	cfg, err := ctl.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := cfg.Find("test"); got.Project != "klub" || got.Token == "" {
		t.Errorf("stored context = %+v, want project klub and the token kept", got)
	}
	out, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "context", "current")
	if err != nil || !strings.Contains(out, "klub") || !strings.Contains(out, "test") ||
		strings.Contains(out, "supersecret") {
		t.Errorf("context current = %q, %v; want the context and project, never the token", out, err)
	}
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "projects", "use", "Bad Slug"); err == nil {
		t.Error("projects use accepted a malformed slug")
	}
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "--context", "ghost", "projects", "use", "x"); !errors.Is(
		err, ctl.ErrContextNotFound) {
		t.Errorf("projects use on a missing context = %v, want ErrContextNotFound", err)
	}
	if got := seen(); len(got) != 0 {
		t.Errorf("projects use / context current contacted the server: %v", got)
	}

	empty := filepath.Join(t.TempDir(), "ctl.yaml")
	if _, err := runCtl(t, "", "ctl", "--ctl-config", empty, "projects", "use", "klub"); !errors.Is(
		err, ctl.ErrNoCurrentContext) {
		t.Errorf("projects use without any context = %v, want ErrNoCurrentContext", err)
	}
}

// TestCtlContext_setProject verifies "context set" stores, keeps and clears the
// project through the persistent --project flag, defaulting to the current
// context when no name is given, and that "config set-context" still works.
func TestCtlContext_setProject(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "ctl.yaml")

	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "context", "set", "prod",
		"--server", "https://kukatko.example.com", "--project", "rodina"); err != nil {
		t.Fatalf("context set: %v", err)
	}
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "config", "set-context", "prod",
		"--server", "https://b.example.com"); err != nil {
		t.Fatalf("config set-context: %v", err)
	}
	cfg, err := ctl.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := cfg.Find("prod"); got.Project != "rodina" || got.Server != "https://b.example.com" {
		t.Errorf("after a server change = %+v, want the project kept", got)
	}

	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "context", "set", "--project", ""); err != nil {
		t.Fatalf("context set --project \"\": %v", err)
	}
	if cfg, err = ctl.Load(configPath); err != nil {
		t.Fatal(err)
	}
	if got, _ := cfg.Find("prod"); got.Project != "" {
		t.Errorf("after clearing = %+v, want no project", got)
	}
	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "context", "list")
	if err != nil || !strings.Contains(out, "PROJECT") {
		t.Errorf("context list = %q, %v; want a PROJECT column", out, err)
	}
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "context", "set", "--project", "-x"); err == nil {
		t.Error("context set accepted a malformed project slug")
	}

	empty := filepath.Join(t.TempDir(), "ctl.yaml")
	if _, err := runCtl(t, "", "ctl", "--ctl-config", empty, "context", "set", "--project", "x"); !errors.Is(
		err, ctl.ErrNoCurrentContext) {
		t.Errorf("context set without a name or current context = %v, want ErrNoCurrentContext", err)
	}
}

// TestCtl_outputFlagStillReachesEveryCommand guards the persistent -o against
// being shadowed by a local flag (see the --project move): it must parse on a
// project-scoped, an instance-level and the member command alike.
func TestCtl_outputFlagStillReachesEveryCommand(t *testing.T) {
	configPath, _ := pathRecorder(t, `[]`)
	for _, args := range [][]string{
		{"members", "list", "-o", "json"},
		{"albums", "list", "-o", "json", "--project", "klub"},
		{"users", "list", "-o", "json"},
	} {
		if _, err := runCtl(t, "", append([]string{"ctl", "--ctl-config", configPath}, args...)...); err != nil &&
			strings.Contains(err.Error(), "unknown") {
			t.Errorf("%v: %v", args, err)
		}
	}
}
