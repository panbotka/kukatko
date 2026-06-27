package photoapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/trash"
)

// fakePurger is a controllable Purger for the trash handler tests.
type fakePurger struct {
	purgeErr   error
	purgedUID  string
	emptyRes   trash.Result
	emptyErr   error
	emptyCalls int
}

// PurgePhoto records the uid and returns the configured error.
func (f *fakePurger) PurgePhoto(_ context.Context, uid string) error {
	f.purgedUID = uid
	return f.purgeErr
}

// EmptyTrash counts its calls and returns the configured result/error.
func (f *fakePurger) EmptyTrash(_ context.Context) (trash.Result, error) {
	f.emptyCalls++
	return f.emptyRes, f.emptyErr
}

// trashRouter mounts only the trash handlers (no auth middleware: the guards are
// injected separately and unit tests exercise the handler logic directly).
func trashRouter(api *API) http.Handler {
	r := chi.NewRouter()
	r.Get("/trash/info", api.handleTrashInfo)
	r.Post("/trash/empty", api.handleEmptyTrash)
	r.Post("/photos/{uid}/purge", api.handlePurge)
	return r
}

// req builds a context-carrying request for the trash handler tests.
func req(method, url string) *http.Request {
	return httptest.NewRequestWithContext(context.Background(), method, url, nil)
}

func TestHandleTrashInfo(t *testing.T) {
	t.Parallel()
	api := &API{retentionDays: 30}
	rec := httptest.NewRecorder()
	trashRouter(api).ServeHTTP(rec, req(http.MethodGet, "/trash/info"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body trashInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.RetentionDays != 30 {
		t.Errorf("retention_days = %d, want 30", body.RetentionDays)
	}
}

func TestHandlePurge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		purger     *fakePurger
		url        string
		wantStatus int
	}{
		{name: "no backend", purger: nil, url: "/photos/ph_1/purge?confirm=true", wantStatus: 503},
		{name: "missing confirm", purger: &fakePurger{}, url: "/photos/ph_1/purge", wantStatus: 400},
		{
			name:       "not found",
			purger:     &fakePurger{purgeErr: photos.ErrPhotoNotFound},
			url:        "/photos/ph_1/purge?confirm=true",
			wantStatus: 404,
		},
		{
			name:       "not archived",
			purger:     &fakePurger{purgeErr: trash.ErrNotArchived},
			url:        "/photos/ph_1/purge?confirm=true",
			wantStatus: 409,
		},
		{
			name:       "internal error",
			purger:     &fakePurger{purgeErr: errors.New("boom")},
			url:        "/photos/ph_1/purge?confirm=true",
			wantStatus: 500,
		},
		{name: "success", purger: &fakePurger{}, url: "/photos/ph_1/purge?confirm=true", wantStatus: 204},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			api := &API{}
			if tt.purger != nil {
				api.purger = tt.purger
			}
			rec := httptest.NewRecorder()
			trashRouter(api).ServeHTTP(rec, req(http.MethodPost, tt.url))
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusNoContent && tt.purger.purgedUID != "ph_1" {
				t.Errorf("purged uid = %q, want ph_1", tt.purger.purgedUID)
			}
		})
	}
}

func TestHandleEmptyTrash(t *testing.T) {
	t.Parallel()

	t.Run("no backend", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		trashRouter(&API{}).ServeHTTP(rec, req(http.MethodPost, "/trash/empty?confirm=true"))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
	})

	t.Run("missing confirm does not purge", func(t *testing.T) {
		t.Parallel()
		fake := &fakePurger{}
		rec := httptest.NewRecorder()
		trashRouter(&API{purger: fake}).ServeHTTP(rec, req(http.MethodPost, "/trash/empty"))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
		if fake.emptyCalls != 0 {
			t.Errorf("EmptyTrash called %d times without confirmation", fake.emptyCalls)
		}
	})

	t.Run("success returns counts", func(t *testing.T) {
		t.Parallel()
		fake := &fakePurger{emptyRes: trash.Result{Purged: 3, Failed: 1}}
		rec := httptest.NewRecorder()
		trashRouter(&API{purger: fake}).ServeHTTP(
			rec, req(http.MethodPost, "/trash/empty?confirm=true"))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body trash.Result
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Purged != 3 || body.Failed != 1 {
			t.Errorf("result = %+v, want {Purged:3 Failed:1}", body)
		}
	})

	t.Run("internal error", func(t *testing.T) {
		t.Parallel()
		fake := &fakePurger{emptyErr: errors.New("boom")}
		rec := httptest.NewRecorder()
		trashRouter(&API{purger: fake}).ServeHTTP(
			rec, req(http.MethodPost, "/trash/empty?confirm=true"))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
	})
}
