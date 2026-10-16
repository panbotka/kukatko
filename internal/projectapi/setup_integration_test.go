//go:build integration

package projectapi_test

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/mailer"
)

// firstAdmin is the wire shape of a create's first_admin (and of a re-sent link).
type firstAdmin struct {
	UserUID        string     `json:"user_uid"`
	Username       string     `json:"username"`
	DisplayName    string     `json:"display_name"`
	Email          string     `json:"email"`
	Account        string     `json:"account"`
	MailSent       bool       `json:"mail_sent"`
	SetPasswordURL string     `json:"set_password_url"`
	ExpiresAt      *time.Time `json:"expires_at"`
}

// setupResult is the wire shape of POST /projects with a first admin.
type setupResult struct {
	overview
	FirstAdmin *firstAdmin `json:"first_admin"`
}

// mailJob is one queued mail_send job: its template and recipient.
type mailJob struct {
	Template, To string
}

// mailJobs returns every queued mail, oldest first.
func (e *env) mailJobs(t *testing.T) []mailJob {
	t.Helper()
	rows, err := e.db.Pool().Query(t.Context(),
		`SELECT payload->>'template', payload->>'to' FROM jobs WHERE type = 'mail_send' ORDER BY id`)
	if err != nil {
		t.Fatalf("reading mail jobs: %v", err)
	}
	defer rows.Close()
	var out []mailJob
	for rows.Next() {
		var j mailJob
		if err := rows.Scan(&j.Template, &j.To); err != nil {
			t.Fatalf("scanning mail job: %v", err)
		}
		out = append(out, j)
	}
	return out
}

// count runs a single-number query.
func (e *env) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.Pool().QueryRow(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// auditActionsFor returns, oldest first, the actions of every audit row whose
// target or details name uid (a project's or an account's).
func (e *env) auditActionsFor(t *testing.T, uids ...string) []string {
	t.Helper()
	rows, err := e.db.Pool().Query(t.Context(),
		`SELECT action FROM audit_log WHERE target_uid = ANY($1) OR details->>'project_uid' = ANY($1) ORDER BY id`,
		uids)
	if err != nil {
		t.Fatalf("reading audit rows: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			t.Fatalf("scanning audit row: %v", err)
		}
		out = append(out, action)
	}
	return out
}

// setup sets slug up for the admin JSON object as token, expecting 201.
func (e *env) setup(t *testing.T, token, slug, admin string) setupResult {
	t.Helper()
	var got setupResult
	e.mustDo(t, token, http.MethodPost, "/api/v1/projects",
		`{"slug":"`+slug+`","name":"Rodina `+slug+`","admin":`+admin+`}`, http.StatusCreated, &got)
	if got.FirstAdmin == nil {
		t.Fatalf("create with an admin answered no first_admin")
	}
	return got
}

// tokenOf is the token in a set-password link.
func tokenOf(t *testing.T, link string) string {
	t.Helper()
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parsing %q: %v", link, err)
	}
	return strings.TrimPrefix(parsed.Path, "/password-reset/")
}

