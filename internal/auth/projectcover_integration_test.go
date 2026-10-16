//go:build integration

package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/project"
)

// fakeCovers is an auth.ProjectCovers that names a cover for every project it
// is asked about and remembers which ones it was asked about.
type fakeCovers struct {
	mu    sync.Mutex
	asked []string
}

// CoverThumbURL implements auth.ProjectCovers.
func (f *fakeCovers) CoverThumbURL(_ context.Context, projectUID, slug string, role project.Role) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, projectUID)
	return "/api/v1/p/" + slug + "/photos/cover-" + string(role) + "/thumb/tile_500", nil
}

// meCovers is the part of GET /auth/me the cover test reads.
type meCovers struct {
	Projects []struct {
		UID           string     `json:"uid"`
		Slug          string     `json:"slug"`
		CoverThumbURL *string    `json:"cover_thumb_url"`
		LastVisitAt   *time.Time `json:"last_visit_at"`
	} `json:"projects"`
}

// TestMe_projectCovers pins the chooser's pictures on GET /auth/me: a cover is
// asked for and named only for a project the caller may enter (approved, in an
// active project), never for a waiting membership, a deactivated project or a
// project the caller does not belong to; and each membership carries its last
// visit.
func TestMe_projectCovers(t *testing.T) {
	base := newHTTPEnv(t, 50)
	projects := project.NewStore(base.db.Pool())
	covers := &fakeCovers{}
	api := auth.NewAPI(auth.APIConfig{
		Service: base.svc, Limiter: auth.NewLimiter(50, time.Minute), Covers: covers,
	})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterInstanceRoutes)
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	env := &projectEnv{httpEnv: base, projects: projects, server: server}

	alfa := env.createProject(t, "alfa")
	beta := env.createProject(t, "beta")
	gama := env.createProject(t, "gama")
	cizi := env.createProject(t, "cizi")
	client, user := env.signIn(t, "eva", auth.AccountCurator)
	env.addMember(t, alfa, user, project.RoleViewer, true)
	env.addMember(t, beta, user, project.RoleEditor, false)
	env.addMember(t, gama, user, project.RoleAdmin, true)
	visited := time.Date(2026, time.October, 15, 8, 0, 0, 0, time.UTC)
	pool := env.db.Pool()
	if _, err := pool.Exec(t.Context(), "UPDATE projects SET active = false WHERE uid = $1", gama.UID); err != nil {
		t.Fatalf("deactivating gama: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE project_members SET last_seen_at = $3
		WHERE project_uid = $1 AND user_uid = $2`, alfa.UID, user.UID, visited); err != nil {
		t.Fatalf("stamping alfa's visit: %v", err)
	}

	status, body := env.get(t, client, "/api/v1/auth/me")
	if status != http.StatusOK {
		t.Fatalf("/auth/me = %d (body %s)", status, body)
	}
	var me meCovers
	if err := json.Unmarshal(body, &me); err != nil {
		t.Fatalf("decoding %s: %v", body, err)
	}
	got := map[string]string{}
	for _, p := range me.Projects {
		if p.CoverThumbURL == nil {
			t.Fatalf("project %s has no cover_thumb_url key: %s", p.Slug, body)
		}
		got[p.Slug] = *p.CoverThumbURL
		wantVisit := p.UID == alfa.UID
		if (p.LastVisitAt != nil) != wantVisit || (wantVisit && !p.LastVisitAt.Equal(visited)) {
			t.Errorf("%s last_visit_at = %v, want visited %v", p.Slug, p.LastVisitAt, wantVisit)
		}
	}
	want := map[string]string{
		project.BootstrapSlug: "/api/v1/p/v/photos/cover-curator/thumb/tile_500",
		"alfa":                "/api/v1/p/alfa/photos/cover-viewer/thumb/tile_500",
		"beta":                "",
		"gama":                "",
	}
	for slug, url := range want {
		if got[slug] != url {
			t.Errorf("%s cover = %q, want %q", slug, got[slug], url)
		}
	}
	if len(got) != len(want) {
		t.Errorf("listed projects = %v, want exactly %v", got, want)
	}
	covers.mu.Lock()
	defer covers.mu.Unlock()
	asked := slices.Sorted(slices.Values(covers.asked))
	if wantAsked := slices.Sorted(slices.Values([]string{project.BootstrapUID, alfa.UID})); !slices.Equal(asked, wantAsked) {
		t.Errorf("covers asked about %v, want only the enterable %v", asked, wantAsked)
	}
	if slices.Contains(covers.asked, cizi.UID) || slices.Contains(covers.asked, beta.UID) {
		t.Errorf("covers asked about a project the caller may not enter: %v", covers.asked)
	}
}
