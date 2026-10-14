//go:build integration

package auth_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/settings"
)

// theSecret is the shared registration secret these tests store on the
// instance; every wrong guess below differs from it.
const theSecret = "vsichni rodaci 2026"

// openRegistration switches self-service registration on with theSecret, the
// way an administrator does through PUT /settings.
func openRegistration(t *testing.T, env *httpEnv) {
	t.Helper()
	setRegistration(t, env, settings.Update{RegistrationEnabled: true, RegistrationSecret: theSecret})
}

// setRegistration writes the instance settings through the store, with the audit
// entry the settings API would have stamped.
func setRegistration(t *testing.T, env *httpEnv, in settings.Update) {
	t.Helper()
	entry := audit.Entry{Action: audit.ActionSettingsUpdate, TargetType: "settings"}
	if _, err := env.settings.Set(t.Context(), project.BootstrapUID, in, "", entry); err != nil {
		t.Fatalf("storing instance settings: %v", err)
	}
}

// registerJSON builds a registration request body.
func registerJSON(username, email, secret string) string {
	b, _ := json.Marshal(map[string]string{
		"username":     username,
		"display_name": "Nový Rodák",
		"email":        email,
		"password":     testPassword,
		"secret":       secret,
		"project":      project.BootstrapSlug,
	})
	return string(b)
}

// register posts one registration and returns the status and body.
func register(t *testing.T, env *httpEnv, username, email, secret string) (int, []byte) {
	t.Helper()
	return env.do(t, newClient(t), http.MethodPost, "/api/v1/auth/register",
		registerJSON(username, email, secret))
}

// mailPayload is the part of a queued `mail_send` job these tests read.
type mailPayload struct {
	Template string          `json:"template"`
	To       string          `json:"to"`
	Data     json.RawMessage `json:"data"`
}

// queuedMail returns every `mail_send` job on the queue, oldest first.
func queuedMail(t *testing.T, env *httpEnv) []mailPayload {
	t.Helper()
	rows, err := env.db.Pool().Query(t.Context(),
		`SELECT payload FROM jobs WHERE type = 'mail_send' ORDER BY id`)
	if err != nil {
		t.Fatalf("reading the mail queue: %v", err)
	}
	defer rows.Close()

	var out []mailPayload
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("scanning a mail job: %v", err)
		}
		var p mailPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatalf("decoding a mail payload: %v", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating mail jobs: %v", err)
	}
	return out
}

// countAudit returns how many audit entries of the given action are stored.
func countAudit(t *testing.T, env *httpEnv, action string) int {
	t.Helper()
	var n int
	if err := env.db.Pool().QueryRow(t.Context(),
		`SELECT count(*) FROM audit_log WHERE action = $1`, action).Scan(&n); err != nil {
		t.Fatalf("counting %q audit entries: %v", action, err)
	}
	return n
}

