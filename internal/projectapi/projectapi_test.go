package projectapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/project"
)

// fakeProjects is a Projects whose answers are set per test; err, when set, is
// what every mutation returns.
type fakeProjects struct {
	overviews   []project.Overview
	err         error
	lastPatch   project.UpdatePatch
	lastCreate  project.CreateParams
	memberships map[string][]project.MembershipSummary
}

// Overviews returns the canned list.
func (f *fakeProjects) Overviews(context.Context) ([]project.Overview, error) {
	return f.overviews, nil
}

// OverviewOf returns the canned project with uid, or project.ErrNotFound.
func (f *fakeProjects) OverviewOf(_ context.Context, uid string) (*project.Overview, error) {
	for i := range f.overviews {
		if f.overviews[i].UID == uid {
			return &f.overviews[i], nil
		}
	}
	return nil, fmt.Errorf("%w: %s", project.ErrNotFound, uid)
}

// Create records p and returns a project built from it, or f.err.
func (f *fakeProjects) Create(_ context.Context, _ audit.Meta, p project.CreateParams) (*project.Project, error) {
	f.lastCreate = p
	if f.err != nil {
		return nil, f.err
	}
	return &project.Project{UID: "pjnew", Slug: p.Slug, Name: p.Name, Active: true}, nil
}

// Update records patch and returns f.err.
func (f *fakeProjects) Update(
	_ context.Context, _ audit.Meta, uid string, patch project.UpdatePatch,
) (*project.Project, error) {
	f.lastPatch = patch
	if f.err != nil {
		return nil, f.err
	}
	return &project.Project{UID: uid}, nil
}

// AddMember returns a membership or f.err.
func (f *fakeProjects) AddMember(
	_ context.Context, _ audit.Meta, projectUID, userUID string, role project.Role, _ bool,
) (*project.Member, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &project.Member{ProjectUID: projectUID, UserUID: userUID, Role: role}, nil
}

// RemoveMember returns f.err.
func (f *fakeProjects) RemoveMember(context.Context, audit.Meta, string, string) error {
	return f.err
}

// AllMemberships returns the canned map.
func (f *fakeProjects) AllMemberships(context.Context) (map[string][]project.MembershipSummary, error) {
	return f.memberships, nil
}

// MembershipsOf returns the canned memberships of userUID.
func (f *fakeProjects) MembershipsOf(_ context.Context, userUID string) ([]project.MembershipSummary, error) {
	return f.memberships[userUID], nil
}

// fakeAccounts is an Accounts over a fixed list.
type fakeAccounts struct {
	users []auth.User
}

// ListUsers returns every account.
func (f *fakeAccounts) ListUsers(context.Context, auth.UserFilter) ([]auth.User, error) {
	return f.users, nil
}

// GetUserByUID returns the account uid or auth.ErrUserNotFound.
func (f *fakeAccounts) GetUserByUID(_ context.Context, uid string) (auth.User, error) {
	for _, u := range f.users {
		if u.UID == uid {
			return u, nil
		}
	}
	return auth.User{}, auth.ErrUserNotFound
}

// SetInstanceRoleAudited changes the role in the list.
func (f *fakeAccounts) SetInstanceRoleAudited(
	_ context.Context, uid string, role auth.Role, _ audit.Entry,
) (auth.User, error) {
	for i := range f.users {
		if f.users[i].UID == uid {
			f.users[i].Role = role
			return f.users[i], nil
		}
	}
	return auth.User{}, auth.ErrUserNotFound
}

// newTestServer mounts an API over the fakes with a pass-through guard.
func newTestServer(t *testing.T, projects *fakeProjects, accounts *fakeAccounts) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{
		Projects: projects, Accounts: accounts,
		RequireMaintainer: func(next http.Handler) http.Handler { return next },
	})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// call sends method path with body and returns the status and the decoded
// error code ("" when the body is no refusal).
func call(t *testing.T, srv *httptest.Server, method, path, body string) (int, string, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var raw json.RawMessage
	_ = json.NewDecoder(resp.Body).Decode(&raw)
	var refusal errorBody
	_ = json.Unmarshal(raw, &refusal)
	return resp.StatusCode, refusal.Error, raw
}

