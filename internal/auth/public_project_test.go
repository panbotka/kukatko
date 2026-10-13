package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/panbotka/kukatko/internal/project"
)

// TestRequireActiveProject covers the one anonymous project guard: it admits
// an existing, active project and records it for PublicProjectFromContext only
// — never as an authorised project — and answers the same 404 for no project,
// an unknown slug and a deactivated project.
func TestRequireActiveProject(t *testing.T) {
	t.Parallel()
	active := &project.Project{UID: "pr1", Slug: "rodina", Name: "Rodina", Active: true}
	inactive := &project.Project{UID: "pr2", Slug: "stara", Name: "Stará", Active: false}
	tests := []struct {
		name       string
		req        *projectRequest
		wantStatus int
	}{
		{name: "no WithProject", req: nil, wantStatus: http.StatusNotFound},
		{name: "unknown slug", req: &projectRequest{err: project.ErrNotFound}, wantStatus: http.StatusNotFound},
		{name: "deactivated", req: &projectRequest{project: inactive}, wantStatus: http.StatusNotFound},
		{name: "lookup failed", req: &projectRequest{err: errors.New("boom")}, wantStatus: http.StatusInternalServerError},
		{name: "active", req: &projectRequest{project: active}, wantStatus: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var seen *project.Project
			var authorised bool
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen, _ = PublicProjectFromContext(r.Context())
				_, authorised = ProjectFromContext(r.Context())
				w.WriteHeader(http.StatusOK)
			})
			ctx := context.Background()
			if tt.req != nil {
				ctx = context.WithValue(ctx, projectRequestKey{}, *tt.req)
			}
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/p/x/settings/public", nil)
			rec := httptest.NewRecorder()
			RequireActiveProject(next).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusNotFound && rec.Body.String() != `{"error":"project not found"}`+"\n" {
				t.Fatalf("404 body = %q, want the shared project-not-found body", rec.Body.String())
			}
			if tt.wantStatus != http.StatusOK {
				return
			}
			if seen != active {
				t.Fatalf("PublicProjectFromContext = %+v, want the active project", seen)
			}
			if authorised {
				t.Fatal("ProjectFromContext reports a project behind the anonymous guard")
			}
		})
	}
}
