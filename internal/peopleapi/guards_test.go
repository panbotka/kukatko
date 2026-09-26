package peopleapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"

	"github.com/panbotka/kukatko/internal/peopleapi"
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

// TestGuards_curatorEditsSubjects proves the four subject mutations hang on
// RequireCurator — deleting and merging included: a curator and an editor pass
// their guard, a viewer is refused. The package has no route left on RequireWrite.
func TestGuards_curatorEditsSubjects(t *testing.T) {
	t.Parallel()
	routes := []guardedRoute{
		{http.MethodPost, "/subjects", true},
		{http.MethodPatch, "/subjects/su1", true},
		{http.MethodDelete, "/subjects/su1", true},
		{http.MethodPost, "/subjects/su1/merge", true},
	}
	for _, role := range []auth.Role{auth.RoleViewer, auth.RoleCurator, auth.RoleEditor} {
		router := chi.NewRouter()
		router.Route("/api/v1", peopleapi.NewAPI(peopleapi.Config{
			RequireCurator: roleGuard(role.CanCurate()),
			RequireAuth:    roleGuard(role.Valid()),
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
