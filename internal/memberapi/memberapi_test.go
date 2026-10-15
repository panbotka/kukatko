package memberapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/project"
)

// fakeMembers is a Members whose answers are set per test; err, when set, is
// what every call returns.
type fakeMembers struct {
	members   []project.Member
	current   *project.Member
	outcome   project.AddOutcome
	withdrawn project.WithdrawOutcome
	err       error
	lastRole  project.Role
	hook      project.ApproveHook
	approved  bool
	removed   bool
}

// Members returns the canned list or f.err.
func (f *fakeMembers) Members(context.Context, string) ([]project.Member, error) {
	return f.members, f.err
}

// AddByIdentifier records the role and returns the canned outcome or f.err.
func (f *fakeMembers) AddByIdentifier(
	_ context.Context, _ audit.Meta, _, identifier string, role project.Role,
) (project.AddOutcome, error) {
	f.lastRole = role
	if f.err != nil {
		return "", f.err
	}
	if strings.TrimSpace(identifier) == "" {
		return "", project.ErrEmptyIdentifier
	}
	return f.outcome, nil
}

// WithdrawByIdentifier returns the canned withdrawal outcome or f.err.
func (f *fakeMembers) WithdrawByIdentifier(
	_ context.Context, _ audit.Meta, _, identifier string,
) (project.WithdrawOutcome, error) {
	if f.err != nil {
		return "", f.err
	}
	if strings.TrimSpace(identifier) == "" {
		return "", project.ErrEmptyIdentifier
	}
	return f.withdrawn, nil
}

// Membership returns f.current, an ordinary member when unset, or f.err.
func (f *fakeMembers) Membership(_ context.Context, projectUID, userUID string) (*project.Member, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.current != nil {
		return f.current, nil
	}
	return &project.Member{ProjectUID: projectUID, UserUID: userUID, Role: project.RoleViewer}, nil
}

// SetRole records the role and returns a member or f.err.
func (f *fakeMembers) SetRole(
	_ context.Context, _ audit.Meta, projectUID, userUID string, role project.Role,
) (*project.Member, error) {
	f.lastRole = role
	if f.err != nil {
		return nil, f.err
	}
	return &project.Member{ProjectUID: projectUID, UserUID: userUID, Role: role}, nil
}

// ApproveWith records the hook, runs it with a nil transaction and returns a
// member or f.err.
func (f *fakeMembers) ApproveWith(
	ctx context.Context, _ audit.Meta, projectUID, userUID string, hook project.ApproveHook,
) (*project.Member, error) {
	f.hook = hook
	f.approved = true
	if f.err != nil {
		return nil, f.err
	}
	member := &project.Member{ProjectUID: projectUID, UserUID: userUID, Role: project.RoleViewer}
	if hook != nil {
		if err := hook(ctx, nil, member); err != nil {
			return nil, err
		}
	}
	now := time.Now()
	member.ApprovedAt = &now
	return member, nil
}

// RemoveMember records the call and returns f.err.
func (f *fakeMembers) RemoveMember(context.Context, audit.Meta, string, string) error {
	f.removed = true
	return f.err
}

// fakeAccounts serves one account by uid.
type fakeAccounts struct{ user auth.User }

// GetUserByUID returns the canned account, or auth.ErrUserNotFound.
func (f fakeAccounts) GetUserByUID(_ context.Context, uid string) (auth.User, error) {
	if uid != f.user.UID {
		return auth.User{}, auth.ErrUserNotFound
	}
	return f.user, nil
}

// fakeMail records the mails it was asked to schedule.
type fakeMail struct{ sent []mailjob.Mail }

// Enqueue records m.
func (f *fakeMail) Enqueue(_ context.Context, _ jobs.Execer, m mailjob.Mail) error {
	f.sent = append(f.sent, m)
	return nil
}

// admit is a guard that lets every request through.
func admit(next http.Handler) http.Handler { return next }

// newTestServer mounts an API over members (and optional mail) behind a guard
// that admits everybody.
func newTestServer(t *testing.T, members Members, accounts Accounts, mail MailScheduler) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{
		Members: members, Accounts: accounts, Mail: mail, RequireAdmin: admit,
		ProjectURL: func(slug string) string { return "https://k.example/p/" + slug + "/" },
	})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// send issues method path with body (none when "") and returns the status and