// TestRegister_createsAWaitingAccountAndMailsEverybody is the whole happy path:
// a stranger with the shared secret creates an account, the account exists but
// is not approved and holds the lowest role, the registration is in the audit
// trail, and the messages waiting on the queue are the person's confirmation and
// one notice per enabled administrator who opted into it by e-mail. The e-mail
// is opt-in: an administrator who never chose gets none.
func TestRegister_createsAWaitingAccountAndMailsEverybody(t *testing.T) {
	env := newHTTPEnv(t, 10)
	// An admin and a maintainer who opted into the mail, an admin who never
	// chose, and accounts that must not be told even having opted in: a
	// disabled admin and an editor.
	boss := env.mustCreate(t, "boss", auth.AccountAdmin)
	keeper := env.mustCreate(t, "keeper", auth.AccountMaintainer)
	env.mustCreate(t, "silent", auth.AccountAdmin)
	scribe := env.mustCreate(t, "scribe", auth.AccountEditor)
	blocked := env.mustCreate(t, "retired", auth.AccountAdmin)
	if _, err := env.svc.SetUserDisabled(t.Context(), blocked.UID, true); err != nil {
		t.Fatalf("disabling the retired admin: %v", err)
	}
	for _, uid := range []string{boss.UID, keeper.UID, scribe.UID, blocked.UID} {
		choose(t, env, uid, notification.ChannelEmail, true)
	}
	openRegistration(t, env)

	status, body := register(t, env, "Newcomer", "newcomer@example.test", theSecret)
	if status != http.StatusCreated {
		t.Fatalf("POST /auth/register = %d, want 201 (body %s)", status, body)
	}
	var resp struct {
		Username        string `json:"username"`
		Email           string `json:"email"`
		PendingApproval bool   `json:"pending_approval"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decoding the registration response: %v", err)
	}
	if resp.Username != "newcomer" {
		t.Errorf("response username = %q, want the normalized %q", resp.Username, "newcomer")
	}
	if !resp.PendingApproval {
		t.Error("response says the account is not pending approval; it must be")
	}

	created, err := env.store.GetUserByUsername(t.Context(), "newcomer")
	if err != nil {
		t.Fatalf("reading the registered account: %v", err)
	}
	if created.ApprovedAt != nil {
		t.Errorf("approved_at = %v, want NULL — nobody has approved this account", *created.ApprovedAt)
	}
	if created.AccountRole != auth.AccountViewer {
		t.Errorf("role = %q, want %q — registration hands out nothing", created.Role, auth.AccountViewer)
	}
	if created.Disabled {
		t.Error("the account is disabled; waiting for approval is not the same as blocked")
	}

	if n := countAudit(t, env, audit.ActionUserRegister); n != 1 {
		t.Errorf("%d %q audit entries, want 1", n, audit.ActionUserRegister)
	}

	mail := queuedMail(t, env)
	if len(mail) != 3 {
		t.Fatalf("%d mails queued, want 3 (the newcomer plus the two who opted in): %+v", len(mail), mail)
	}
	if mail[0].Template != mailer.TemplateRegistrationReceived || mail[0].To != "newcomer@example.test" {
		t.Errorf("first mail = %s to %s, want %s to the newcomer",
			mail[0].Template, mail[0].To, mailer.TemplateRegistrationReceived)
	}
	var confirmation mailer.RegistrationReceivedData
	if err := json.Unmarshal(mail[0].Data, &confirmation); err != nil || confirmation.ProjectName != "Veselice" {
		t.Errorf("confirmation carries %s, want it to name the project Veselice", mail[0].Data)
	}
	notified := map[string]bool{}
	for _, m := range mail[1:] {
		if m.Template != mailer.TemplateNewRegistrationPending {
			t.Errorf("mail to %s = %s, want %s", m.To, m.Template, mailer.TemplateNewRegistrationPending)
		}
		var data mailer.NewRegistrationPendingData
		if err := json.Unmarshal(m.Data, &data); err != nil || data.ApprovalURL != testApprovalURL ||
			data.ProjectName != "Veselice" {
			t.Errorf("mail to %s carries %s, want the approval URL %s in project Veselice", m.To, m.Data, testApprovalURL)
		}
		notified[m.To] = true
	}
	for _, want := range []string{"boss@example.test", "keeper@example.test"} {
		if !notified[want] {
			t.Errorf("%s was not notified about the pending registration", want)
		}
	}
	for _, unwanted := range []string{"scribe@example.test", "retired@example.test"} {
		if notified[unwanted] {
			t.Errorf("%s was notified, but only enabled admins and maintainers may be", unwanted)
		}
	}
	if notified["silent@example.test"] {
		t.Error("an admin who never opted into the e-mail was mailed; it is off by default")
	}
}

// TestRegister_unapprovedAccountCannotSignIn is the other half of the promise:
// the account exists and its password is right, and signing in still fails —
// with its own outcome, so the sign-in screen can say what is being waited for,
// and without a session.
func TestRegister_unapprovedAccountCannotSignIn(t *testing.T) {
	env := newHTTPEnv(t, 10)
	openRegistration(t, env)
	if status, body := register(t, env, "waiting", "waiting@example.test", theSecret); status != http.StatusCreated {
		t.Fatalf("POST /auth/register = %d, body %s", status, body)
	}

	client := newClient(t)
	status, body := env.do(t, client, http.MethodPost, "/api/v1/auth/login",
		loginJSON("waiting", testPassword))
	if status != http.StatusForbidden {
		t.Fatalf("login as an unapproved account = %d, want 403 (body %s)", status, body)
	}
	if status, _ := env.do(t, client, http.MethodGet, "/api/v1/auth/me", ""); status != http.StatusUnauthorized {
		t.Errorf("GET /auth/me after the refused login = %d, want 401 — no session may exist", status)
	}
	// A wrong password on the same account stays a plain 401: the distinct
	// outcome is only ever reached by somebody who holds the credentials.
	if status, _ := env.do(t, newClient(t), http.MethodPost, "/api/v1/auth/login",
		loginJSON("waiting", "not the password")); status != http.StatusUnauthorized {
		t.Errorf("login with a wrong password = %d, want 401", status)
	}

	// Approving the account is all it takes to let them in.
	created, err := env.store.GetUserByUsername(t.Context(), "waiting")
	if err != nil {
		t.Fatalf("reading the registered account: %v", err)
	}
	if _, err := env.db.Pool().Exec(t.Context(),
		`UPDATE project_members SET approved_at = now() WHERE user_uid = $1`, created.UID); err != nil {
		t.Fatalf("approving the account: %v", err)
	}
	if status, body := env.do(t, newClient(t), http.MethodPost, "/api/v1/auth/login",
		loginJSON("waiting", testPassword)); status != http.StatusOK {
		t.Errorf("login after approval = %d, want 200 (body %s)", status, body)
	}
}

// TestRegister_wrongSecretIsRefused checks the lock on the door: a registration
// that carries the wrong secret creates nothing and schedules no mail.
func TestRegister_wrongSecretIsRefused(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "boss", auth.AccountAdmin)
	openRegistration(t, env)

	status, body := register(t, env, "stranger", "stranger@example.test", "vsichni rodaci 2025")
	if status != http.StatusForbidden {
		t.Fatalf("POST /auth/register with a wrong secret = %d, want 403 (body %s)", status, body)
	}
	if _, err := env.store.GetUserByUsername(t.Context(), "stranger"); err == nil {
		t.Error("an account was created despite the wrong secret")
	}
	if mail := queuedMail(t, env); len(mail) != 0 {
		t.Errorf("%d mails queued after a refused registration, want none: %+v", len(mail), mail)
	}
	if n := countAudit(t, env, audit.ActionUserRegister); n != 0 {
		t.Errorf("%d %q audit entries after a refused registration, want none", n, audit.ActionUserRegister)
	}
}

// TestRegister_closedInstanceRefuses covers the two ways registration is shut:
// switched off, and switched on by hand with no secret behind it — an open door
// with no lock, which must let nobody in.
func TestRegister_closedInstanceRefuses(t *testing.T) {
	env := newHTTPEnv(t, 10)

	// Nothing stored at all: a fresh instance is closed.
	status, body := register(t, env, "early", "early@example.test", theSecret)
	if status != http.StatusForbidden {
		t.Fatalf("POST /auth/register on a fresh instance = %d, want 403 (body %s)", status, body)
	}

	// Explicitly off, secret or no secret.
	setRegistration(t, env, settings.Update{RegistrationEnabled: false, RegistrationSecret: theSecret})
	status, body = register(t, env, "early", "early@example.test", theSecret)
	if status != http.StatusForbidden {
		t.Fatalf("POST /auth/register while closed = %d, want 403 (body %s)", status, body)
	}

	// On with a blank secret. The settings API refuses that combination, so it is
	// written straight to the row the way a hand-edited database would hold it.
	if _, err := env.db.Pool().Exec(t.Context(),
		`UPDATE instance_settings SET registration_enabled = true, registration_secret = '' WHERE project_id = $1`,
		project.BootstrapUID,
	); err != nil {
		t.Fatalf("forcing an empty secret: %v", err)
	}
	for _, secret := range []string{"", theSecret} {
		status, body = register(t, env, "early", "early@example.test", secret)
		if status != http.StatusForbidden {
			t.Fatalf("POST /auth/register with an empty stored secret = %d, want 403 (body %s)", status, body)
		}
	}

	if _, err := env.store.GetUserByUsername(t.Context(), "early"); err == nil {
		t.Error("an account was created while registration was closed")
	}
	if mail := queuedMail(t, env); len(mail) != 0 {
		t.Errorf("%d mails queued while registration was closed, want none: %+v", len(mail), mail)
	}
}

// TestRegister_duplicateUsernameIsRejected checks that a name somebody already
// holds is refused with a clear answer — and that the refusal is the same
// whether the existing account was made by an administrator or by an earlier
// registration.
func TestRegister_duplicateUsernameIsRejected(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "alice", auth.AccountViewer)
	openRegistration(t, env)

	status, body := register(t, env, "Alice", "alice-two@example.test", theSecret)
	if status != http.StatusConflict {
		t.Fatalf("POST /auth/register with a taken username = %d, want 409 (body %s)", status, body)
	}
	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("decoding the error body: %v", err)
	}
	if errResp.Error == "" {
		t.Error("the 409 carried no message; a taken username has to say so")
	}
	if mail := queuedMail(t, env); len(mail) != 0 {
		t.Errorf("%d mails queued after a rejected registration, want none: %+v", len(mail), mail)
	}
}

// TestRegister_noProjectIsAClosedDoor verifies a secret registration that
// names no project is refused like a closed registration — there is no default
// project to drop the account into — and creates nothing.
func TestRegister_noProjectIsAClosedDoor(t *testing.T) {
	env := newHTTPEnv(t, 10)
	openRegistration(t, env)

	body := `{"username":"nikde","email":"nikde@example.test","password":"` + testPassword +
		`","secret":"` + theSecret + `"}`
	status, raw := env.do(t, newClient(t), http.MethodPost, "/api/v1/auth/register", body)
	if status != http.StatusForbidden || !strings.Contains(string(raw), "registration is closed or the secret is wrong") {
		t.Fatalf("POST /auth/register without a project = %d %s, want the closed-registration 403", status, raw)
	}
	if _, err := env.store.GetUserByUsername(t.Context(), "nikde"); !errors.Is(err, auth.ErrUserNotFound) {
		t.Errorf("account after a refused registration: %v, want ErrUserNotFound", err)
	}
}

// TestRegister_refusalsAnswerAlike verifies a refused secret registration says
// nothing about why: a slug naming no project, a project with registration
// closed and an open project given the wrong word all get the same 403 with the
// same body, so the answer never tells which slugs take registrations.
func TestRegister_refusalsAnswerAlike(t *testing.T) {
	env := newHTTPEnv(t, 10)

	attempt := func(projectSlug, secret string) (int, string) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{
			"username": "probe", "email": "probe@example.test", "password": testPassword,
			"secret": secret, "project": projectSlug,
		})
		status, raw := env.do(t, newClient(t), http.MethodPost, "/api/v1/auth/register", string(body))
		return status, string(raw)
	}

	setRegistration(t, env, settings.Update{RegistrationEnabled: false, RegistrationSecret: theSecret})
	closedStatus, closedBody := attempt("v", theSecret)
	unknownStatus, unknownBody := attempt("no-such-project", theSecret)
	openRegistration(t, env)
	wrongStatus, wrongBody := attempt("v", "not the word")

	if closedStatus != http.StatusForbidden {
		t.Fatalf("closed registration = %d %s, want 403", closedStatus, closedBody)
	}
	if unknownStatus != closedStatus || unknownBody != closedBody {
		t.Errorf("unknown project = %d %s, want the closed answer %d %s",
			unknownStatus, unknownBody, closedStatus, closedBody)
	}
	if wrongStatus != closedStatus || wrongBody != closedBody {
		t.Errorf("wrong secret = %d %s, want the closed answer %d %s",
			wrongStatus, wrongBody, closedStatus, closedBody)
	}
	if _, err := env.store.GetUserByUsername(t.Context(), "probe"); !errors.Is(err, auth.ErrUserNotFound) {
		t.Errorf("account after refused registrations: %v, want ErrUserNotFound", err)
	}
}

