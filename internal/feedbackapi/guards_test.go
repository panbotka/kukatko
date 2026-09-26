package feedbackapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
)

// roleGuard stands in for one of auth's role middlewares as a single signed-in
// user meets it: refused answers 403, admitted answers 204 right at the guard
// instead of handing on. The handler never runs, so the test pins which guard a
// route hangs on — the wiring — without needing any of the handler's dependencies.
func roleGuard(allowed bool) func(http.Handler) http.Handler {
	return func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if allowed {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.WriteHeader(http.StatusForbidden)
		})
	}
}

// guardedRoute is one route of the package and whether a curator may pass its
// guard: true for the people-and-faces routes that moved to RequireCurator.
type guardedRoute struct {
	method, path string
	curator      bool
}

// TestGuards_curatorOpinionsButNotDuplicates proves the split of this package: the
// face, label and repeated-marker opinions hang on RequireCurator, so a curator
// passes them, while the near-duplicate-photo opinions stay on RequireWrite and
// refuse the same curator. An editor passes all twelve, a viewer none.
func TestGuards_curatorOpinionsButNotDuplicates(t *testing.T) {
	t.Parallel()
	routes := []guardedRoute{
		{http.MethodPost, "/feedback/face-rejections", true},
		{http.MethodDelete, "/feedback/face-rejections", true},
		{http.MethodPost, "/feedback/face-confirmations", true},
		{http.MethodDelete, "/feedback/face-confirmations", true},
		{http.MethodPost, "/feedback/label-rejections", true},
		{http.MethodDelete, "/feedback/label-rejections", true},
		{http.MethodPost, "/feedback/duplicate-marker-dismissals", true},
		{http.MethodDelete, "/feedback/duplicate-marker-dismissals", true},
		{http.MethodPost, "/feedback/duplicate-dismissals", false},
		{http.MethodDelete, "/feedback/duplicate-dismissals", false},
		{http.MethodPost, "/feedback/duplicate-confirmations", false},
		{http.MethodDelete, "/feedback/duplicate-confirmations", false},
	}
	for _, role := range []auth.Role{auth.RoleViewer, auth.RoleCurator, auth.RoleEditor} {
		router := chi.NewRouter()
		router.Route("/api/v1", NewAPI(Config{
			RequireCurator: roleGuard(role.CanCurate()),
			RequireWrite:   roleGuard(role.CanWrite()),
		}).RegisterRoutes)
		for _, route := range routes {
			want := http.StatusForbidden
			if role.CanWrite() || (role == auth.RoleCurator && route.curator) {
				want = http.StatusNoContent
			}
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(context.Background(), route.method,
				"/api/v1"+route.path, strings.NewReader(`{}`))
			router.ServeHTTP(rec, req)
			if rec.Code != want {
				t.Errorf("%s %s %s = %d, want %d", role, route.method, route.path, rec.Code, want)
			}
		}
	}
}