// the answer.
func send(t *testing.T, srv *httptest.Server, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("building %s %s: %v", method, path, err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.String()
}

// TestHandleAdd_answersTheSameForEveryOutcome verifies the add's answer is
// byte-identical whatever the store says happened.
func TestHandleAdd_answersTheSameForEveryOutcome(t *testing.T) {
	t.Parallel()
	outcomes := []project.AddOutcome{project.OutcomeAdded, project.OutcomeAlreadyMember,
		project.OutcomeNoSuchAccount, project.OutcomeAmbiguous}
	answers := make([]string, 0, len(outcomes))
	for _, outcome := range outcomes {
		srv := newTestServer(t, &fakeMembers{outcome: outcome}, nil, nil)
		status, body := send(t, srv, http.MethodPost, "/members", `{"identifier":"jan","role":"viewer"}`)
		if status != http.StatusAccepted {
			t.Errorf("add with outcome %s = %d, want 202", outcome, status)
		}
		answers = append(answers, body)
	}
	for _, body := range answers {
		if body != `{"status":"accepted"}`+"\n" {
			t.Errorf("add answered %q, want the one neutral body", body)
		}
	}
}

// TestHandleAdd_refusesMalformedRequests verifies a bad body, a role outside
// the project ladder (maintainer included) and a blank identifier are refused
// before anything is looked up.
func TestHandleAdd_refusesMalformedRequests(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body string
		status     int
		code       string
	}{
		{"not json", `{`, http.StatusBadRequest, codeInvalidBody},
		{"unknown field", `{"identifier":"jan","role":"viewer","x":1}`, http.StatusBadRequest, codeInvalidBody},
		{"maintainer", `{"identifier":"jan","role":"maintainer"}`, http.StatusUnprocessableEntity, codeInvalidRole},
		{"no role", `{"identifier":"jan"}`, http.StatusUnprocessableEntity, codeInvalidRole},
		{"blank identifier", `{"identifier":"  ","role":"viewer"}`, http.StatusUnprocessableEntity,
			codeInvalidIdentifier},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := newTestServer(t, &fakeMembers{outcome: project.OutcomeAdded}, nil, nil)
			status, body := send(t, srv, http.MethodPost, "/members", tt.body)
			if status != tt.status || !strings.Contains(body, `"error":"`+tt.code+`"`) {
				t.Errorf("add %s = %d %s, want %d %s", tt.body, status, body, tt.status, tt.code)
			}
		})
	}
}

// TestHandleSetRole_mapsStoreErrors verifies the role change's refusals.
func TestHandleSetRole_mapsStoreErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"last admin", project.ErrLastAdmin, http.StatusConflict, codeLastAdmin},
		{"not member", project.ErrNotMember, http.StatusNotFound, codeNotMember},
		{"unexpected", errors.New("boom"), http.StatusInternalServerError, codeInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := newTestServer(t, &fakeMembers{err: fmt.Errorf("wrapped: %w", tt.err)}, nil, nil)
			status, body := send(t, srv, http.MethodPatch, "/members/us1", `{"role":"viewer"}`)
			if status != tt.status || !strings.Contains(body, `"error":"`+tt.code+`"`) {
				t.Errorf("role change = %d %s, want %d %s", status, body, tt.status, tt.code)
			}
		})
	}
	srv := newTestServer(t, &fakeMembers{}, nil, nil)
	if status, body := send(t, srv, http.MethodPatch, "/members/us1", `{"role":"owner"}`); status !=
		http.StatusUnprocessableEntity {
		t.Errorf("role change to an unknown role = %d %s, want 422", status, body)
	}
}

// TestHandleSetRole_refusesTheMaintainer verifies a system administrator's
// membership keeps its role: the change is refused before the store is asked,
// and the list names the account as a maintainer.
func TestHandleSetRole_refusesTheMaintainer(t *testing.T) {
	t.Parallel()
	maintainer := project.Member{UserUID: "us1", Role: project.RoleAdmin, Maintainer: true}
	fake := &fakeMembers{current: &maintainer, members: []project.Member{maintainer}}
	srv := newTestServer(t, fake, nil, nil)
	status, body := send(t, srv, http.MethodPatch, "/members/us1", `{"role":"viewer"}`)
	if status != http.StatusConflict || !strings.Contains(body, `"error":"maintainer"`) {
		t.Fatalf("demoting the maintainer = %d %s, want 409 maintainer", status, body)
	}
	if fake.lastRole != "" {
		t.Errorf("the store was asked to set %q", fake.lastRole)
	}
	if status, body := send(t, srv, http.MethodGet, "/members", ""); status != http.StatusOK ||
		!strings.Contains(body, `"maintainer":true`) {
		t.Errorf("list = %d %s, want the maintainer flagged", status, body)
	}
}