// TestSetup_newAccount pins the main path: a new account with no password is
// the project's only member and admin, the caller is not a member, the link
// carries the project, the audit trail has all four entries, and the mail job
// uses the project-setup template. Following the link sets the first password.
func TestSetup_newAccount(t *testing.T) {
	e := newEnv(t)
	_, token := e.user(t, "root", auth.AccountMaintainer)

	got := e.setup(t, token, "rodina", `{"email":"jana.novakova@example.com","display_name":"Jana Nováková"}`)
	admin := got.FirstAdmin
	if admin.Account != auth.FirstAdminCreated || admin.Username != "jana.novakova" || !admin.MailSent {
		t.Fatalf("first admin = %+v, want a created jana.novakova with the mail sent", admin)
	}
	if got.MembersCount != 1 {
		t.Errorf("members_count = %d, want 1", got.MembersCount)
	}
	if !strings.HasPrefix(admin.SetPasswordURL, "https://k.example/password-reset/") ||
		!strings.HasSuffix(admin.SetPasswordURL, "?project=rodina") {
		t.Errorf("set_password_url = %q, want the reset page with ?project=rodina", admin.SetPasswordURL)
	}
	if admin.ExpiresAt == nil || time.Until(*admin.ExpiresAt) < 6*24*time.Hour {
		t.Errorf("expires_at = %v, want about seven days ahead", admin.ExpiresAt)
	}

	members := e.count(t, `SELECT count(*) FROM project_members WHERE project_uid = $1`, got.UID)
	admins := e.count(t, `SELECT count(*) FROM project_members WHERE project_uid = $1 AND user_uid = $2
		AND role = 'admin' AND approved_at IS NOT NULL`, got.UID, admin.UserUID)
	if members != 1 || admins != 1 {
		t.Errorf("members = %d, approved admin rows = %d, want 1 and 1 (the caller is no member)", members, admins)
	}
	account, err := e.authSvc.GetUser(t.Context(), admin.UserUID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if account.HasPassword() || account.Role != auth.RoleUser || account.Email != "jana.novakova@example.com" {
		t.Errorf("account = %+v, want a plain user without a password", account)
	}
	if _, _, err := e.authSvc.Login(t.Context(), "jana.novakova", ""); err == nil {
		t.Error("a passwordless account signed in with an empty password")
	}

	wantActions := []string{audit.ActionUserCreate, audit.ActionProjectMemberAdd,
		audit.ActionProjectCreate, audit.ActionUserPasswordReset}
	if actions := e.auditActionsFor(t, got.UID, admin.UserUID); !slices.Equal(actions, wantActions) {
		t.Errorf("audit actions = %v, want %v", actions, wantActions)
	}
	if jobs := e.mailJobs(t); !slices.Equal(jobs, []mailJob{{mailer.TemplateProjectSetup, admin.Email}}) {
		t.Errorf("mail jobs = %v, want one %s to %s", jobs, mailer.TemplateProjectSetup, admin.Email)
	}

	reset := auth.NewPasswordReset(auth.PasswordResetConfig{Service: e.authSvc})
	status, err := reset.Status(t.Context(), tokenOf(t, admin.SetPasswordURL))
	if err != nil || !status.Valid || !status.FirstPassword || status.DisplayName != "Jana Nováková" {
		t.Fatalf("Status = %+v, %v; want a valid first-password link greeting Jana", status, err)
	}
	if _, err := reset.Consume(t.Context(), tokenOf(t, admin.SetPasswordURL), testPassword,
		audit.Entry{Action: audit.ActionUserPasswordResetUse, TargetType: "users"}); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if _, _, err := e.authSvc.Login(t.Context(), "jana.novakova", testPassword); err != nil {
		t.Errorf("Login after setting the password: %v", err)
	}
}

// TestSetup_derivedUsernameIsUnique pins that a derived username already taken
// gets the next free number, and that a given one taken is refused.
func TestSetup_derivedUsernameIsUnique(t *testing.T) {
	e := newEnv(t)
	_, token := e.user(t, "root", auth.AccountMaintainer)
	e.user(t, "jana", auth.AccountViewer)

	got := e.setup(t, token, "rodina", `{"email":"jana@example.org"}`)
	if got.FirstAdmin.Username != "jana2" || got.FirstAdmin.Account != auth.FirstAdminCreated {
		t.Errorf("first admin = %+v, want a created jana2", got.FirstAdmin)
	}
	got = e.setup(t, token, "chata", `{"email":"pavel@example.org","username":"Pavel.N"}`)
	if got.FirstAdmin.Username != "pavel.n" {
		t.Errorf("username = %q, want the given one normalized", got.FirstAdmin.Username)
	}
}

// TestSetup_existingAccount pins that an address that already has an account
// adds that account (matched without regard to case), creates none, sends the
// announcement without a password link and says so.
func TestSetup_existingAccount(t *testing.T) {
	e := newEnv(t)
	_, token := e.user(t, "root", auth.AccountMaintainer)
	petr, _ := e.user(t, "petr", auth.AccountViewer)
	before := e.count(t, `SELECT count(*) FROM users`)

	got := e.setup(t, token, "rodina", `{"email":"petr@EXAMPLE.test","username":"ignored"}`)
	admin := got.FirstAdmin
	if admin.Account != auth.FirstAdminExisting || admin.UserUID != petr.UID || admin.SetPasswordURL != "" {
		t.Errorf("first admin = %+v, want the existing petr without a link", admin)
	}
	if after := e.count(t, `SELECT count(*) FROM users`); after != before {
		t.Errorf("accounts = %d, want still %d", after, before)
	}
	if jobs := e.mailJobs(t); !slices.Equal(jobs, []mailJob{{mailer.TemplateProjectAdminAdded, petr.Email}}) {
		t.Errorf("mail jobs = %v, want one %s", jobs, mailer.TemplateProjectAdminAdded)
	}
	if n := e.count(t, `SELECT count(*) FROM password_reset_tokens WHERE user_uid = $1`, petr.UID); n != 0 {
		t.Errorf("reset tokens = %d, want none for an account with a password", n)
	}
}

// TestSetup_mailOff pins that with mail off everything is still created, no
// mail is queued, and the administrator gets the link to pass on by hand.
func TestSetup_mailOff(t *testing.T) {
	e := newEnvMail(t, false)
	_, token := e.user(t, "root", auth.AccountMaintainer)

	got := e.setup(t, token, "rodina", `{"email":"jana@example.com"}`)
	if got.FirstAdmin.MailSent || got.FirstAdmin.SetPasswordURL == "" {
		t.Errorf("first admin = %+v, want no mail sent and the link answered", got.FirstAdmin)
	}
	if jobs := e.mailJobs(t); len(jobs) != 0 {
		t.Errorf("mail jobs = %v, want none with mail off", jobs)
	}
}

// TestSetup_failureRollsBack pins that every refusal leaves nothing behind: no
// project without an admin, no orphan account.
func TestSetup_failureRollsBack(t *testing.T) {
	e := newEnv(t)
	_, token := e.user(t, "root", auth.AccountMaintainer)
	e.createProject(t, token, "taken")
	e.user(t, "occupied", auth.AccountViewer)
	e.user(t, "twin1", auth.AccountViewer)
	e.user(t, "twin2", auth.AccountViewer)
	if _, err := e.db.Pool().Exec(t.Context(),
		`UPDATE users SET email = 'twins@example.com' WHERE username IN ('twin1', 'twin2')`); err != nil {
		t.Fatalf("sharing an address: %v", err)
	}
	accounts := e.count(t, `SELECT count(*) FROM users`)
	projects := e.count(t, `SELECT count(*) FROM projects`)

	tests := []struct {
		name, slug, admin string
		status            int
		code              string
	}{
		{"slug taken", "taken", `{"email":"new@example.com"}`, http.StatusConflict, "slug_taken"},
		{"invalid slug", "Bad Slug", `{"email":"new@example.com"}`, http.StatusUnprocessableEntity, "invalid_slug"},
		{"invalid email", "fresh", `{"email":"not-an-address"}`, http.StatusUnprocessableEntity, "invalid_email"},
		{"username taken", "fresh", `{"email":"new@example.com","username":"occupied"}`,
			http.StatusConflict, "username_taken"},
		{"shared address", "fresh", `{"email":"twins@example.com"}`, http.StatusConflict, "email_ambiguous"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e.expectError(t, token, http.MethodPost, "/api/v1/projects",
				`{"slug":"`+tt.slug+`","name":"N","admin":`+tt.admin+`}`, tt.status, tt.code)
		})
	}
	if got := e.count(t, `SELECT count(*) FROM users`); got != accounts {
		t.Errorf("accounts = %d, want still %d", got, accounts)
	}
	if got := e.count(t, `SELECT count(*) FROM projects`); got != projects {
		t.Errorf("projects = %d, want still %d", got, projects)
	}
	if jobs := e.mailJobs(t); len(jobs) != 0 {
		t.Errorf("mail jobs = %v, want none", jobs)
	}
}

