package bulkapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/bulk"
	"github.com/panbotka/kukatko/internal/ratelimit"
)

// passthrough is a no-op middleware standing in for the write guard in tests.
func passthrough(next http.Handler) http.Handler { return next }

// TestToOperations verifies the wire form resolves into the expected operations
// and rejects conflicting or invalid inputs.
func TestToOperations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      operationsInput
		wantErr bool
		check   func(*testing.T, bulk.Operations)
	}{
		{
			name: "caption set maps to title",
			in:   operationsInput{SetCaption: new("hello")},
			check: func(t *testing.T, ops bulk.Operations) {
				if ops.Title == nil || *ops.Title != "hello" {
					t.Errorf("Title = %v, want hello", ops.Title)
				}
			},
		},
		{
			name: "clear description sets empty pointer",
			in:   operationsInput{ClearDescription: true},
			check: func(t *testing.T, ops bulk.Operations) {
				if ops.Description == nil || *ops.Description != "" {
					t.Errorf("Description = %v, want empty pointer", ops.Description)
				}
			},
		},
		{
			name:    "set and clear caption conflict",
			in:      operationsInput{SetCaption: new("x"), ClearCaption: true},
			wantErr: true,
		},
		{
			name:    "archive and unarchive conflict",
			in:      operationsInput{Archive: true, Unarchive: true},
			wantErr: true,
		},
		{
			name: "archive sets pointer true",
			in:   operationsInput{Archive: true},
			check: func(t *testing.T, ops bulk.Operations) {
				if ops.Archive == nil || !*ops.Archive {
					t.Errorf("Archive = %v, want true", ops.Archive)
				}
			},
		},
		{
			name:    "set and clear location conflict",
			in:      operationsInput{SetLocation: &locationInput{Lat: 1, Lng: 2}, ClearLocation: true},
			wantErr: true,
		},
		{
			name:    "out of range latitude",
			in:      operationsInput{SetLocation: &locationInput{Lat: 200, Lng: 2}},
			wantErr: true,
		},
		{
			name: "valid location",
			in:   operationsInput{SetLocation: &locationInput{Lat: 50, Lng: 14}},
			check: func(t *testing.T, ops bulk.Operations) {
				if ops.Location == nil || ops.Location.Lat != 50 || ops.Location.Lng != 14 {
					t.Errorf("Location = %v, want {50,14}", ops.Location)
				}
			},
		},
		{
			name: "clear location",
			in:   operationsInput{ClearLocation: true},
			check: func(t *testing.T, ops bulk.Operations) {
				if !ops.ClearLocation {
					t.Errorf("ClearLocation = false, want true")
				}
			},
		},
		{
			name: "set rating",
			in:   operationsInput{SetRating: new(4)},
			check: func(t *testing.T, ops bulk.Operations) {
				if ops.Rating == nil || *ops.Rating != 4 {
					t.Errorf("Rating = %v, want 4", ops.Rating)
				}
			},
		},
		{
			name:    "rating out of range",
			in:      operationsInput{SetRating: new(6)},
			wantErr: true,
		},
		{
			name: "set flag",
			in:   operationsInput{SetFlag: new("reject")},
			check: func(t *testing.T, ops bulk.Operations) {
				if ops.Flag == nil || *ops.Flag != "reject" {
					t.Errorf("Flag = %v, want reject", ops.Flag)
				}
			},
		},
		{
			name:    "unknown flag",
			in:      operationsInput{SetFlag: new("star")},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ops, err := tt.in.toOperations()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("toOperations() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("toOperations() error = %v, want nil", err)
			}
			if tt.check != nil {
				tt.check(t, ops)
			}
		})
	}
}

// TestValidateCoords verifies the coordinate bounds.
func TestValidateCoords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		lat, lng float64
		wantErr  bool
	}{
		{"valid", 50, 14, false},
		{"min edge", -90, -180, false},
		{"max edge", 90, 180, false},
		{"lat too high", 90.1, 0, true},
		{"lng too low", 0, -181, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateCoords(tt.lat, tt.lng)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateCoords(%g,%g) error = %v, wantErr %v", tt.lat, tt.lng, err, tt.wantErr)
			}
		})
	}
}

// TestBulkStatus verifies each sentinel maps to the right HTTP status.
func TestBulkStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"no photos", bulk.ErrNoPhotos, http.StatusBadRequest},
		{"no operations", bulk.ErrNoOperations, http.StatusBadRequest},
		{"album not found", bulk.ErrAlbumNotFound, http.StatusBadRequest},
		{"label not found", bulk.ErrLabelNotFound, http.StatusBadRequest},
		{"batch too large", bulk.ErrBatchTooLarge, http.StatusRequestEntityTooLarge},
		{"other", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, _ := bulkStatus(tt.err)
			if got != tt.want {
				t.Errorf("bulkStatus(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

// TestHandleBulk_unauthenticated verifies a request without an authenticated
// user is rejected before any work, since the acting user is required for
// favorites and the audit entry.
func TestHandleBulk_unauthenticated(t *testing.T) {
	t.Parallel()

	api := NewAPI(Config{Service: stubService{}, RequireWrite: passthrough})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/photos/bulk",
		strings.NewReader(`{"photo_uids":["ph1"],"operations":{"archive":true}}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// TestHandleBulk_rateLimited verifies the injected rate-limit middleware is wired
// into the route: once a client IP exhausts its burst, further requests are
// rejected with 429 before reaching the handler.
func TestHandleBulk_rateLimited(t *testing.T) {
	t.Parallel()

	// A burst of 2 with a negligible refill rate so the third request within the
	// test window is denied.
	limiter := ratelimit.New(0.0001, 2)
	api := NewAPI(Config{Service: stubService{}, RequireWrite: passthrough, RateLimit: limiter.Middleware})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)

	do := func() int {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/photos/bulk",
			strings.NewReader(`{"photo_uids":["ph1"],"operations":{"archive":true}}`))
		req.RemoteAddr = "203.0.113.7:9000"
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	// The stub service has no authenticated user, so allowed requests return 401;
	// the point is that the first two pass the limiter and the third is throttled.
	if got := do(); got != http.StatusUnauthorized {
		t.Fatalf("first request: status = %d, want 401 (passed limiter)", got)
	}
	if got := do(); got != http.StatusUnauthorized {
		t.Fatalf("second request: status = %d, want 401 (passed limiter)", got)
	}
	if got := do(); got != http.StatusTooManyRequests {
		t.Fatalf("third request: status = %d, want 429 (throttled)", got)
	}
}

// stubService is a Service that records nothing and returns an empty result.
type stubService struct{}

// Apply returns an empty result, satisfying the Service interface for tests that
// never reach it.
func (stubService) Apply(
	_ context.Context, _ string, _ []string, _ bulk.Operations,
) (bulk.Result, error) {
	return bulk.Result{}, nil
}
