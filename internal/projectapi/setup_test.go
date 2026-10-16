package projectapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/project"
)

// fakeSetup is a Setup whose err, when set, is what every call returns.
type fakeSetup struct {
	err        error
	lastParams project.CreateParams
	lastInput  auth.FirstAdminInput
	lastResend [2]string
}

// Create records its input and answers a created first admin, or f.err.
func (f *fakeSetup) Create(
	_ context.Context, _ audit.Meta, p project.CreateParams, in auth.FirstAdminInput,
) (*project.Project, auth.FirstAdmin, error) {
	f.lastParams, f.lastInput = p, in
	if f.err != nil {
		return nil, auth.FirstAdmin{}, f.err
	}
	return &project.Project{UID: "pjnew", Slug: p.Slug, Name: p.Name, Active: true},
		auth.FirstAdmin{UserUID: "usnew", Email: in.Email, Account: auth.FirstAdminCreated,
			SetPasswordURL: "/password-reset/t?project=" + p.Slug}, nil
}

// ResendLink records the pair and answers a link, or f.err.
func (f *fakeSetup) ResendLink(_ context.Context, _ audit.Meta, projectUID, userUID string) (auth.FirstAdmin, error) {
	f.lastResend = [2]string{projectUID, userUID}
	if f.err != nil {
		return auth.FirstAdmin{}, f.err
	}
	return auth.FirstAdmin{UserUID: userUID, Account: auth.FirstAdminExisting, SetPasswordURL: "/password-reset/u"}, nil
}

// AwaitingPassword answers one member, or f.err.
func (f *fakeSetup) AwaitingPassword(context.Context, string) ([]auth.AwaitingMember, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []auth.AwaitingMember{{UserUID: "usnew", Role: project.RoleAdmin}}, nil
}

// newSetupServer mounts an API over the fakes, the setup flow included.
func newSetupServer(t *testing.T, projects *fakeProjects, setup *fakeSetup) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{
		Projects: projects, Accounts: &fakeAccounts{}, Setup: setup,
		RequireMaintainer: func(next http.Handler) http.Handler { return next },
	})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// TestHandleCreate_withAdminGoesThroughSetup verifies a body with "admin" is
// handed to the setup flow — not the plain create, and with no creator — and
// that its answer carries first_admin beside the project.
func TestHandleCreate_withAdminGoesThroughSetup(t *testing.T) {
	t.Parallel()
	projects, setup := &fakeProjects{}, &fakeSetup{}
	srv := newSetupServer(t, projects, setup)

	status, code, raw := call(t, srv, http.MethodPost, "/api/v1/projects",
		`{"slug":"rodina","name":"Rodina","admin":{"email":"jana@example.com","display_name":"Jana","username":"j"}}`)
	if status != http.StatusCreated || code != "" {
		t.Fatalf("create = %d %q (%s), want 201", status, code, raw)
	}
	want := auth.FirstAdminInput{Email: "jana@example.com", DisplayName: "Jana", Username: "j"}
	if setup.lastInput != want || setup.lastParams.Slug != "rodina" || setup.lastParams.CreatorUID != "" {
		t.Errorf("setup got %+v / %+v, want %+v and no creator", setup.lastInput, setup.lastParams, want)
	}
	if projects.lastCreate.Slug != "" {
		t.Errorf("the plain create ran too: %+v", projects.lastCreate)
	}
	var got struct {
		Slug         string           `json:"slug"`
		MembersCount int              `json:"members_count"`
		FirstAdmin   *auth.FirstAdmin `json:"first_admin"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}
	if got.Slug != "rodina" || got.MembersCount != 1 || got.FirstAdmin == nil ||
		got.FirstAdmin.Account != auth.FirstAdminCreated {
		t.Errorf("answer = %+v, want the project with its created first admin", got)
	}
}

// TestHandleCreate_setupRefusals verifies the setup flow's errors map onto
// their codes.
func TestHandleCreate_setupRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{auth.ErrInvalidEmail, http.StatusUnprocessableEntity, codeInvalidEmail},
		{auth.ErrUsernameTooLong, http.StatusUnprocessableEntity, codeInvalidUser},
		{auth.ErrUsernameTaken, http.StatusConflict, codeUsernameTaken},
		{auth.ErrEmailAmbiguous, http.StatusConflict, codeEmailAmbiguous},
		{auth.ErrUserDisabled, http.StatusConflict, codeUserDisabled},
		{fmt.Errorf("%w: %q", project.ErrSlugTaken, "x"), http.StatusConflict, codeSlugTaken},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			t.Parallel()
			srv := newSetupServer(t, &fakeProjects{}, &fakeSetup{err: tt.err})
			status, code, _ := call(t, srv, http.MethodPost, "/api/v1/projects",
				`{"slug":"x","name":"X","admin":{"email":"a@example.com"}}`)
			if status != tt.status || code != tt.code {
				t.Errorf("create = %d %q, want %d %q", status, code, tt.status, tt.code)
			}
		})
	}
}

// TestHandleResendLink verifies the re-send names the project and member from
// the path, and its refusals map onto codes.
func TestHandleResendLink(t *testing.T) {
	t.Parallel()
	setup := &fakeSetup{}
	srv := newSetupServer(t, &fakeProjects{}, setup)
	status, _, raw := call(t, srv, http.MethodPost, "/api/v1/projects/pj1/members/us1/password-link", "")
	if status != http.StatusOK || setup.lastResend != [2]string{"pj1", "us1"} {
		t.Errorf("resend = %d (%s) for %v, want 200 for pj1/us1", status, raw, setup.lastResend)
	}

	for _, tt := range []struct {
		err    error
		status int
		code   string
	}{
		{auth.ErrPasswordAlreadySet, http.StatusConflict, codePasswordSet},
		{project.ErrNotMember, http.StatusNotFound, codeNotMember},
	} {
		srv := newSetupServer(t, &fakeProjects{}, &fakeSetup{err: tt.err})
		status, code, _ := call(t, srv, http.MethodPost, "/api/v1/projects/pj1/members/us1/password-link", "")
		if status != tt.status || code != tt.code {
			t.Errorf("resend with %v = %d %q, want %d %q", tt.err, status, code, tt.status, tt.code)
		}
	}
}

// TestHandleAwaitingPassword verifies the listing answers for a known project
// and 404 for an unknown one.
func TestHandleAwaitingPassword(t *testing.T) {
	t.Parallel()
	projects := &fakeProjects{overviews: []project.Overview{{Project: project.Project{UID: "pj1"}}}}
	srv := newSetupServer(t, projects, &fakeSetup{})
	status, _, raw := call(t, srv, http.MethodGet, "/api/v1/projects/pj1/awaiting-password", "")
	var list []auth.AwaitingMember
	if err := json.Unmarshal(raw, &list); err != nil || status != http.StatusOK || len(list) != 1 {
		t.Errorf("awaiting = %d %s (%v), want 200 with one member", status, raw, err)
	}
	status, code, _ := call(t, srv, http.MethodGet, "/api/v1/projects/nope/awaiting-password", "")
	if status != http.StatusNotFound || code != codeNotFound {
		t.Errorf("awaiting for an unknown project = %d %q, want 404 not_found", status, code)
	}
}