// TestSetup_resendLink pins the detail page's re-send: the member is listed as
// awaiting a password, a fresh link replaces the old one and is mailed, and
// once the password is set the re-send is refused.
func TestSetup_resendLink(t *testing.T) {
	e := newEnv(t)
	_, token := e.user(t, "root", auth.AccountMaintainer)
	other, _ := e.user(t, "other", auth.AccountViewer)
	got := e.setup(t, token, "rodina", `{"email":"jana@example.com"}`)
	admin := got.FirstAdmin
	base := "/api/v1/projects/" + got.UID

	var waiting []auth.AwaitingMember
	e.mustDo(t, token, http.MethodGet, base+"/awaiting-password", "", http.StatusOK, &waiting)
	if len(waiting) != 1 || waiting[0].UserUID != admin.UserUID || waiting[0].LinkExpiresAt == nil {
		t.Fatalf("awaiting = %+v, want jana with a live link", waiting)
	}

	var resent firstAdmin
	e.mustDo(t, token, http.MethodPost, base+"/members/"+admin.UserUID+"/password-link", "", http.StatusOK, &resent)
	if resent.SetPasswordURL == "" || resent.SetPasswordURL == admin.SetPasswordURL || !resent.MailSent {
		t.Fatalf("resent = %+v, want a fresh mailed link", resent)
	}
	reset := auth.NewPasswordReset(auth.PasswordResetConfig{Service: e.authSvc})
	if status, _ := reset.Status(t.Context(), tokenOf(t, admin.SetPasswordURL)); status.Valid {
		t.Error("the superseded link still works")
	}
	if jobs := e.mailJobs(t); len(jobs) != 2 || jobs[1].Template != mailer.TemplateProjectSetup {
		t.Errorf("mail jobs = %v, want a second %s", jobs, mailer.TemplateProjectSetup)
	}
	if n := e.count(t, `SELECT count(*) FROM audit_log WHERE action = $1 AND target_uid = $2`,
		audit.ActionUserPasswordReset, admin.UserUID); n != 2 {
		t.Errorf("password_reset audit rows = %d, want 2", n)
	}

	e.expectError(t, token, http.MethodPost, base+"/members/"+other.UID+"/password-link", "",
		http.StatusNotFound, "not_member")
	if _, err := reset.Consume(t.Context(), tokenOf(t, resent.SetPasswordURL), testPassword,
		audit.Entry{Action: audit.ActionUserPasswordResetUse, TargetType: "users"}); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	e.expectError(t, token, http.MethodPost, base+"/members/"+admin.UserUID+"/password-link", "",
		http.StatusConflict, "password_set")
	e.mustDo(t, token, http.MethodGet, base+"/awaiting-password", "", http.StatusOK, &waiting)
	if len(waiting) != 0 {
		t.Errorf("awaiting after the password was set = %+v, want none", waiting)
	}
}

