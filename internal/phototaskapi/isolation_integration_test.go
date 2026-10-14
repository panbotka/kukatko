//go:build integration

package phototaskapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit/audittest"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/phototask"
	"github.com/panbotka/kukatko/internal/phototaskapi"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// These are the HTTP-level tenant-isolation tests of the work queue: through
// B's prefix, A's task — its detail, its edits, its photos, its participants and
// its thread — is exactly a task that does not exist, and B's listing and badge
// count only B's.

// newIsolationServer mounts the task routes under /api/v1/p/{project} behind
// auth.WithProject over the database seeded with opts, the way the server does.
func newIsolationServer(
	t *testing.T, opts ...projecttest.Option,
) (*httptest.Server, *projecttest.Fixture, *comments.Store) {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool, opts...)

	svc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: svc, Limiter: auth.NewLimiter(100, time.Minute)})
	threads := comments.NewStore(pool)
	api := phototaskapi.NewAPI(phototaskapi.Config{
		Store:        phototask.NewStore(pool),
		Comments:     threads,
		RequireAuth:  authAPI.RequireAuth,
		RequireWrite: authAPI.RequireWrite,
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
	return server, f, threads
}

// TestTasks_foreignTaskIsMissing addresses A's task through B's prefix on every
// task route and asserts each answer is the very 404 a missing task gets, that
// commenting on it writes nothing, and that A's task is unchanged afterwards.
func TestTasks_foreignTaskIsMissing(t *testing.T) {
	server, f, threads := newIsolationServer(t)
	client := f.ClientFor(t, server, projecttest.SideB)
	foreign := "tasks/" + f.A.TaskUID
	missing := "tasks/" + projecttest.MissingUID("tk")

	f.AssertInvisible(t, client, f.PathFor(projecttest.SideB, foreign))
	f.AssertInvisible(t, client, f.PathFor(projecttest.SideB, foreign+"/participants"))
	for _, tc := range []struct {
		method, sub string
		body        any
	}{
		{http.MethodGet, "", nil},
		{http.MethodPatch, "", map[string]string{"title": "Uneseno"}},
		{http.MethodPost, "/photos", map[string][]string{"photo_uids": {f.B.PhotoUID}}},
		{http.MethodDelete, "/photos", map[string][]string{"photo_uids": {f.A.PhotoUID}}},
		{http.MethodGet, "/participants", nil},
		{http.MethodPost, "/participants", map[string]string{"user_uid": f.B.UserUID}},
		{http.MethodPost, "/comments", map[string]string{"body": "Přes hranici"}},
		{http.MethodGet, "/comments", nil},
		{http.MethodDelete, "", nil},
	} {
		foreignResp := f.Send(t, client, tc.method, f.PathFor(projecttest.SideB, foreign+tc.sub), tc.body)
		missingResp := f.Send(t, client, tc.method, f.PathFor(projecttest.SideB, missing+tc.sub), tc.body)
		if tc.method != http.MethodGet || tc.sub != "/comments" {
			if foreignResp.StatusCode != http.StatusNotFound {
				t.Errorf("%s %s of A's task through B = %d, want 404", tc.method, tc.sub, foreignResp.StatusCode)
			}
		}
		projecttest.AssertSameShape(t, foreignResp, missingResp)
	}

	thread, err := threads.List(t.Context(), f.A.ProjectUID, comments.TaskSubject(f.A.TaskUID))
	if err != nil || len(thread) != 0 {
		t.Errorf("A's thread after B's attempts = %d comments, %v; want none", len(thread), err)
	}
	survived := f.Get(t, f.ClientFor(t, server, projecttest.SideA), f.PathFor(projecttest.SideA, foreign))
	defer func() { _ = survived.Body.Close() }()
	var task phototask.Task
	if err := json.NewDecoder(survived.Body).Decode(&task); err != nil {
		t.Fatalf("decoding A's task: %v", err)
	}
	if survived.StatusCode != http.StatusOK || task.Title == "Uneseno" || task.PhotoCount != 1 ||
		len(task.Participants) != 0 {
		t.Fatalf("A's task after B's attempts = %d %+v, want it untouched", survived.StatusCode, task)
	}
}

// TestTasks_listAndSummaryArePerProject asserts B's listing never names A's
// task and B's badge counts only B's one open task.
func TestTasks_listAndSummaryArePerProject(t *testing.T) {
	server, f, _ := newIsolationServer(t)
	client := f.ClientFor(t, server, projecttest.SideB)

	f.AssertListExcludes(t, client, f.PathFor(projecttest.SideB, "tasks"), f.A.TaskUID, f.A.PhotoUID)
	f.AssertListExcludes(t, client, f.PathFor(projecttest.SideB, "tasks?photo="+f.A.PhotoUID), f.A.TaskUID)

	resp := f.Get(t, client, f.PathFor(projecttest.SideB, "tasks/summary"))
	defer func() { _ = resp.Body.Close() }()
	var summary phototask.Summary
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		t.Fatalf("decoding the summary: %v", err)
	}
	if resp.StatusCode != http.StatusOK || summary.Open != 1 {
		t.Fatalf("B's summary = %d %+v, want 200 and one open task", resp.StatusCode, summary)
	}
}

// TestTasks_foreignAccountIsMissing hands B's task — on the assign route and at
// an opening — to A's member, who is the instance's system administrator but
// no member of B, and asserts each answer is byte-for-byte the 404 a uid with
// no account gets: the task is neither a way to learn whether an account exists
// and what it is called, nor to put a stranger on B's work. B's own member is
// still accepted, and A's member appears in none of B's tasks afterwards.
func TestTasks_foreignAccountIsMissing(t *testing.T) {
	server, f, _ := newIsolationServer(t, projecttest.AsMaintainer(projecttest.SideA))
	client := f.ClientFor(t, server, projecttest.SideB)
	missing := projecttest.MissingUID("us")
	assign := f.PathFor(projecttest.SideB, "tasks/"+f.B.TaskUID+"/participants")

	foreignResp := f.Send(t, client, http.MethodPost, assign, map[string]string{"user_uid": f.A.UserUID})
	missingResp := f.Send(t, client, http.MethodPost, assign, map[string]string{"user_uid": missing})
	if foreignResp.StatusCode != http.StatusNotFound {
		t.Errorf("assigning A's member to B's task = %d, want 404", foreignResp.StatusCode)
	}
	projecttest.AssertSameShape(t, foreignResp, missingResp)

	open := f.PathFor(projecttest.SideB, "tasks")
	foreignResp = f.Send(t, client, http.MethodPost, open, map[string]any{
		"title": "Kdo to je?", "photo_uids": []string{f.B.PhotoUID}, "participants": []string{f.A.UserUID},
	})
	missingResp = f.Send(t, client, http.MethodPost, open, map[string]any{
		"title": "Kdo to je?", "photo_uids": []string{f.B.PhotoUID}, "participants": []string{missing},
	})
	if foreignResp.StatusCode != http.StatusNotFound {
		t.Errorf("opening a B task asking A's member = %d, want 404", foreignResp.StatusCode)
	}
	projecttest.AssertSameShape(t, foreignResp, missingResp)

	own := f.Send(t, client, http.MethodPost, assign, map[string]string{"user_uid": f.B.UserUID})
	_ = own.Body.Close()
	if own.StatusCode != http.StatusOK {
		t.Errorf("assigning B's own member = %d, want 200", own.StatusCode)
	}
	f.AssertListExcludes(t, client, f.PathFor(projecttest.SideB, "tasks"), f.A.UserUID)
	f.AssertListExcludes(t, client, assign, f.A.UserUID)
}