// TestHandleSetRole_returnsTheMember verifies a role change answers the member
// in the page's shape, without an e-mail address.
func TestHandleSetRole_returnsTheMember(t *testing.T) {
	t.Parallel()
	fake := &fakeMembers{}
	srv := newTestServer(t, fake, nil, nil)
	status, body := send(t, srv, http.MethodPatch, "/members/us1", `{"role":"curator"}`)
	if status != http.StatusOK || fake.lastRole != project.RoleCurator {
		t.Fatalf("role change = %d %s (role %s)", status, body, fake.lastRole)
	}
	for _, key := range []string{`"user_uid":"us1"`, `"role":"curator"`, `"has_picture":false`, `"approved_at":null`} {
		if !strings.Contains(body, key) {
			t.Errorf("member body %s lacks %s", body, key)
		}
	}
	if strings.Contains(body, "email") || strings.Contains(body, "project_uid") {
		t.Errorf("member body %s carries more than the page needs", body)
	}
}

// TestHandleApprove_schedulesTheMail verifies an approval schedules the mail
// to the account's address with the project's link, and an API without mail
// passes no hook at all.
func TestHandleApprove_schedulesTheMail(t *testing.T) {
	t.Parallel()
	mail := &fakeMail{}
	accounts := fakeAccounts{user: auth.User{UID: "us1", Email: "jan@example.test", DisplayName: "Jan"}}
	srv := newTestServer(t, &fakeMembers{}, accounts, mail)
	if status, body := send(t, srv, http.MethodPost, "/members/us1/approve", ""); status != http.StatusOK {
		t.Fatalf("approve = %d %s", status, body)
	}
	if len(mail.sent) != 1 || mail.sent[0].To != "jan@example.test" ||
		mail.sent[0].Template != mailer.TemplateAccountApproved {
		t.Fatalf("scheduled mail = %+v, want one approval mail to jan", mail.sent)
	}
	if data, ok := mail.sent[0].Data.(mailer.AccountApprovedData); !ok || data.DisplayName != "Jan" {
		t.Errorf("mail data = %#v", mail.sent[0].Data)
	}

	without := &fakeMembers{}
	srv = newTestServer(t, without, nil, nil)
	if status, _ := send(t, srv, http.MethodPost, "/members/us1/approve", ""); status != http.StatusOK ||
		without.hook != nil {
		t.Errorf("approve without mail = %d, hook %v; want 200 and no hook", status, without.hook != nil)
	}
}

// TestHandleApprove_failedMailFailsTheApproval verifies an account that cannot
// be read fails the approval rather than approving silently.
func TestHandleApprove_failedMailFailsTheApproval(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t, &fakeMembers{}, fakeAccounts{}, &fakeMail{})
	if status, body := send(t, srv, http.MethodPost, "/members/us9/approve", ""); status !=
		http.StatusInternalServerError {
		t.Errorf("approve of an unreadable account = %d %s, want 500", status, body)
	}
}

// TestHandleListAndRemove verifies the list's shape (an empty project is [])
// and that a removal answers 204, a last admin 409.
func TestHandleListAndRemove(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t, &fakeMembers{members: []project.Member{}}, nil, nil)
	if status, body := send(t, srv, http.MethodGet, "/members", ""); status != http.StatusOK || body != "[]\n" {
		t.Errorf("empty list = %d %q, want 200 []", status, body)
	}
	if status, _ := send(t, srv, http.MethodDelete, "/members/us1", ""); status != http.StatusNoContent {
		t.Errorf("remove = %d, want 204", status)
	}
	srv = newTestServer(t, &fakeMembers{err: project.ErrLastAdmin}, nil, nil)
	if status, body := send(t, srv, http.MethodDelete, "/members/us1", ""); status != http.StatusConflict ||
		!strings.Contains(body, codeLastAdmin) {
		t.Errorf("remove of the last admin = %d %s, want 409 last_admin", status, body)
	}
}