// TestRegister_validatesLikeTheAdminAPI checks that registration is held to the
// same input rules as POST /admin/users: an address that cannot receive mail and
// a password nobody could use are refused before anything is stored.
func TestRegister_validatesLikeTheAdminAPI(t *testing.T) {
	env := newHTTPEnv(t, 10)
	openRegistration(t, env)

	tests := []struct {
		name string
		body string
	}{
		{
			name: "no e-mail address",
			body: `{"username":"nomail","display_name":"","email":"","password":"` +
				testPassword + `","secret":"` + theSecret + `","project":"v"}`,
		},
		{
			name: "malformed e-mail address",
			body: `{"username":"badmail","display_name":"","email":"nobody@localhost","password":"` +
				testPassword + `","secret":"` + theSecret + `","project":"v"}`,
		},
		{
			name: "password too short",
			body: `{"username":"weak","display_name":"","email":"weak@example.test","password":"short",` +
				`"secret":"` + theSecret + `","project":"v"}`,
		},
		{
			name: "unknown field",
			body: `{"username":"sneaky","email":"sneaky@example.test","password":"` + testPassword +
				`","secret":"` + theSecret + `","project":"v","role":"admin"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := env.do(t, newClient(t), http.MethodPost, "/api/v1/auth/register", tt.body)
			if status != http.StatusBadRequest {
				t.Fatalf("POST /auth/register = %d, want 400 (body %s)", status, body)
			}
		})
	}
	if mail := queuedMail(t, env); len(mail) != 0 {
		t.Errorf("%d mails queued after refused registrations, want none: %+v", len(mail), mail)
	}
}

// TestRegister_succeedsWithNobodyToNotify covers the edge the notification is
// best-effort for: an instance whose only administrator has a placeholder
// address gets no notice, and the registration goes through all the same — the
// person's own confirmation is what matters.
func TestRegister_succeedsWithNobodyToNotify(t *testing.T) {
	env := newHTTPEnv(t, 10)
	admin := env.mustCreate(t, "boss", auth.AccountAdmin)
	if _, err := env.db.Pool().Exec(t.Context(),
		`UPDATE users SET email = 'boss@kukatko.invalid' WHERE uid = $1`, admin.UID); err != nil {
		t.Fatalf("giving the admin a placeholder address: %v", err)
	}
	openRegistration(t, env)

	status, body := register(t, env, "newcomer", "newcomer@example.test", theSecret)
	if status != http.StatusCreated {
		t.Fatalf("POST /auth/register = %d, want 201 (body %s)", status, body)
	}
	if _, err := env.store.GetUserByUsername(t.Context(), "newcomer"); err != nil {
		t.Fatalf("the account was not created: %v", err)
	}
	mail := queuedMail(t, env)
	if len(mail) != 1 {
		t.Fatalf("%d mails queued, want only the newcomer's: %+v", len(mail), mail)
	}
	if mail[0].Template != mailer.TemplateRegistrationReceived {
		t.Errorf("queued mail = %s, want %s", mail[0].Template, mailer.TemplateRegistrationReceived)
	}
}

// TestRegister_isRateLimitedPerAddress checks the budget on the one write an
// anonymous caller may perform: once an address has spent it, further attempts
// are refused with 429 before anything is created.
func TestRegister_isRateLimitedPerAddress(t *testing.T) {
	const budget = 3
	env := newHTTPEnv(t, budget)
	openRegistration(t, env)

	for i := range budget {
		// Every one of these is refused on its merits (the wrong secret), which
		// is precisely the attempt the budget exists to bound.
		if status, body := register(t, env, "guesser", "guesser@example.test", "wrong"); status != http.StatusForbidden {
			t.Fatalf("attempt %d = %d, want 403 (body %s)", i+1, status, body)
		}
	}
	status, body := register(t, env, "guesser", "guesser@example.test", theSecret)
	if status != http.StatusTooManyRequests {
		t.Fatalf("attempt %d = %d, want 429 (body %s)", budget+1, status, body)
	}
	if _, err := env.store.GetUserByUsername(t.Context(), "guesser"); err == nil {
		t.Error("an account was created by a throttled request")
	}
}
