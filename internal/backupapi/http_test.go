package backupapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/backup"
)

// fakeService is a stub Service for the HTTP tests.
type fakeService struct {
	status     backup.Status
	triggerErr error
	triggered  bool
}

// Status returns the configured status.
func (f *fakeService) Status() backup.Status { return f.status }

// Trigger records the call and returns the configured error.
func (f *fakeService) Trigger(_ context.Context, _ time.Time) error {
	f.triggered = true
	return f.triggerErr
}

// passthrough is a maintainer guard that allows every request (auth is tested in
// the auth package; here we only test the backup handlers).
func passthrough(next http.Handler) http.Handler { return next }

// newRouter mounts the API under /api/v1 with the given service.
func newRouter(svc Service) http.Handler {
	api := NewAPI(Config{Service: svc, RequireMaintainer: passthrough})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	return r
}

// newRequest builds a request with a background context (noctx-clean) for the
// handler tests.
func newRequest(t *testing.T, method string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, "/api/v1/backup", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	return req
}

func TestHandleStatus_configured(t *testing.T) {
	t.Parallel()
	started := time.Date(2026, 6, 27, 3, 0, 0, 0, time.UTC)
	svc := &fakeService{status: backup.Status{
		Configured:    true,
		Running:       true,
		LastStartedAt: &started,
	}}
	rec := httptest.NewRecorder()
	newRouter(svc).ServeHTTP(rec, newRequest(t, http.MethodGet))

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200", rec.Code)
	}
	var body backup.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if !body.Configured || !body.Running {
		t.Errorf("body = %+v, want configured and running", body)
	}
}

func TestHandleStatus_notConfigured(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	// A nil interface means no destination configured.
	newRouter(nil).ServeHTTP(rec, newRequest(t, http.MethodGet))

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200", rec.Code)
	}
	var body backup.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body.Configured {
		t.Error("body.Configured = true, want false when unconfigured")
	}
}

func TestHandleTrigger(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		svc        Service
		wantStatus int
		wantCalled bool
	}{
		{
			name:       "started",
			svc:        &fakeService{},
			wantStatus: http.StatusAccepted,
			wantCalled: true,
		},
		{
			name:       "already running",
			svc:        &fakeService{triggerErr: backup.ErrAlreadyRunning},
			wantStatus: http.StatusConflict,
			wantCalled: true,
		},
		{
			name:       "not configured",
			svc:        nil,
			wantStatus: http.StatusServiceUnavailable,
			wantCalled: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			newRouter(tt.svc).ServeHTTP(rec, newRequest(t, http.MethodPost))

			if rec.Code != tt.wantStatus {
				t.Errorf("status code = %d, want %d", rec.Code, tt.wantStatus)
			}
			if fake, ok := tt.svc.(*fakeService); ok && fake.triggered != tt.wantCalled {
				t.Errorf("triggered = %v, want %v", fake.triggered, tt.wantCalled)
			}
		})
	}
}