// TestHandleWithdraw_answersTheSameForEveryOutcome verifies the withdrawal's
// answer is the add's one neutral body whatever the store says happened, and
// that only a malformed request is refused.
func TestHandleWithdraw_answersTheSameForEveryOutcome(t *testing.T) {
	t.Parallel()
	for _, outcome := range []project.WithdrawOutcome{project.OutcomeWithdrawn, project.OutcomeNotWithheld,
		project.OutcomeWithdrawNoSuchAccount, project.OutcomeWithdrawAmbiguous} {
		srv := newTestServer(t, &fakeMembers{withdrawn: outcome}, nil, nil)
		status, body := send(t, srv, http.MethodPost, "/members/withdraw", `{"identifier":"jan"}`)
		if status != http.StatusAccepted || body != `{"status":"accepted"}`+"\n" {
			t.Errorf("withdraw with outcome %s = %d %q, want 202 and the one neutral body", outcome, status, body)
		}
	}
	tests := []struct {
		name, body string
		status     int
		code       string
	}{
		{"not json", `{`, http.StatusBadRequest, codeInvalidBody},
		{"a role", `{"identifier":"jan","role":"viewer"}`, http.StatusBadRequest, codeInvalidBody},
		{"blank identifier", `{"identifier":" "}`, http.StatusUnprocessableEntity, codeInvalidIdentifier},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := newTestServer(t, &fakeMembers{withdrawn: project.OutcomeWithdrawn}, nil, nil)
			status, body := send(t, srv, http.MethodPost, "/members/withdraw", tt.body)
			if status != tt.status || !strings.Contains(body, `"error":"`+tt.code+`"`) {
				t.Errorf("withdraw %s = %d %s, want %d %s", tt.body, status, body, tt.status, tt.code)
			}
		})
	}
}

// TestMemberRoutes_withheldIsNoMember verifies every route addressing a member
// by uid answers a membership whose identity is withheld exactly like no
// membership — 404 not_member — without asking the store to change anything,
// so an admin who knows the uid cannot learn that the address they typed into
// the blind add was that account's.
func TestMemberRoutes_withheldIsNoMember(t *testing.T) {
	t.Parallel()
	now := time.Now()
	withheld := project.Member{UserUID: "us1", Role: project.RoleViewer, ApprovedAt: &now, IdentityWithheld: true}
	tests := []struct{ method, path, body string }{
		{http.MethodPatch, "/members/us1", `{"role":"editor"}`},
		{http.MethodPost, "/members/us1/approve", ""},
		{http.MethodDelete, "/members/us1", ""},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			t.Parallel()
			fake := &fakeMembers{current: &withheld}
			srv := newTestServer(t, fake, nil, nil)
			status, body := send(t, srv, tt.method, tt.path, tt.body)
			absent := newTestServer(t, &fakeMembers{err: project.ErrNotMember}, nil, nil)
			wantStatus, wantBody := send(t, absent, tt.method, tt.path, tt.body)
			if status != http.StatusNotFound || status != wantStatus || body != wantBody {
				t.Errorf("%s %s = %d %q, want what no membership gets: %d %q",
					tt.method, tt.path, status, body, wantStatus, wantBody)
			}
			if fake.lastRole != "" || fake.approved || fake.removed {
				t.Errorf("the store was asked to change a withheld membership: %+v", fake)
			}
		})
	}
}

// TestRegisterRoutes_throttlesTheBlindRoutes verifies the configured throttle
// wraps the blind add and its withdrawal, and nothing else.
func TestRegisterRoutes_throttlesTheBlindRoutes(t *testing.T) {
	t.Parallel()
	var throttled []string
	api := NewAPI(Config{
		Members:      &fakeMembers{outcome: project.OutcomeAdded, withdrawn: project.OutcomeWithdrawn},
		RequireAdmin: admit,
		Throttle: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				throttled = append(throttled, r.Method+" "+r.URL.Path)
				w.WriteHeader(http.StatusTooManyRequests)
			})
		},
	})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	for _, path := range []string{"/members", "/members/withdraw"} {
		if status, _ := send(t, srv, http.MethodPost, path, `{"identifier":"jan","role":"viewer"}`); status !=
			http.StatusTooManyRequests {
			t.Errorf("POST %s = %d, want the throttle's 429", path, status)
		}
	}
	if status, _ := send(t, srv, http.MethodGet, "/members", ""); status != http.StatusOK {
		t.Errorf("GET /members = %d, want 200 unthrottled", status)
	}
	if want := []string{"POST /members", "POST /members/withdraw"}; strings.Join(throttled, ",") !=
		strings.Join(want, ",") {
		t.Errorf("throttled %v, want %v", throttled, want)
	}
}
