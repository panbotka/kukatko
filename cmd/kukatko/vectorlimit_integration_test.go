//go:build integration

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/project"
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
