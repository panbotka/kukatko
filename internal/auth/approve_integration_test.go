//go:build integration

package auth_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/mailer"
)

// waitingAccount registers one account through the public endpoint and returns
// it as the store holds it: created, unapproved, waiting for somebody to let it
// in. Going through the endpoint rather than seeding the row is the point — it
// is the state an approval actually meets in production.
func waitingAccount(t *testing.T, env *httpEnv, username string) auth.User {
	t.Helper()
	openRegistration(t, env)
	status, body := register(t, env, username, username+"@example.test", theSecret)
	if status != http.StatusCreated {
		t.Fatalf("registering %q = %d, body %s", username, status, body)
	}
	user, err := env.store.GetUserByUsername(t.Context(), username)
	if err != nil {
		t.Fatalf("reading the registered account: %v", err)
	}
	if user.ApprovedAt != nil {
		t.Fatalf("registered account is already approved at %v", *user.ApprovedAt)
	}
	return user
}

// approve posts one approval as client and returns the status and body.
func approve(t *testing.T, env *httpEnv, client *http.Client, uid string) (int, []byte) {
	t.Helper()
	return env.do(t, client, http.MethodPost, "/api/v1/admin/users/"+uid+"/approve", "")
}

// approvalMail returns the queued `account_approved` messages, oldest first.
func approvalMail(t *testing.T, env *httpEnv) []mailPayload {
	t.Helper()
	var out []mailPayload
	for _, m := range queuedMail(t, env) {
		if m.Template == mailer.TemplateAccountApproved {
			out = append(out, m)
		}
	}
	return out
}

// TestApprove_letsAWaitingAccountIn is the happy path: an administrator approves
// somebody who registered, the account comes back stamped and still on the role
// it registered with, the decision is in the audit trail, and the person has a
// message waiting on the queue that points them at the sign-in page.
func TestApprove_letsAWaitingAccountIn(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "boss", auth.RoleAdmin)
	admin := signInAs(t, env, "boss")
	waiting := waitingAccount(t, env, "newcomer")
	before := time.Now()

	status, body := approve(t, env, admin, waiting.UID)
	if status != http.StatusOK {
		t.Fatalf("POST approve = %d, body %s", status, body)
	}
	var approved struct {
		accountFlags
		Role auth.Role `json:"role"`
	}
	if err := json.Unmarshal(body, &approved); err != nil {
		t.Fatalf("decoding the approved account: %v", err)
	}
	if approved.ApprovedAt == nil {
		t.Fatal("the approved account still has approved_at = null")
	}
	if approved.ApprovedAt.Before(before.Add(-time.Minute)) {
		t.Errorf("approved_at = %v, want roughly now (%v)", *approved.ApprovedAt, before)
	}
	if approved.Disabled {
		t.Error("approving disabled the account")
	}
	// Approving is not promoting: the role the registration handed out stays.
	if approved.Role != auth.RoleViewer {
		t.Errorf("role after approval = %q, want %q", approved.Role, auth.RoleViewer)
	}

	stored, err := env.store.GetUserByUID(t.Context(), waiting.UID)
	if err != nil {
		t.Fatalf("re-reading the approved account: %v", err)
	}
	if stored.ApprovedAt == nil || !stored.ApprovedAt.Equal(*approved.ApprovedAt) {
		t.Errorf("stored approved_at = %v, want the answered %v", stored.ApprovedAt, *approved.ApprovedAt)
	}

	if n := countAudit(t, env, audit.ActionUserApprove); n != 1 {
		t.Errorf("%q audit entries = %d, want 1", audit.ActionUserApprove, n)
	}

	mails := approvalMail(t, env)
	if len(mails) != 1 {
		t.Fatalf("queued approval mails = %d, want 1", len(mails))
	}
	if mails[0].To != "newcomer@example.test" {
		t.Errorf("approval mail to %q, want the account's address", mails[0].To)
	}
	var data mailer.AccountApprovedData
	if err := json.Unmarshal(mails[0].Data, &data); err != nil {
		t.Fatalf("decoding the approval mail data: %v", err)
	}
	if data.SignInURL != testSignInURL {
		t.Errorf("approval mail sign-in URL = %q, want %q", data.SignInURL, testSignInURL)
	}
}

