package photoapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/comments"
)

// fakeComments is a controllable CommentStore for the handler unit tests.
type fakeComments struct {
	counts  map[string]int
	err     error
	gotUIDs []string
}

// List returns nothing; the thread endpoint is covered by the integration tests.
func (f *fakeComments) List(_ context.Context, _ string) ([]comments.Comment, error) {
	return nil, f.err
}

// CountsAmong records the requested UIDs and returns the configured counts or error.
func (f *fakeComments) CountsAmong(_ context.Context, photoUIDs []string) (map[string]int, error) {
	f.gotUIDs = photoUIDs
	if f.err != nil {
		return nil, f.err
	}
	return f.counts, nil
}

// Get is unused by these tests.
func (f *fakeComments) Get(_ context.Context, _ string) (comments.Comment, error) {
	return comments.Comment{}, f.err
}

// Create is unused by these tests.
func (f *fakeComments) Create(
	_ context.Context, _, _, _ string, _ audit.Entry,
) (comments.Comment, error) {
	return comments.Comment{}, f.err
}

// Update is unused by these tests.
func (f *fakeComments) Update(_ context.Context, _, _ string, _ audit.Entry) (comments.Comment, error) {
	return comments.Comment{}, f.err
}

// Delete is unused by these tests.
func (f *fakeComments) Delete(_ context.Context, _ string, _ audit.Entry) error { return f.err }

// TestCanEditComment verifies only the author may rewrite a comment, and that a
// comment left authorless by a deleted account is editable by nobody.
func TestCanEditComment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		user auth.User
		c    comments.Comment
		want bool
	}{
		{
			name: "the author may edit",
			user: auth.User{UID: "us_1", Role: auth.RoleViewer},
			c:    comments.Comment{AuthorUID: "us_1"},
			want: true,
		},
		{
			name: "another user may not",
			user: auth.User{UID: "us_2", Role: auth.RoleEditor},
			c:    comments.Comment{AuthorUID: "us_1"},
		},
		{
			name: "an admin may not edit someone else's",
			user: auth.User{UID: "us_2", Role: auth.RoleAdmin},
			c:    comments.Comment{AuthorUID: "us_1"},
		},
		{
			name: "an authorless comment belongs to nobody",
			user: auth.User{UID: "", Role: auth.RoleAdmin},
			c:    comments.Comment{AuthorUID: ""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := canEditComment(tt.user, tt.c); got != tt.want {
				t.Errorf("canEditComment(%+v, %+v) = %v, want %v", tt.user, tt.c, got, tt.want)
			}
		})
	}
}

// TestCanDeleteComment verifies the author and any admin (or maintainer, up the
// ladder) may remove a comment, and that editors and viewers may not touch
// someone else's.
func TestCanDeleteComment(t *testing.T) {
	t.Parallel()

	someone := comments.Comment{AuthorUID: "us_1"}
	tests := []struct {
		name string
		user auth.User
		c    comments.Comment
		want bool
	}{
		{name: "the author", user: auth.User{UID: "us_1", Role: auth.RoleViewer}, c: someone, want: true},
		{name: "an admin", user: auth.User{UID: "us_2", Role: auth.RoleAdmin}, c: someone, want: true},
		{name: "a maintainer", user: auth.User{UID: "us_2", Role: auth.RoleMaintainer}, c: someone, want: true},
		{name: "an editor who is not the author", user: auth.User{UID: "us_2", Role: auth.RoleEditor}, c: someone},
		{name: "a viewer who is not the author", user: auth.User{UID: "us_2", Role: auth.RoleViewer}, c: someone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := canDeleteComment(tt.user, tt.c); got != tt.want {
				t.Errorf("canDeleteComment(%+v) = %v, want %v", tt.user, got, tt.want)
			}
		})
	}
}

// TestCommentHandlers_noBackend verifies every comment endpoint answers 503 when
// no comments backend is wired, before any auth or body parsing.
func TestCommentHandlers_noBackend(t *testing.T) {
	t.Parallel()

	api := &API{} // no comments store
	cases := []struct {
		name    string
		method  string
		body    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{name: "list", method: http.MethodGet, handler: api.handleListComments},
		{name: "create", method: http.MethodPost, body: `{"body":"hi"}`, handler: api.handleCreateComment},
		{name: "update", method: http.MethodPatch, body: `{"body":"hi"}`, handler: api.handleUpdateComment},
		{name: "delete", method: http.MethodDelete, handler: api.handleDeleteComment},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(
				t.Context(), tc.method, "/api/v1/photos/ph_1/comments", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			tc.handler(rec, req)
			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("%s status = %d, want 503", tc.method, rec.Code)
			}
		})
	}
}

