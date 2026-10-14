package photoapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestVectorRateLimit_mountedOnTheVectorRoutes verifies the vector throttle
// guards exactly the two routes that run a vector search, inside the auth guard:
// a refused caller is answered 401 by the guard without spending a token, an
// admitted one reaches the throttle (which here refuses with 429).
func TestVectorRateLimit_mountedOnTheVectorRoutes(t *testing.T) {
	t.Parallel()

	pass := func(next http.Handler) http.Handler { return next }
	paths := []string{"/search?q=les", "/photos/ph_1/similar"}
	for _, admitted := range []bool{false, true} {
		charged := 0
		auth := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !admitted {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				next.ServeHTTP(w, r)
			})
		}
		api := &API{
			requireAuth: auth, requireCurator: pass, requireWrite: pass,
			requireAdmin: pass, requireMaintainer: pass, requireDownload: pass,
			vectorRateLimit: func(http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					charged++
					w.WriteHeader(http.StatusTooManyRequests)
				})
			},
		}
		r := chi.NewRouter()
		api.RegisterRoutes(r)
		want := http.StatusUnauthorized
		if admitted {
			want = http.StatusTooManyRequests
		}
		for _, path := range paths {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
			if rec.Code != want {
				t.Errorf("admitted=%v GET %s = %d, want %d", admitted, path, rec.Code, want)
			}
		}
		if wantCharged := map[bool]int{false: 0, true: len(paths)}[admitted]; charged != wantCharged {
			t.Errorf("admitted=%v: throttle charged %d times, want %d", admitted, charged, wantCharged)
		}
	}
}
