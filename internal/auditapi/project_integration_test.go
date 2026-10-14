//go:build integration

package auditapi_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/project"
)

// projectFixture is two projects — the bootstrap `v` and a created `b` — with
// one entry filed under each, one instance entry under neither, an admin of
// each project and a maintainer.
type projectFixture struct {
	env        *env
	b          *project.Project
	adminV     *http.Client
	adminB     *http.Client
	maintainer *http.Client
}

// newProjectFixture seeds the two projects, their admins and the three entries:
// photo.update on ph-v (in v), photo.update on ph-b (in b) and user.create (no
// project).
func newProjectFixture(t *testing.T) projectFixture {
	t.Helper()
	env := newEnv(t)
	b, err := env.projects.Create(t.Context(), audit.Meta{}, project.CreateParams{Slug: "b", Name: "Projekt B"})
	if err != nil {
		t.Fatalf("creating project b: %v", err)
	}
	adminV := env.login(t, "admin-v", auth.AccountAdmin)
	adminB, adminBUID := env.loginUser(t, "admin-b", auth.AccountViewer)
	// admin-b is a viewer of v (the account role's project) and the admin of b.
	if _, err := env.projects.AddMember(t.Context(), audit.Meta{}, b.UID, adminBUID, project.RoleAdmin, true); err != nil {
		t.Fatalf("adding admin-b to b: %v", err)
	}
	env.seed(t,
		audit.Entry{Action: audit.ActionPhotoUpdate, TargetType: "photos", TargetUID: "ph-v",
			ProjectUID: project.BootstrapUID},
		audit.Entry{Action: audit.ActionPhotoUpdate, TargetType: "photos", TargetUID: "ph-b", ProjectUID: b.UID},
		audit.Entry{Action: audit.ActionUserCreate, TargetType: "users", TargetUID: "us-x"},
	)
	return projectFixture{
		env: env, b: b, adminV: adminV, adminB: adminB,
		maintainer: env.login(t, "boss", auth.AccountMaintainer),
	}
}

// targets returns the target uids of a listing, in order.
func targets(resp listResponse) []string {
	out := make([]string, 0, len(resp.Entries))
	for _, e := range resp.Entries {
		if e.TargetUID != nil {
			out = append(out, *e.TargetUID)
		}
	}
	return out
}

// TestProjectListing_isolation proves a project admin's listing holds only that
// project's entries — never another project's, never the instance's — and that
// nothing in the query widens it.
func TestProjectListing_isolation(t *testing.T) {
	f := newProjectFixture(t)
	base := f.env.baseURL + "/api/v1"

	for name, tc := range map[string]struct {
		client  *http.Client
		path    string
		want    string
		foreign string
		project string
	}{
		"admin of v": {client: f.adminV, path: "/p/v/audit", want: "ph-v", foreign: "ph-b", project: "v"},
		"admin of b": {client: f.adminB, path: "/p/b/audit", want: "ph-b", foreign: "ph-v", project: "b"},
	} {
		got := list(t, tc.client, base+tc.path)
		seen := targets(got)
		if !slices.Contains(seen, tc.want) || slices.Contains(seen, tc.foreign) || slices.Contains(seen, "us-x") {
			t.Errorf("%s: listing targets = %v, want %s and neither %s nor the instance entry",
				name, seen, tc.want, tc.foreign)
		}
		for _, e := range got.Entries {
			if e.Project == nil || *e.Project != tc.project {
				t.Errorf("%s: entry %d project = %v, want %q", name, e.ID, e.Project, tc.project)
			}
		}
	}

	// The admin of v is no member of b: the project is a 404, like an unknown one.
	if resp := do(t, f.adminV, http.MethodGet, base+"/p/b/audit", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("admin of v reading b = %d, want 404", resp.StatusCode)
	} else {
		_ = resp.Body.Close()
	}
	// A project route cannot be widened by a project parameter.
	badRequest(t, f.adminV, base+"/p/v/audit?project=b", "project")
	// A viewer of v is no admin there.
	if resp := do(t, f.adminB, http.MethodGet, base+"/p/v/audit", nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer of v reading v = %d, want 403", resp.StatusCode)
	} else {
		_ = resp.Body.Close()
	}
}

// TestInstanceListing_projectFilter proves the maintainer's ?project=<slug>
// narrows the whole trail to that project, every record names its project, and
// an unknown slug is refused rather than answered with an empty page.
func TestInstanceListing_projectFilter(t *testing.T) {
	f := newProjectFixture(t)
	base := f.env.baseURL + "/api/v1/audit"

	all := targets(list(t, f.maintainer, base))
	for _, want := range []string{"ph-v", "ph-b", "us-x"} {
		if !slices.Contains(all, want) {
			t.Errorf("unfiltered listing %v misses %s", all, want)
		}
	}
	onlyB := list(t, f.maintainer, base+"?project=b")
	if seen := targets(onlyB); !slices.Contains(seen, "ph-b") || slices.Contains(seen, "ph-v") || slices.Contains(seen, "us-x") {
		t.Errorf("?project=b targets = %v, want b's entries only", seen)
	}
	for _, e := range onlyB.Entries {
		if e.ProjectUID == nil || *e.ProjectUID != f.b.UID || e.Project == nil || *e.Project != "b" {
			t.Errorf("entry %d project = %v/%v, want %s/b", e.ID, e.ProjectUID, e.Project, f.b.UID)
		}
	}
	badRequest(t, f.maintainer, base+"?project=nowhere", "nowhere")
}