// TestOptionalLimit_UnmarshalJSON verifies absent, null and a number are told
// apart, and a non-number is refused.
func TestOptionalLimit_UnmarshalJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		body      string
		wantSet   bool
		wantValue *int64
		wantErr   bool
	}{
		{name: "absent", body: `{}`},
		{name: "null clears", body: `{"max_file_bytes":null}`, wantSet: true},
		{name: "number sets", body: `{"max_file_bytes":42}`, wantSet: true, wantValue: new(int64(42))},
		{name: "string refused", body: `{"max_file_bytes":"42"}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var req updateRequest
			err := json.Unmarshal([]byte(tt.body), &req)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Unmarshal(%s) error = %v, want error %v", tt.body, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			got := req.MaxFileBytes
			if got.set != tt.wantSet || (got.value == nil) != (tt.wantValue == nil) ||
				(got.value != nil && *got.value != *tt.wantValue) {
				t.Errorf("Unmarshal(%s) = %+v, want set %v value %v", tt.body, got, tt.wantSet, tt.wantValue)
			}
		})
	}
}

// TestUpdateRequest_patch verifies a cleared and a set limit land in the right
// halves of the store's patch, and an absent one in neither.
func TestUpdateRequest_patch(t *testing.T) {
	t.Parallel()
	var req updateRequest
	if err := json.Unmarshal([]byte(`{"name":"N","max_file_bytes":null,"active":false}`), &req); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	patch := req.patch()
	if patch.Name == nil || *patch.Name != "N" || patch.Active == nil || *patch.Active ||
		!patch.ClearMaxFileBytes || patch.MaxFileBytes != nil ||
		patch.ClearMaxTotalBytes || patch.MaxTotalBytes != nil || patch.Description != nil {
		t.Errorf("patch = %+v", patch)
	}
	if err := json.Unmarshal([]byte(`{"max_total_bytes":7}`), &req); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if patch := req.patch(); patch.MaxTotalBytes == nil || *patch.MaxTotalBytes != 7 || patch.ClearMaxTotalBytes {
		t.Errorf("patch with a total limit = %+v", patch)
	}
}

// TestClassify verifies the store's sentinels map onto their status and code,
// the explained ones carry the rule, and anything else is a bare 500.
func TestClassify(t *testing.T) {
	t.Parallel()
	slugErr := project.ValidateSlug("Bad Slug")
	tests := []struct {
		err         error
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{fmt.Errorf("x: %w", project.ErrSlugTaken), http.StatusConflict, codeSlugTaken, ""},
		{slugErr, http.StatusUnprocessableEntity, codeInvalidSlug, strings.TrimPrefix(slugErr.Error(),
			"invalid project slug: ")},
		{project.ErrLastAdmin, http.StatusConflict, codeLastAdmin, ""},
		{project.ErrNotMember, http.StatusNotFound, codeNotMember, ""},
		{project.ErrUserNotFound, http.StatusUnprocessableEntity, codeUserNotFound, ""},
		{auth.ErrUserNotFound, http.StatusNotFound, codeNotFound, ""},
		{auth.ErrLastMaintainer, http.StatusConflict, codeLastMaintainer, ""},
		{errors.New("boom"), http.StatusInternalServerError, codeInternal, ""},
	}
	for _, tt := range tests {
		got := classify(tt.err)
		if got.status != tt.wantStatus || got.code != tt.wantCode || got.message != tt.wantMessage {
			t.Errorf("classify(%v) = %+v, want %d %q %q", tt.err, got, tt.wantStatus, tt.wantCode, tt.wantMessage)
		}
	}
	if !strings.Contains(classify(slugErr).message, "lowercase") {
		t.Errorf("the slug message %q does not state the rule", classify(slugErr).message)
	}
}

// TestHandlers_projects drives the project routes over the fakes: list, get,
// create (201 and a refusal), update (slug refused, limit cleared), members.
func TestHandlers_projects(t *testing.T) {
	t.Parallel()
	projects := &fakeProjects{overviews: []project.Overview{{
		Project: project.Project{UID: "pj1", Slug: "one", Name: "One"}, MembersCount: 3, PhotosCount: 9,
	}}}
	srv := newTestServer(t, projects, &fakeAccounts{})

	status, _, raw := call(t, srv, http.MethodGet, "/api/v1/projects", "")
	if status != http.StatusOK || !strings.Contains(string(raw), `"members_count":3`) ||
		!strings.HasPrefix(string(raw), "[") {
		t.Errorf("GET /projects = %d %s, want a bare array with counts", status, raw)
	}
	if status, code, _ := call(t, srv, http.MethodGet, "/api/v1/projects/pjx", ""); status != http.StatusNotFound ||
		code != codeNotFound {
		t.Errorf("GET unknown = %d %q", status, code)
	}

	status, _, raw = call(t, srv, http.MethodPost, "/api/v1/projects", `{"slug":"two","name":"Two"}`)
	if status != http.StatusCreated || !strings.Contains(string(raw), `"members_count":1`) {
		t.Errorf("POST /projects = %d %s", status, raw)
	}
	if status, code, _ := call(t, srv, http.MethodPost, "/api/v1/projects", `{"slug":"two"`); status !=
		http.StatusBadRequest || code != codeInvalidBody {
		t.Errorf("POST truncated body = %d %q", status, code)
	}

	if status, code, _ := call(t, srv, http.MethodPatch, "/api/v1/projects/pj1", `{"slug":null}`); status !=
		http.StatusUnprocessableEntity || code != codeSlugImmutable {
		t.Errorf("PATCH slug = %d %q, want 422 slug_immutable", status, code)
	}
	if status, _, _ := call(t, srv, http.MethodPatch, "/api/v1/projects/pj1", `{"max_total_bytes":null}`); status !=
		http.StatusOK || !projects.lastPatch.ClearMaxTotalBytes {
		t.Errorf("PATCH clear = %d, patch %+v", status, projects.lastPatch)
	}

	if status, code, _ := call(t, srv, http.MethodPost, "/api/v1/projects/pj1/members",
		`{"user_uid":"us1","role":"boss"}`); status != http.StatusUnprocessableEntity || code != codeInvalidRole {
		t.Errorf("POST member with a bad role = %d %q", status, code)
	}
	if status, code, _ := call(t, srv, http.MethodPost, "/api/v1/projects/pj1/members",
		`{"role":"viewer"}`); status != http.StatusBadRequest || code != codeInvalidBody {
		t.Errorf("POST member without a user = %d %q", status, code)
	}
	if status, _, _ := call(t, srv, http.MethodPost, "/api/v1/projects/pj1/members",
		`{"user_uid":"us1","role":"viewer"}`); status != http.StatusCreated {
		t.Errorf("POST member = %d", status)
	}
	if status, _, _ := call(t, srv, http.MethodDelete, "/api/v1/projects/pj1/members/us1", ""); status !=
		http.StatusNoContent {
		t.Errorf("DELETE member = %d", status)
	}

	projects.err = fmt.Errorf("wrapped: %w", project.ErrLastAdmin)
	if status, code, _ := call(t, srv, http.MethodDelete, "/api/v1/projects/pj1/members/us1", ""); status !=
		http.StatusConflict || code != codeLastAdmin {
		t.Errorf("DELETE last admin = %d %q", status, code)
	}
}

// TestHandlers_users drives the account routes over the fakes: the list joins
// the memberships, a promotion lands, and bad input is refused.
func TestHandlers_users(t *testing.T) {
	t.Parallel()
	projects := &fakeProjects{memberships: map[string][]project.MembershipSummary{
		"us1": {{ProjectUID: "pj1", Slug: "one", Name: "One", Role: project.RoleEditor, Approved: true}},
	}}
	accounts := &fakeAccounts{users: []auth.User{
		{UID: "us1", Username: "anna", Role: auth.RoleUser},
		{UID: "us2", Username: "bob", Role: auth.RoleUser, Disabled: true},
	}}
	srv := newTestServer(t, projects, accounts)

	status, _, raw := call(t, srv, http.MethodGet, "/api/v1/users", "")
	var list []userResponse
	if err := json.Unmarshal(raw, &list); err != nil || status != http.StatusOK || len(list) != 2 {
		t.Fatalf("GET /users = %d %s (%v)", status, raw, err)
	}
	if got := list[0]; len(got.Memberships) != 1 || got.Memberships[0].ProjectSlug != "one" ||
		got.Memberships[0].Role != project.RoleEditor {
		t.Errorf("anna = %+v", got)
	}
	if got := list[1]; !got.Disabled || got.Memberships == nil || len(got.Memberships) != 0 {
		t.Errorf("bob = %+v, want disabled with an empty membership list", got)
	}

	status, _, raw = call(t, srv, http.MethodPatch, "/api/v1/users/us1", `{"instance_role":"maintainer"}`)
	if status != http.StatusOK || !strings.Contains(string(raw), `"instance_role":"maintainer"`) {
		t.Errorf("promote = %d %s", status, raw)
	}
	if status, code, _ := call(t, srv, http.MethodPatch, "/api/v1/users/us1", `{"instance_role":"admin"}`); status !=
		http.StatusUnprocessableEntity || code != codeInvalidRole {
		t.Errorf("invalid role = %d %q", status, code)
	}
	if status, code, _ := call(t, srv, http.MethodPatch, "/api/v1/users/usx", `{"instance_role":"user"}`); status !=
		http.StatusNotFound || code != codeNotFound {
		t.Errorf("unknown account = %d %q", status, code)
	}
}