// TestApprove_twiceChangesNothing pins the idempotence an administrator's second
// click depends on: the same 200, the first stamp untouched, and neither a
// second audit entry nor a second mail promising somebody something twice.
func TestApprove_twiceChangesNothing(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "boss", auth.RoleAdmin)
	admin := signInAs(t, env, "boss")
	waiting := waitingAccount(t, env, "newcomer")

	status, body := approve(t, env, admin, waiting.UID)
	if status != http.StatusOK {
		t.Fatalf("first approve = %d, body %s", status, body)
	}
	var first accountFlags
	if err := json.Unmarshal(body, &first); err != nil {
		t.Fatalf("decoding the first approval: %v", err)
	}

	status, body = approve(t, env, admin, waiting.UID)
	if status != http.StatusOK {
		t.Fatalf("second approve = %d, body %s — approving twice must not fail", status, body)
	}
	var second accountFlags
	if err := json.Unmarshal(body, &second); err != nil {
		t.Fatalf("decoding the second approval: %v", err)
	}
	if second.ApprovedAt == nil || !second.ApprovedAt.Equal(*first.ApprovedAt) {
		t.Errorf("second approval moved approved_at to %v, want the first %v",
			second.ApprovedAt, *first.ApprovedAt)
	}
	if n := countAudit(t, env, audit.ActionUserApprove); n != 1 {
		t.Errorf("%q audit entries = %d, want 1 — a repeat click is not a decision",
			audit.ActionUserApprove, n)
	}
	if n := len(approvalMail(t, env)); n != 1 {
		t.Errorf("queued approval mails = %d, want 1", n)
	}
}

// TestApprove_blockedAccountIsRefused covers the state that must not be
// conflated with waiting: a blocked account is refused with 409 and nothing —
// stamp, trail, mail — moves, because unblocking is its own action.
func TestApprove_blockedAccountIsRefused(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "boss", auth.RoleAdmin)
	admin := signInAs(t, env, "boss")
	waiting := waitingAccount(t, env, "newcomer")
	if _, err := env.svc.SetUserDisabled(t.Context(), waiting.UID, true); err != nil {
		t.Fatalf("blocking the waiting account: %v", err)
	}

	status, body := approve(t, env, admin, waiting.UID)
	if status != http.StatusConflict {
		t.Fatalf("approving a blocked account = %d, want 409 (body %s)", status, body)
	}

	stored, err := env.store.GetUserByUID(t.Context(), waiting.UID)
	if err != nil {
		t.Fatalf("re-reading the blocked account: %v", err)
	}
	if stored.ApprovedAt != nil {
		t.Errorf("the refused approval stamped approved_at = %v", *stored.ApprovedAt)
	}
	if n := countAudit(t, env, audit.ActionUserApprove); n != 0 {
		t.Errorf("%q audit entries = %d, want 0", audit.ActionUserApprove, n)
	}
	if n := len(approvalMail(t, env)); n != 0 {
		t.Errorf("queued approval mails = %d, want 0", n)
	}
}

// TestApprove_auditEntryNamesBothParties reads the stored entry: the decision is
// attributed to the administrator who made it and points at the account it let
// in, which is what the trail is for.
func TestApprove_auditEntryNamesBothParties(t *testing.T) {
	env := newHTTPEnv(t, 10)
	boss := env.mustCreate(t, "boss", auth.RoleAdmin)
	admin := signInAs(t, env, "boss")
	waiting := waitingAccount(t, env, "newcomer")

	if status, body := approve(t, env, admin, waiting.UID); status != http.StatusOK {
		t.Fatalf("approve = %d, body %s", status, body)
	}

	var actorUID, targetType, targetUID string
	if err := env.db.Pool().QueryRow(t.Context(),
		`SELECT actor_uid, target_type, target_uid FROM audit_log WHERE action = $1`,
		audit.ActionUserApprove).Scan(&actorUID, &targetType, &targetUID); err != nil {
		t.Fatalf("reading the approval audit entry: %v", err)
	}
	if actorUID != boss.UID {
		t.Errorf("audit actor = %q, want the approving admin %q", actorUID, boss.UID)
	}
	if targetType != "users" {
		t.Errorf("audit target type = %q, want %q", targetType, "users")
	}
	if targetUID != waiting.UID {
		t.Errorf("audit target = %q, want the approved account %q", targetUID, waiting.UID)
	}
}

