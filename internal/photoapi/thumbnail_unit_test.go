package photoapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/thumbjob"
)

// fakeRegenerator is a controllable ThumbnailRegenerator for the handler tests.
type fakeRegenerator struct {
	sizes  []string
	err    error
	gotUID string
	calls  int
}

// ForceRegenerate records the uid and returns the configured sizes/error.
func (f *fakeRegenerator) ForceRegenerate(_ context.Context, uid string) ([]string, error) {
	f.calls++
	f.gotUID = uid
	return f.sizes, f.err
}

// regenPass is a pass-through guard for routes the regenerate tests do not gate.
func regenPass(next http.Handler) http.Handler { return next }

// regenDeny models the RequireWrite guard rejecting a viewer, short-circuiting
// with 403 before the handler runs.
func regenDeny(http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})
}

// regenRouter mounts only the regenerate handler (no auth middleware) so the
// handler logic can be exercised directly.
func regenRouter(api *API) http.Handler {
	r := chi.NewRouter()
	r.Post("/photos/{uid}/regenerate-thumbnail", api.handleRegenerateThumbnail)
	return r
}

func TestHandleRegenerateThumbnail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		regenerator *fakeRegenerator
		wantStatus  int
	}{
		{name: "no backend", regenerator: nil, wantStatus: http.StatusServiceUnavailable},
		{
			name:        "success",
			regenerator: &fakeRegenerator{sizes: []string{"tile_500", "fit_1920"}},
			wantStatus:  http.StatusOK,
		},
		{
			name:        "photo not found",
			regenerator: &fakeRegenerator{err: photos.ErrPhotoNotFound},
			wantStatus:  http.StatusNotFound,
		},
		{
			name:        "undecodable original",
			regenerator: &fakeRegenerator{err: fmt.Errorf("%w: boom", thumbjob.ErrRegenerateFailed)},
			wantStatus:  http.StatusUnprocessableEntity,
		},
		{
			name:        "internal error",
			regenerator: &fakeRegenerator{err: errors.New("boom")},
			wantStatus:  http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			api := &API{}
			if tt.regenerator != nil {
				api.regenerator = tt.regenerator
			}
			rec := httptest.NewRecorder()
			regenRouter(api).ServeHTTP(rec, req(http.MethodPost, "/photos/ph_1/regenerate-thumbnail"))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus != http.StatusOK {
				return
			}
			if tt.regenerator.gotUID != "ph_1" {
				t.Errorf("regenerated uid = %q, want ph_1", tt.regenerator.gotUID)
			}
			var body regenerateThumbnailResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Status != "regenerated" || len(body.Sizes) != 2 {
				t.Errorf("body = %+v, want status regenerated with 2 sizes", body)
			}
		})
	}
}

// TestRegenerateThumbnailRBAC verifies the regenerate route is guarded by the
// write gate: a request the guard rejects (a viewer) is forbidden and never
// reaches the regenerator, while an allowed request (editor/admin) does.
func TestRegenerateThumbnailRBAC(t *testing.T) {
	t.Parallel()

	t.Run("viewer forbidden", func(t *testing.T) {
		t.Parallel()
		fake := &fakeRegenerator{}
		api := &API{
			regenerator:     fake,
			requireAuth:     regenPass,
			requireWrite:    regenDeny,
			requireDownload: regenPass,
		}
		r := chi.NewRouter()
		api.RegisterRoutes(r)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req(http.MethodPost, "/photos/ph_1/regenerate-thumbnail"))

		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
		if fake.calls != 0 {
			t.Errorf("regenerator called %d times for a forbidden request", fake.calls)
		}
	})

	t.Run("editor allowed", func(t *testing.T) {
		t.Parallel()
		fake := &fakeRegenerator{sizes: []string{"tile_500"}}
		api := &API{
			regenerator:     fake,
			requireAuth:     regenPass,
			requireWrite:    regenPass,
			requireDownload: regenPass,
		}
		r := chi.NewRouter()
		api.RegisterRoutes(r)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req(http.MethodPost, "/photos/ph_1/regenerate-thumbnail"))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if fake.calls != 1 || fake.gotUID != "ph_1" {
			t.Errorf("regenerator calls=%d uid=%q, want 1 and ph_1", fake.calls, fake.gotUID)
		}
	})
}