// TestCommentHandlers_unauthenticated verifies the mutating endpoints answer 401
// when no principal is on the context — the guard is the route's, but a handler
// reached without one must never write on behalf of nobody.
func TestCommentHandlers_unauthenticated(t *testing.T) {
	t.Parallel()

	api := &API{comments: &fakeComments{}}
	cases := []struct {
		name    string
		method  string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{name: "create", method: http.MethodPost, handler: api.handleCreateComment},
		{name: "update", method: http.MethodPatch, handler: api.handleUpdateComment},
		{name: "delete", method: http.MethodDelete, handler: api.handleDeleteComment},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(
				t.Context(), tc.method, "/api/v1/photos/ph_1/comments", strings.NewReader(`{"body":"hi"}`))
			rec := httptest.NewRecorder()
			tc.handler(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s status = %d, want 401", tc.method, rec.Code)
			}
		})
	}
}

// TestDecodeComment verifies the request body decoding: a plain body is accepted
// verbatim (trimming and length are the store's job), and a malformed body or an
// unknown field is rejected.
func TestDecodeComment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{name: "plain body", body: `{"body":"  hello  "}`, want: "  hello  "},
		{name: "missing body key is an empty string", body: `{}`},
		{name: "unknown field", body: `{"body":"hi","author":"me"}`, wantErr: true},
		{name: "malformed json", body: `{"body":`, wantErr: true},
		{name: "empty request", body: ``, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(
				t.Context(), http.MethodPost, "/api/v1/photos/ph_1/comments", strings.NewReader(tt.body))
			got, err := decodeComment(req)
			if (err != nil) != tt.wantErr {
				t.Fatalf("decodeComment(%q) error = %v, wantErr %v", tt.body, err, tt.wantErr)
			}
			if !tt.wantErr && got.Body != tt.want {
				t.Errorf("decodeComment(%q) = %q, want %q", tt.body, got.Body, tt.want)
			}
		})
	}
}

// TestWriteCommentError verifies the store errors map to the right status codes.
func TestWriteCommentError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "missing photo", err: comments.ErrPhotoNotFound, want: http.StatusNotFound},
		{name: "missing comment", err: comments.ErrNotFound, want: http.StatusNotFound},
		{name: "empty body", err: comments.ErrEmptyBody, want: http.StatusBadRequest},
		{name: "over-long body", err: comments.ErrBodyTooLong, want: http.StatusBadRequest},
		{name: "anything else", err: errors.New("boom"), want: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			writeCommentError(rec, tt.err, "it failed")
			if rec.Code != tt.want {
				t.Errorf("writeCommentError(%v) status = %d, want %d", tt.err, rec.Code, tt.want)
			}
		})
	}
}

// TestCommentCount verifies the detail's count: it asks the bulk lookup for the
// one photo, reports zero for a photo with no comments and for a missing backend,
// and propagates a store failure.
func TestCommentCount(t *testing.T) {
	t.Parallel()

	t.Run("counts through the bulk lookup", func(t *testing.T) {
		t.Parallel()
		fake := &fakeComments{counts: map[string]int{"ph_1": 3}}
		api := &API{comments: fake}
		got, err := api.commentCount(context.Background(), "ph_1")
		if err != nil {
			t.Fatalf("commentCount: %v", err)
		}
		if got != 3 {
			t.Errorf("commentCount = %d, want 3", got)
		}
		if len(fake.gotUIDs) != 1 || fake.gotUIDs[0] != "ph_1" {
			t.Errorf("CountsAmong got %v, want exactly [ph_1]", fake.gotUIDs)
		}
	})

	t.Run("a photo with no comments counts zero", func(t *testing.T) {
		t.Parallel()
		api := &API{comments: &fakeComments{counts: map[string]int{}}}
		got, err := api.commentCount(context.Background(), "ph_1")
		if err != nil || got != 0 {
			t.Errorf("commentCount = %d, %v, want 0, nil", got, err)
		}
	})

	t.Run("no backend counts zero without failing the detail", func(t *testing.T) {
		t.Parallel()
		api := &API{}
		got, err := api.commentCount(context.Background(), "ph_1")
		if err != nil || got != 0 {
			t.Errorf("commentCount = %d, %v, want 0, nil", got, err)
		}
	})

	t.Run("propagates a store failure", func(t *testing.T) {
		t.Parallel()
		api := &API{comments: &fakeComments{err: errors.New("boom")}}
		if _, err := api.commentCount(context.Background(), "ph_1"); err == nil {
			t.Fatal("commentCount error = nil, want the store error")
		}
	})
}
