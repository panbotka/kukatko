//go:build integration

package main

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/candidatesapi"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/duplicatesapi"
	"github.com/panbotka/kukatko/internal/expandapi"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/reviewapi"
	"github.com/panbotka/kukatko/internal/sweepapi"
)

// throttledStatus runs one request through h as user in project p and returns
// the status it was answered with.
func throttledStatus(t *testing.T, h http.Handler, user string, p *project.Project) int {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/search", nil)
	req = req.WithContext(auth.ContextWithProject(req.Context(), auth.User{UID: user}, p, project.RoleViewer))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// TestNewVectorThrottle_userThenProject verifies the vector throttle's two
// buckets and their order. With a per-user burst of 2 under a per-project burst
// of 3: one member gets two searches and a 429, and that refusal spends nothing
// of the project's bucket, so a second member still gets one search before the
// project's three tokens are gone and the project as a whole is refused. A
// member of another project is untouched by all of it.
func TestNewVectorThrottle_userThenProject(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{RateLimit: config.RateLimitConfig{
		Vector:        config.RateLimitRule{RatePerSec: 0.001, Burst: 2},
		VectorProject: config.RateLimitRule{RatePerSec: 0.001, Burst: 3},
	}}
	h := newVectorThrottle(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	projA := &project.Project{UID: "pj-a", Slug: "a"}
	projB := &project.Project{UID: "pj-b", Slug: "b"}

	steps := []struct {
		user string
		p    *project.Project
		want int
	}{
		{"us-1", projA, http.StatusNoContent},
		{"us-1", projA, http.StatusNoContent},
		{"us-1", projA, http.StatusTooManyRequests}, // its own share spent; the project's untouched
		{"us-2", projA, http.StatusNoContent},       // the project's third and last token
		{"us-3", projA, http.StatusTooManyRequests}, // the project's bucket is empty
		{"us-4", projB, http.StatusNoContent},       // another project has its own
	}
	for i, step := range steps {
		if got := throttledStatus(t, h, step.user, step.p); got != step.want {
			t.Errorf("step %d (%s in %s) = %d, want %d", i, step.user, step.p.UID, got, step.want)
		}
	}
}

// heavyRoute is one of the heavy vector routes, as the end-to-end throttle test
// sends it.
type heavyRoute struct{ method, path string }

// heavyRoutes are the five route groups the heavy vector throttle guards,
// mounted from their real packages (without a service, so an admitted request
// answers 503) behind a guard that admits whoever the request names: the
// X-Test-User header in the project with the X-Test-Project uid.
func heavyRoutes(t *testing.T, throttle func(http.Handler) http.Handler) http.Handler {
	t.Helper()
	projects := map[string]*project.Project{
		"pj-a": {UID: "pj-a", Slug: "a"},
		"pj-b": {UID: "pj-b", Slug: "b"},
	}
	guard := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.ContextWithProject(r.Context(), auth.User{UID: r.Header.Get("X-Test-User")},
				projects[r.Header.Get("X-Test-Project")], project.RoleEditor)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	router := chi.NewRouter()
	router.Route("/api/v1", func(r chi.Router) {
		duplicatesapi.NewAPI(duplicatesapi.Config{RequireWrite: guard, RateLimit: throttle}).RegisterRoutes(r)
		reviewapi.NewAPI(reviewapi.Config{RequireCurator: guard, RequireAuth: guard, RateLimit: throttle}).
			RegisterRoutes(r)
		sweepapi.NewAPI(sweepapi.Config{RequireCurator: guard, RateLimit: throttle}).RegisterRoutes(r)
		candidatesapi.NewAPI(candidatesapi.Config{RequireCurator: guard, RateLimit: throttle}).RegisterRoutes(r)
		expandapi.NewAPI(expandapi.Config{RequireCurator: guard, RateLimit: throttle}).RegisterRoutes(r)
	})
	return router
}

// sendHeavy sends route as user in the project with uid projectUID and returns
// the recorded answer.
func sendHeavy(t *testing.T, h http.Handler, route heavyRoute, user, projectUID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), route.method, "/api/v1"+route.path, strings.NewReader("{}"))
	req.Header.Set("X-Test-User", user)
	req.Header.Set("X-Test-Project", projectUID)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestNewHeavyVectorThrottle_burstAcrossRoutes drives a burst through the real
// heavy vector routes (duplicates, review queue, sweep, candidates, expansion)
// behind one newHeavyVectorThrottle, with a per-user burst of 2 under a
// per-project burst of 3. One member spends their two tokens on two different
// routes and the third route answers 429 with the limiter's documented shape
// (Retry-After, JSON {"error":"rate limit exceeded"}) — spreading over the
// routes buys nothing. A second member gets the project's last token and a
// third is refused, while a member of another project is untouched. The routes
// beside them that scan nothing (the merge, the answer) never draw on it.
func TestNewHeavyVectorThrottle_burstAcrossRoutes(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{RateLimit: config.RateLimitConfig{
		HeavyVector:        config.RateLimitRule{RatePerSec: 0.001, Burst: 2},
		HeavyVectorProject: config.RateLimitRule{RatePerSec: 0.001, Burst: 3},
	}}
	h := heavyRoutes(t, newHeavyVectorThrottle(cfg))
	var (
		duplicates = heavyRoute{http.MethodGet, "/duplicates"}
		queue      = heavyRoute{http.MethodGet, "/review/queue"}
		sweep      = heavyRoute{http.MethodGet, "/faces/sweep"}
		candidates = heavyRoute{http.MethodPost, "/subjects/su-1/candidates"}
		expand     = heavyRoute{http.MethodGet, "/albums/al-1/similar"}
		merge      = heavyRoute{http.MethodPost, "/duplicates/merge"}
		answer     = heavyRoute{http.MethodPost, "/review/answer"}
	)

	steps := []struct {
		route      heavyRoute
		user, proj string
		throttled  bool
	}{
		{duplicates, "us-1", "pj-a", false},
		{queue, "us-1", "pj-a", false},
		{sweep, "us-1", "pj-a", true}, // its own share spent on two other routes
		{merge, "us-1", "pj-a", false},
		{answer, "us-1", "pj-a", false},
		{candidates, "us-2", "pj-a", false}, // the project's third and last token
		{expand, "us-3", "pj-a", true},      // the project's bucket is empty
		{duplicates, "us-4", "pj-b", false}, // another project has its own
		{sweep, "us-4", "pj-b", false},
	}
	for i, step := range steps {
		rec := sendHeavy(t, h, step.route, step.user, step.proj)
		if got := rec.Code == http.StatusTooManyRequests; got != step.throttled {
			t.Errorf("step %d (%s %s as %s in %s) = %d, want throttled %v",
				i, step.route.method, step.route.path, step.user, step.proj, rec.Code, step.throttled)
			continue
		}
		if step.throttled {
			assertTooManyRequestsShape(t, rec)
		}
	}
}

// assertTooManyRequestsShape checks a 429 carries what docs/API.md promises: a
// Retry-After hint and the JSON body {"error":"rate limit exceeded"}.
func assertTooManyRequestsShape(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if got := rec.Header().Get("Retry-After"); got != "1" {
		t.Errorf("Retry-After = %q, want %q", got, "1")
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode 429 body %q: %v", rec.Body.String(), err)
	}
	if want := map[string]string{"error": "rate limit exceeded"}; !maps.Equal(body, want) {
		t.Errorf("429 body = %v, want %v", body, want)
	}
}