// TestSetup_withoutAdminIsUnchanged pins that a create without the admin
// section still makes the caller the admin and answers no first_admin.
func TestSetup_withoutAdminIsUnchanged(t *testing.T) {
	e := newEnv(t)
	root, token := e.user(t, "root", auth.AccountMaintainer)
	var got setupResult
	e.mustDo(t, token, http.MethodPost, "/api/v1/projects", `{"slug":"mine","name":"Mine"}`, http.StatusCreated, &got)
	if got.FirstAdmin != nil {
		t.Errorf("first_admin = %+v, want none", got.FirstAdmin)
	}
	if n := e.count(t, `SELECT count(*) FROM project_members WHERE project_uid = $1 AND user_uid = $2`,
		got.UID, root.UID); n != 1 {
		t.Errorf("creator memberships = %d, want 1", n)
	}
}

// TestSetup_disabledAccountRefused pins that an address belonging to a blocked
// account is refused rather than handing a project to an account nobody can use.
func TestSetup_disabledAccountRefused(t *testing.T) {
	e := newEnv(t)
	_, token := e.user(t, "root", auth.AccountMaintainer)
	blocked, _ := e.user(t, "blocked", auth.AccountViewer)
	if _, err := e.authSvc.SetUserDisabled(t.Context(), blocked.UID, true); err != nil {
		t.Fatalf("SetUserDisabled: %v", err)
	}
	e.expectError(t, token, http.MethodPost, "/api/v1/projects",
		`{"slug":"x","name":"X","admin":{"email":"`+blocked.Email+`"}}`, http.StatusConflict, "user_disabled")
}
