//go:build integration

package importapi_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit/audittest"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/importapi"
	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// newIsolationServer mounts the real auth API and the import routes under
// /api/v1/p/{project} the way the server does.
func newIsolationServer(t *testing.T, pool *pgxpool.Pool) *httptest.Server {
	t.Helper()
	authSvc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})
	runs := importer.NewStore(pool)
	api := importapi.NewAPI(importapi.Config{
		Runs: runs, Failures: runs, RequireMaintainer: authAPI.RequireMaintainer,
	})
	r := chi.NewRouter()
	r.Route(auth.APIBasePath, func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		r.Route("/p/{"+auth.ProjectURLParam+"}", func(r chi.Router) {
			r.Use(auth.WithProject(project.NewStore(pool)))
			api.RegisterRoutes(r)
		})
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return server
}

// TestImportRuns_neverCrossProjects records a run with a failure in each
// project and lists both through B: B's history and B's failures hold B's run
// only, and the store refuses a run of A asked for in B like a missing one.
func TestImportRuns_neverCrossProjects(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool, projecttest.AsMaintainer(projecttest.SideB))
	store := importer.NewStore(pool)

	runOf := map[projecttest.Side]importer.Run{}
	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		run, err := store.Start(t.Context(), f.Tenant(side).ProjectUID, importer.SourceFolder)
		if err != nil {
			t.Fatalf("starting %s's run: %v", side, err)
		}
		failure := importer.NewFailure(run.ID, importer.SourceFolder, importer.StagePhoto, "",
			"ref-"+side.String(), "detail", nil)
		if err := store.RecordFailures(t.Context(), []importer.Failure{failure}); err != nil {
			t.Fatalf("recording %s's failure: %v", side, err)
		}
		runOf[side] = run
	}

	server := newIsolationServer(t, pool)
	client := f.ClientFor(t, server, projecttest.SideB)
	own, foreign := runOf[projecttest.SideB].ID, runOf[projecttest.SideA].ID

	var runs runsBody
	decodeJSON(t, f.Get(t, client, f.PathFor(projecttest.SideB, "import/runs")), &runs)
	if len(runs.Runs) != 1 || runs.Runs[0].ID != own {
		t.Errorf("B's run history = %+v, want only B's run %d (never A's %d)", runs.Runs, own, foreign)
	}
	var failures failuresBody
	decodeJSON(t, f.Get(t, client, f.PathFor(projecttest.SideB, "import/failures")), &failures)
	if len(failures.Failures) != 1 || failures.Failures[0].RunID != own {
		t.Errorf("B's failures = %+v, want only the failure of B's run %d", failures.Failures, own)
	}
	var filtered failuresBody
	decodeJSON(t, f.Get(t, client, f.PathFor(projecttest.SideB, fmt.Sprintf("import/failures?run_id=%d", foreign))),
		&filtered)
	if len(filtered.Failures) != 0 {
		t.Errorf("B asking for A's run's failures got %+v, want none", filtered.Failures)
	}

	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		list, err := store.List(t.Context(), projectUID, 10, 0)
		ids := make([]string, 0, len(list))
		for _, run := range list {
			ids = append(ids, strconv.FormatInt(run.ID, 10))
		}
		return ids, err
	})
	if _, err := store.Get(t.Context(), f.B.ProjectUID, foreign); !errors.Is(err, importer.ErrRunNotFound) {
		t.Errorf("Get(A's run in B) = %v, want ErrRunNotFound", err)
	}
	if _, err := store.List(t.Context(), "", 10, 0); !errors.Is(err, importer.ErrNoProject) {
		t.Errorf("List without a project = %v, want ErrNoProject", err)
	}
}

// decodeJSON decodes a projecttest response's 200 body into out.
func decodeJSON(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decoding the body: %v", err)
	}
}
