package bulkapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"

	"github.com/panbotka/kukatko/internal/bulkapi"
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
// guard.
type guardedRoute struct {
	method, path string
	curator      bool
}

// TestGuards_curatorBulkApply proves the bulk apply hangs on RequireCurator
// (the field rule then limits a curator inside the handler) while the location
// summary stays on RequireWrite: it feeds only the bulk location operation, which
// a curator may not use. A viewer is refused both.
func TestGuards_curatorBulkApply(t *testing.T) {
	t.Parallel()
	routes := []guardedRoute{
		{http.MethodPost, "/photos/bulk", true},
		{http.MethodPost, "/photos/bulk/location-summary", false},
	}
	for _, role := range []auth.Role{auth.RoleViewer, auth.RoleCurator, auth.RoleEditor} {
		router := chi.NewRouter()
		router.Route("/api/v1", bulkapi.NewAPI(bulkapi.Config{
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