// TestApprove_maintainerBoundaryAndMissingAccount covers the two refusals that
// are not about approval at all: an admin may not touch a maintainer account
// through this endpoint any more than through the others, and a UID naming
// nobody is a 404.
func TestApprove_maintainerBoundaryAndMissingAccount(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "boss", auth.RoleAdmin)
	keeper := env.mustCreate(t, "keeper", auth.RoleMaintainer)
	admin := signInAs(t, env, "boss")

	if status, body := approve(t, env, admin, keeper.UID); status != http.StatusForbidden {
		t.Errorf("admin approving a maintainer = %d, want 403 (body %s)", status, body)
	}
	if status, body := approve(t, env, admin, "usr_nobody"); status != http.StatusNotFound {
		t.Errorf("approving an unknown account = %d, want 404 (body %s)", status, body)
	}
}

// TestApprove_isAdminOnly pins the guard on the route itself: a viewer holding a
// session cannot approve anybody.
func TestApprove_isAdminOnly(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "mia", auth.RoleViewer)
	viewer := signInAs(t, env, "mia")
	waiting := waitingAccount(t, env, "newcomer")

	if status, body := approve(t, env, viewer, waiting.UID); status != http.StatusForbidden {
		t.Errorf("viewer approving = %d, want 403 (body %s)", status, body)
	}
	if status, body := approve(t, env, newClient(t), waiting.UID); status != http.StatusUnauthorized {
		t.Errorf("anonymous approving = %d, want 401 (body %s)", status, body)
	}
}

// TestListUsers_pendingFilter covers the listing filter an administrator uses to
// find the accounts waiting for them: pending=true lists only those,
// pending=false only the ones already in, an absent parameter everybody, and a
// value that is not a boolean is refused rather than quietly ignored.
func TestListUsers_pendingFilter(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "boss", auth.RoleAdmin)
	admin := signInAs(t, env, "boss")
	waiting := waitingAccount(t, env, "newcomer")

	if got := listUsernames(t, env, admin, "?pending=true"); len(got) != 1 || got[0] != "newcomer" {
		t.Errorf("pending=true listed %v, want only the waiting account", got)
	}
	if got := listUsernames(t, env, admin, "?pending=false"); len(got) != 1 || got[0] != "boss" {
		t.Errorf("pending=false listed %v, want only the approved account", got)
	}
	if got := listUsernames(t, env, admin, ""); len(got) != 2 {
		t.Errorf("unfiltered listing = %v, want both accounts", got)
	}
	status, body := env.do(t, admin, http.MethodGet, "/api/v1/admin/users?pending=maybe", "")
	if status != http.StatusBadRequest {
		t.Errorf("pending=maybe = %d, want 400 (body %s)", status, body)
	}

	// And once approved, the account leaves the waiting list.
	if status, body = approve(t, env, admin, waiting.UID); status != http.StatusOK {
		t.Fatalf("approve = %d, body %s", status, body)
	}
	if got := listUsernames(t, env, admin, "?pending=true"); len(got) != 0 {
		t.Errorf("pending=true after the approval listed %v, want nothing", got)
	}
}

// listUsernames reads the admin roster with the given query string and returns
// the usernames in the order they were listed.
func listUsernames(t *testing.T, env *httpEnv, client *http.Client, query string) []string {
	t.Helper()
	status, raw := env.do(t, client, http.MethodGet, "/api/v1/admin/users"+query, "")
	if status != http.StatusOK {
		t.Fatalf("GET /admin/users%s = %d, body %s", query, status, raw)
	}
	var users []accountFlags
	if err := json.Unmarshal(raw, &users); err != nil {
		t.Fatalf("decoding the user list: %v", err)
	}
	names := make([]string, 0, len(users))
	for _, u := range users {
		names = append(names, u.Username)
	}
	return names
}