// TestProjectMine_narrowedToProject proves a member's own actions under a
// project are only those filed there, while the instance /audit/mine lists
// them in every project.
func TestProjectMine_narrowedToProject(t *testing.T) {
	f := newProjectFixture(t)
	client, uid := f.env.loginUser(t, "eva", auth.AccountEditor)
	if _, err := f.env.projects.AddMember(t.Context(), audit.Meta{}, f.b.UID, uid, project.RoleEditor, true); err != nil {
		t.Fatalf("adding eva to b: %v", err)
	}
	f.env.seed(t,
		audit.Entry{ActorUID: uid, Action: audit.ActionAlbumCreate, TargetType: "albums", TargetUID: "al-v",
			ProjectUID: project.BootstrapUID},
		audit.Entry{ActorUID: uid, Action: audit.ActionAlbumCreate, TargetType: "albums", TargetUID: "al-b",
			ProjectUID: f.b.UID},
	)
	base := f.env.baseURL + "/api/v1"

	if seen := targets(list(t, client, base+"/p/b/audit/mine")); len(seen) != 1 || seen[0] != "al-b" {
		t.Errorf("/p/b/audit/mine targets = %v, want [al-b]", seen)
	}
	if seen := targets(list(t, client, base+"/audit/mine")); !slices.Contains(seen, "al-v") || !slices.Contains(seen, "al-b") {
		t.Errorf("/audit/mine targets = %v, want both projects' entries", seen)
	}
}

// TestMine_noSlugOrUsernameOracle proves the own-activity listing tells a
// member nothing about other projects or accounts: a viewer of v probing either
// mount with another project's slug, a slug nobody has, another project's
// member or a name nobody has gets exactly the answer the bare request gets —
// the same status and the same body, the caller's own entries in scope.
func TestMine_noSlugOrUsernameOracle(t *testing.T) {
	f := newProjectFixture(t)
	viewer, uid := f.env.loginUser(t, "vera", auth.AccountViewer)
	f.env.seed(t, audit.Entry{ActorUID: uid, Action: audit.ActionAlbumCreate, TargetType: "albums",
		TargetUID: "al-vera", ProjectUID: project.BootstrapUID})
	base := f.env.baseURL + "/api/v1"

	probes := []string{"project=b", "project=nowhere", "user=admin-b", "user=nobody-by-that-name"}
	for _, mount := range []string{"/audit/mine", "/p/v/audit/mine"} {
		wantStatus, wantBody := rawGet(t, viewer, base+mount)
		if wantStatus != http.StatusOK {
			t.Fatalf("%s = %d %s, want 200", mount, wantStatus, wantBody)
		}
		for _, probe := range probes {
			status, body := rawGet(t, viewer, base+mount+"?"+probe)
			if status != wantStatus || body != wantBody {
				t.Errorf("%s?%s = %d %s, want the bare answer %d %s", mount, probe, status, body, wantStatus, wantBody)
			}
		}
		if seen := targets(list(t, viewer, base+mount+"?project=b")); len(seen) != 1 || seen[0] != "al-vera" {
			t.Errorf("%s?project=b targets = %v, want only the caller's own [al-vera]", mount, seen)
		}
	}
}

// TestProjectListing_noUsernameOracle proves a project admin's user filter
// resolves only among the project's people: an account of another project and
// an account that does not exist — by username or by uid — get exactly the
// answer a member who did nothing gets (the same 200 and the same body, an
// empty page), while a former member who acted in the project and a current
// member are still found. The maintainer's instance listing keeps its 400.
func TestProjectListing_noUsernameOracle(t *testing.T) {
	f := newProjectFixture(t)
	// vera is an account of v only; ola acted in b once and has since left it.
	_, veraUID := f.env.loginUser(t, "vera", auth.AccountViewer)
	_, olaUID := f.env.loginUser(t, "ola", auth.AccountViewer)
	f.env.seed(t,
		audit.Entry{ActorUID: veraUID, Action: audit.ActionAlbumCreate, TargetType: "albums", TargetUID: "al-vera",
			ProjectUID: project.BootstrapUID},
		audit.Entry{ActorUID: olaUID, Action: audit.ActionAlbumCreate, TargetType: "albums", TargetUID: "al-ola",
			ProjectUID: f.b.UID},
	)
	base := f.env.baseURL + "/api/v1/p/b/audit"

	// admin-b is a member of b with no entries of its own: the reference answer.
	wantStatus, wantBody := rawGet(t, f.adminB, base+"?user=admin-b")
	if wantStatus != http.StatusOK || !strings.Contains(wantBody, `"entries":[]`) {
		t.Fatalf("?user=admin-b = %d %s, want 200 with an empty page", wantStatus, wantBody)
	}
	for _, probe := range []string{"vera", veraUID, "nobody-by-that-name", "usnonexistent000000000000"} {
		status, body := rawGet(t, f.adminB, base+"?user="+probe)
		if status != wantStatus || body != wantBody {
			t.Errorf("?user=%s = %d %s, want the non-acting member's answer %d %s",
				probe, status, body, wantStatus, wantBody)
		}
	}
	for _, ref := range []string{"ola", olaUID} {
		if seen := targets(list(t, f.adminB, base+"?user="+ref)); len(seen) != 1 || seen[0] != "al-ola" {
			t.Errorf("?user=%s targets = %v, want the former member's [al-ola]", ref, seen)
		}
	}
	badRequest(t, f.maintainer, f.env.baseURL+"/api/v1/audit?user=nobody-by-that-name", "nobody-by-that-name")
}
