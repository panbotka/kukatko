//go:build integration

package projecttest_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case,
// so they do not run in parallel.

// probe is what the guarded probe route answers: the project the request
// entered and the caller's role in it.
type probe struct {
	ProjectUID string `json:"project_uid"`
	Role       string `json:"role"`
}

// newPool returns the pool of a freshly truncated integration database.
func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	return db.Pool()
}

// newServer raises the auth API plus one guarded probe route mounted the way
// the server mounts the project API: under /api/v1/p/{project} behind
// auth.WithProject.
func newServer(t *testing.T, pool *pgxpool.Pool) *httptest.Server {
	t.Helper()
	svc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	api := auth.NewAPI(auth.APIConfig{Service: svc, Limiter: auth.NewLimiter(100, time.Minute)})
	projects := project.NewStore(pool)
	r := chi.NewRouter()
	r.Route(auth.APIBasePath, func(r chi.Router) {
		api.RegisterRoutes(r)
		r.Route("/p/{"+auth.ProjectURLParam+"}", func(r chi.Router) {
			r.Use(auth.WithProject(projects))
			r.With(api.RequireAuth).Get("/probe", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(probe{
					ProjectUID: auth.ProjectUIDFromContext(r.Context()),
					Role:       string(auth.ProjectRoleFromContext(r.Context())),
				})
			})
		})
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return server
}

// TestSeed_twoTenants proves the shape of the fixture: A is the bootstrap
// project, B a created one with its own prefix, every uid is distinct, and
// each member belongs to its own project only.
func TestSeed_twoTenants(t *testing.T) {
	pool := newPool(t)
	f := projecttest.Seed(t, pool)

	if f.A.ProjectUID != project.BootstrapUID || f.A.Slug != project.BootstrapSlug {
		t.Errorf("A = %s/%s, want the bootstrap project", f.A.ProjectUID, f.A.Slug)
	}
	if !strings.HasPrefix(f.B.Slug, "b-") || f.B.StoragePrefix != project.StoragePrefixFor(f.B.ProjectUID) {
		t.Errorf("B = slug %q prefix %q, want b-<random> and projects/<uid>/", f.B.Slug, f.B.StoragePrefix)
	}
	all := append(f.A.UIDs(), f.B.UIDs()...)
	for i, uid := range all {
		if uid == "" || slices.Contains(all[i+1:], uid) {
			t.Errorf("uid %q is empty or not unique", uid)
		}
	}

	projects := project.NewStore(pool)
	for _, tc := range []struct {
		tenant  *projecttest.Tenant
		project string
		member  bool
	}{
		{&f.A, f.A.ProjectUID, true},
		{&f.A, f.B.ProjectUID, false},
		{&f.B, f.B.ProjectUID, true},
		{&f.B, f.A.ProjectUID, false},
	} {
		m, err := projects.Membership(t.Context(), tc.project, tc.tenant.UserUID)
		if tc.member && (err != nil || m.Role != project.RoleAdmin || m.ApprovedAt == nil) {
			t.Errorf("%s in %s = %+v, %v; want an approved admin", tc.tenant.Username, tc.project, m, err)
		}
		if !tc.member && err == nil {
			t.Errorf("%s is a member of %s too", tc.tenant.Username, tc.project)
		}
	}
}

// TestSeed_withRole proves the option sets one side's role and leaves the other
// an admin.
func TestSeed_withRole(t *testing.T) {
	pool := newPool(t)
	f := projecttest.Seed(t, pool, projecttest.WithRole(projecttest.SideB, project.RoleViewer))
	if f.A.Role != project.RoleAdmin || f.B.Role != project.RoleViewer {
		t.Fatalf("roles = %s/%s, want admin/viewer", f.A.Role, f.B.Role)
	}
	m, err := project.NewStore(pool).Membership(t.Context(), f.B.ProjectUID, f.B.UserUID)
	if err != nil || m.Role != project.RoleViewer {
		t.Errorf("B's membership = %+v, %v; want viewer", m, err)
	}
}

// TestSeed_rowsLandInTheirProject runs AssertStoreScoped over every seeded root
// table, read straight with SQL: each side's rows carry its own project_id and
// both sides have one. It is also the worked example of a store-level
// isolation test.
func TestSeed_rowsLandInTheirProject(t *testing.T) {
	pool := newPool(t)
	f := projecttest.Seed(t, pool)
	for _, table := range []string{
		"photos", "subjects", "albums", "labels", "photo_tasks", "saved_searches", "upload_links",
	} {
		t.Run(table, func(t *testing.T) {
			f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
				rows, err := pool.Query(t.Context(), "SELECT uid FROM "+table+" WHERE project_id = $1", projectUID)
				if err != nil {
					return nil, err
				}
				var uids []string
				for rows.Next() {
					var uid string
					if err := rows.Scan(&uid); err != nil {
						return nil, err
					}
					uids = append(uids, uid)
				}
				return uids, rows.Err()
			})
		})
	}
	// The child rows hang off the right parents.
	var linked int
	err := pool.QueryRow(t.Context(), `SELECT count(*) FROM photos p
		JOIN photo_files pf ON pf.photo_uid = p.uid AND pf.is_primary
		JOIN markers m ON m.photo_uid = p.uid AND m.subject_uid = $2
		JOIN album_photos ap ON ap.photo_uid = p.uid AND ap.album_uid = $3
		JOIN photo_labels pl ON pl.photo_uid = p.uid AND pl.label_uid = $4
		JOIN photo_task_photos tp ON tp.photo_uid = p.uid AND tp.task_uid = $5
		WHERE p.uid = $1`, f.B.PhotoUID, f.B.SubjectUID, f.B.AlbumUID, f.B.LabelUID, f.B.TaskUID).Scan(&linked)
	if err != nil || linked != 1 {
		t.Errorf("B's photo links = %d, %v; want file, marker, album, label and task on it", linked, err)
	}
}

// TestClientFor_entersOnlyItsOwnProject is the worked example of an HTTP-level
// isolation test against the real project guard: each member enters its own
// project with its role, the other project is a 404 that names nothing of it,
// and that 404 is the very answer an unknown slug gets.
func TestClientFor_entersOnlyItsOwnProject(t *testing.T) {
	pool := newPool(t)
	f := projecttest.Seed(t, pool)
	server := newServer(t, pool)

	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		client := f.ClientFor(t, server, side)
		resp := f.Get(t, client, f.PathFor(side, "probe"))
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		var got probe
		if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &got) != nil ||
			got.ProjectUID != f.Tenant(side).ProjectUID || got.Role != string(project.RoleAdmin) {
			t.Errorf("%s in its own project = %d %s, want 200 as admin", side, resp.StatusCode, body)
		}

		f.AssertInvisible(t, client, f.PathFor(side.Other(), "probe"))
		foreign := f.Get(t, client, f.PathFor(side.Other(), "probe"))
		missing := f.Get(t, client, auth.APIBasePath+"/p/no-such-project/probe")
		projecttest.AssertSameShape(t, foreign, missing)
		_, _ = foreign.Body.Close(), missing.Body.Close()
	}
}
