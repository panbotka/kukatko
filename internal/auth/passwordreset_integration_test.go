//go:build integration

package auth_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/mailer"
)

// issuedReset is the administrator's half of a reset: the link to pass on, when
// it dies and where it was mailed.
type issuedReset struct {
	ResetURL  string    `json:"reset_url"`
	ExpiresAt time.Time `json:"expires_at"`
	Email     string    `json:"email"`
}

// issueReset starts a reset for uid as client and returns the status and body.
func issueReset(t *testing.T, env *httpEnv, client *http.Client, uid string) (int, []byte) {
	t.Helper()
	return env.do(t, client, http.MethodPost, "/api/v1/admin/users/"+uid+"/password-reset", "")
}

// mustIssueReset starts a reset that has to succeed and returns what the
// administrator was handed.
func mustIssueReset(t *testing.T, env *httpEnv, client *http.Client, uid string) issuedReset {
	t.Helper()
	status, body := issueReset(t, env, client, uid)
	if status != http.StatusOK {
		t.Fatalf("POST password-reset = %d, body %s", status, body)
	}
	var issued issuedReset
	if err := json.Unmarshal(body, &issued); err != nil {
		t.Fatalf("decoding the issued reset: %v", err)
	}
	return issued
}

// resetToken extracts the token from a reset link, which is its last path
// segment. It is the only place the plaintext token exists outside the mail.
func resetToken(t *testing.T, link string) string {
	t.Helper()
	if !strings.HasPrefix(link, testResetLinkBase+"/") {
		t.Fatalf("reset link %q does not start with %q", link, testResetLinkBase)
	}
	token := strings.TrimPrefix(link, testResetLinkBase+"/")
	if token == "" {
		t.Fatal("the reset link carries no token")
	}
	return token
}

// resetStatus reads the public status of a link.
func resetStatus(t *testing.T, env *httpEnv, token string) auth.PasswordResetStatus {
	t.Helper()
	status, body := env.do(t, newClient(t), http.MethodGet, "/api/v1/auth/password-reset/"+token, "")
	if status != http.StatusOK {
		t.Fatalf("GET password-reset = %d, body %s", status, body)
	}
	var out auth.PasswordResetStatus
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decoding the reset status: %v", err)
	}
	return out
}

// consumeReset posts a new password behind a link and returns status and body.
func consumeReset(t *testing.T, env *httpEnv, token, password string) (int, []byte) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"password": password})
	return env.do(t, newClient(t), http.MethodPost, "/api/v1/auth/password-reset/"+token, string(body))
}

// resetMail returns the queued `password_reset` messages, oldest first.
func resetMail(t *testing.T, env *httpEnv) []mailPayload {
	t.Helper()
	var out []mailPayload
	for _, m := range queuedMail(t, env) {
		if m.Template == mailer.TemplatePasswordReset {
			out = append(out, m)
		}
	}
	return out
}

// countResetTokens returns how many reset rows the account holds.
func countResetTokens(t *testing.T, env *httpEnv, uid string) int {
	t.Helper()
	var n int
	if err := env.db.Pool().QueryRow(t.Context(),
		`SELECT count(*) FROM password_reset_tokens WHERE user_uid = $1`, uid).Scan(&n); err != nil {
		t.Fatalf("counting reset tokens: %v", err)
	}
	return n
}

// expireResetTokens ages every link of the account past its expiry, which is the
// only way to reach the expired branch without waiting a week.
func expireResetTokens(t *testing.T, env *httpEnv, uid string) {
	t.Helper()
	if _, err := env.db.Pool().Exec(t.Context(),
		`UPDATE password_reset_tokens SET expires_at = now() - interval '1 hour' WHERE user_uid = $1`,
		uid); err != nil {
		t.Fatalf("expiring the reset tokens: %v", err)
	}
}

// canSignIn reports whether username/password is accepted right now.
func canSignIn(t *testing.T, env *httpEnv, username, password string) bool {
	t.Helper()
	status, _ := env.do(t, newClient(t), http.MethodPost, "/api/v1/auth/login",
		loginJSON(username, password))
	return status == http.StatusOK
}

// theNewPassword is what every test below sets through a link.
const theNewPassword = "a-brand-new-password"

// TestPasswordReset_issuesALinkAndMailsIt is the administrator's half of the
// happy path: the link comes back in the response, the same link is waiting on
// the mail queue, the decision is in the audit trail, and the database holds a
// hash rather than the link itself.
func TestPasswordReset_issuesALinkAndMailsIt(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "boss", auth.RoleAdmin)
	forgetful := env.mustCreate(t, "forgetful", auth.RoleViewer)
	admin := signInAs(t, env, "boss")
	before := time.Now()

	issued := mustIssueReset(t, env, admin, forgetful.UID)
	token := resetToken(t, issued.ResetURL)
	if issued.Email != "forgetful@example.test" {
		t.Errorf("issued.Email = %q, want the account's address", issued.Email)
	}
	if want := before.Add(auth.PasswordResetTTL); issued.ExpiresAt.Before(want.Add(-time.Minute)) {
		t.Errorf("expires_at = %v, want roughly %v (seven days)", issued.ExpiresAt, want)
	}

	mails := resetMail(t, env)
	if len(mails) != 1 {
		t.Fatalf("queued reset mails = %d, want 1", len(mails))
	}
	if mails[0].To != "forgetful@example.test" {
		t.Errorf("reset mail to %q, want the account's address", mails[0].To)
	}
	var data mailer.PasswordResetData
	if err := json.Unmarshal(mails[0].Data, &data); err != nil {
		t.Fatalf("decoding the reset mail data: %v", err)
	}
	if data.ResetURL != issued.ResetURL {
		t.Errorf("mailed link %q, answered %q — they must be the same link", data.ResetURL, issued.ResetURL)
	}
	if data.ValidFor != auth.PasswordResetTTL {
		t.Errorf("mailed validity = %v, want %v", data.ValidFor, auth.PasswordResetTTL)
	}

	if n := countAudit(t, env, audit.ActionUserPasswordReset); n != 1 {
		t.Errorf("%q audit entries = %d, want 1", audit.ActionUserPasswordReset, n)
	}

	var stored string
	if err := env.db.Pool().QueryRow(t.Context(),
		`SELECT token_hash FROM password_reset_tokens WHERE user_uid = $1`,
		forgetful.UID).Scan(&stored); err != nil {
		t.Fatalf("reading the stored token: %v", err)
	}
	if stored == token || strings.Contains(stored, token) {
		t.Error("the table stores the token itself, not a hash of it")
	}
}

// TestPasswordReset_setsThePasswordAndBurnsTheLink is the other half: the person
// behind the link sees it is good, chooses a password, and the link is spent —
// the same request a second time fails and the old password is gone.
func TestPasswordReset_setsThePasswordAndBurnsTheLink(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "boss", auth.RoleAdmin)
	forgetful := env.mustCreate(t, "forgetful", auth.RoleViewer)
	admin := signInAs(t, env, "boss")

	issued := mustIssueReset(t, env, admin, forgetful.UID)
	token := resetToken(t, issued.ResetURL)

	status := resetStatus(t, env, token)
	if !status.Valid {
		t.Fatal("a fresh link reports itself invalid")
	}
	if status.ExpiresAt == nil {
		t.Error("a valid link answers no expiry")
	}

	if code, body := consumeReset(t, env, token, theNewPassword); code != http.StatusNoContent {
		t.Fatalf("POST password-reset = %d, body %s", code, body)
	}
	if !canSignIn(t, env, "forgetful", theNewPassword) {
		t.Error("the new password does not sign in")
	}
	if canSignIn(t, env, "forgetful", testPassword) {
		t.Error("the old password still signs in")
	}

	// Second use: the link is gone, and it cannot set yet another password.
	if got := resetStatus(t, env, token); got.Valid {
		t.Error("a spent link still reports itself valid")
	}
	code, _ := consumeReset(t, env, token, "another-password-entirely")
	if code != http.StatusNotFound {
		t.Errorf("reusing the link = %d, want 404", code)
	}
	if canSignIn(t, env, "forgetful", "another-password-entirely") {
		t.Error("the second use set a password anyway")
	}

	if n := countAudit(t, env, audit.ActionUserPasswordResetUse); n != 1 {
		t.Errorf("%q audit entries = %d, want 1", audit.ActionUserPasswordResetUse, n)
	}
}

// TestPasswordReset_invalidatesEverySession pins the point of a reset: whoever
// was signed in as that account — including somebody signed in right now — is
// signed out by it.
func TestPasswordReset_invalidatesEverySession(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "boss", auth.RoleAdmin)
	forgetful := env.mustCreate(t, "forgetful", auth.RoleViewer)
	admin := signInAs(t, env, "boss")
	first := signInAs(t, env, "forgetful")
	second := signInAs(t, env, "forgetful")

	issued := mustIssueReset(t, env, admin, forgetful.UID)
	if code, body := consumeReset(t, env, resetToken(t, issued.ResetURL), theNewPassword); code != http.StatusNoContent {
		t.Fatalf("POST password-reset = %d, body %s", code, body)
	}

	for name, client := range map[string]*http.Client{"first": first, "second": second} {
		if code, _ := env.do(t, client, http.MethodGet, "/api/v1/auth/me", ""); code != http.StatusUnauthorized {
			t.Errorf("the %s session answers %d after the reset, want 401", name, code)
		}
	}
	// The administrator who issued the link keeps their own session.
	if code, _ := env.do(t, admin, http.MethodGet, "/api/v1/auth/me", ""); code != http.StatusOK {
		t.Errorf("the administrator's session answers %d, want 200", code)
	}
}

// TestPasswordReset_expiredLinkIsRefused covers the seven-day limit: an aged link
// reports itself unusable and sets nothing.
func TestPasswordReset_expiredLinkIsRefused(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "boss", auth.RoleAdmin)
	forgetful := env.mustCreate(t, "forgetful", auth.RoleViewer)
	admin := signInAs(t, env, "boss")

	issued := mustIssueReset(t, env, admin, forgetful.UID)
	token := resetToken(t, issued.ResetURL)
	expireResetTokens(t, env, forgetful.UID)

	if got := resetStatus(t, env, token); got.Valid {
		t.Error("an expired link reports itself valid")
	}
	if code, _ := consumeReset(t, env, token, theNewPassword); code != http.StatusNotFound {
		t.Errorf("using an expired link = %d, want 404", code)
	}
	if !canSignIn(t, env, "forgetful", testPassword) {
		t.Error("an expired link changed the password anyway")
	}

	// The cleanup that prunes expired sessions takes the row with it.
	if _, err := env.svc.CleanupFinishedPasswordResets(t.Context()); err != nil {
		t.Fatalf("CleanupFinishedPasswordResets: %v", err)
	}
	if n := countResetTokens(t, env, forgetful.UID); n != 0 {
		t.Errorf("reset rows after cleanup = %d, want 0", n)
	}
}

// TestPasswordReset_supersededLinkIsRefused pins that only the most recent link
// works: issuing a second one kills the first, and exactly one row is left.
func TestPasswordReset_supersededLinkIsRefused(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "boss", auth.RoleAdmin)
	forgetful := env.mustCreate(t, "forgetful", auth.RoleViewer)
	admin := signInAs(t, env, "boss")

	first := resetToken(t, mustIssueReset(t, env, admin, forgetful.UID).ResetURL)
	second := resetToken(t, mustIssueReset(t, env, admin, forgetful.UID).ResetURL)
	if first == second {
		t.Fatal("the second reset handed out the same token")
	}
	if n := countResetTokens(t, env, forgetful.UID); n != 1 {
		t.Errorf("outstanding reset rows = %d, want 1", n)
	}

	if got := resetStatus(t, env, first); got.Valid {
		t.Error("the superseded link reports itself valid")
	}
	if code, _ := consumeReset(t, env, first, theNewPassword); code != http.StatusNotFound {
		t.Errorf("using the superseded link = %d, want 404", code)
	}
	if code, body := consumeReset(t, env, second, theNewPassword); code != http.StatusNoContent {
		t.Fatalf("using the newest link = %d, body %s", code, body)
	}
	if !canSignIn(t, env, "forgetful", theNewPassword) {
		t.Error("the newest link did not set the password")
	}
}

// TestPasswordReset_blockedAccountIsRefused covers both directions of the rule:
// a blocked account gets no link, and a link that was issued before the block
// stops working.
func TestPasswordReset_blockedAccountIsRefused(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "boss", auth.RoleAdmin)
	forgetful := env.mustCreate(t, "forgetful", auth.RoleViewer)
	admin := signInAs(t, env, "boss")

	issued := mustIssueReset(t, env, admin, forgetful.UID)
	token := resetToken(t, issued.ResetURL)
	if code, body := env.do(t, admin, http.MethodPost,
		"/api/v1/admin/users/"+forgetful.UID+"/disable", ""); code != http.StatusOK {
		t.Fatalf("disabling the account = %d, body %s", code, body)
	}

	if got := resetStatus(t, env, token); got.Valid {
		t.Error("a blocked account's link reports itself valid")
	}
	if code, _ := consumeReset(t, env, token, theNewPassword); code != http.StatusNotFound {
		t.Errorf("using a blocked account's link = %d, want 404", code)
	}
	if code, body := issueReset(t, env, admin, forgetful.UID); code != http.StatusConflict {
		t.Errorf("issuing for a blocked account = %d, body %s, want 409", code, body)
	}
}

// TestPasswordReset_refusesTheUnknownAndTheWeak covers what the two public
// endpoints answer to a caller who has no link, or a password the rules refuse.
func TestPasswordReset_refusesTheUnknownAndTheWeak(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "boss", auth.RoleAdmin)
	forgetful := env.mustCreate(t, "forgetful", auth.RoleViewer)
	admin := signInAs(t, env, "boss")

	if got := resetStatus(t, env, "not-a-token-anybody-issued"); got.Valid {
		t.Error("an unknown link reports itself valid")
	}
	if got := resetStatus(t, env, "not-a-token-anybody-issued"); got.DisplayName != "" {
		t.Errorf("an unknown link answers a display name %q", got.DisplayName)
	}
	if code, _ := consumeReset(t, env, "not-a-token-anybody-issued", theNewPassword); code != http.StatusNotFound {
		t.Errorf("using an unknown link = %d, want 404", code)
	}

	token := resetToken(t, mustIssueReset(t, env, admin, forgetful.UID).ResetURL)
	if code, _ := consumeReset(t, env, token, "short"); code != http.StatusBadRequest {
		t.Errorf("a too-short password = %d, want 400", code)
	}
	// The refusal did not spend the link: the person may simply try again.
	if got := resetStatus(t, env, token); !got.Valid {
		t.Error("a refused password burnt the link")
	}
}

// TestPasswordReset_maintainerBoundary pins that the link does not become a way
// around the maintainer boundary: it ultimately sets a password, so an admin may
// not start one for a maintainer.
func TestPasswordReset_maintainerBoundary(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "boss", auth.RoleAdmin)
	root := env.mustCreate(t, "root", auth.RoleMaintainer)
	admin := signInAs(t, env, "boss")

	if code, body := issueReset(t, env, admin, root.UID); code != http.StatusForbidden {
		t.Errorf("an admin resetting a maintainer = %d, body %s, want 403", code, body)
	}
	if n := countResetTokens(t, env, root.UID); n != 0 {
		t.Errorf("reset rows for the maintainer = %d, want 0", n)
	}
	if code, _ := issueReset(t, env, admin, "usnosuchaccount"); code != http.StatusNotFound {
		t.Errorf("resetting an unknown account = %d, want 404", code)
	}
}

// TestPasswordReset_publicEndpointsAreRateLimited checks the per-address budget
// on the two endpoints anybody may call: with a budget of one, the second read
// of a link is throttled rather than answered.
func TestPasswordReset_publicEndpointsAreRateLimited(t *testing.T) {
	env := newHTTPEnv(t, 1)
	client := newClient(t)
	path := "/api/v1/auth/password-reset/whatever-token"

	if code, body := env.do(t, client, http.MethodGet, path, ""); code != http.StatusOK {
		t.Fatalf("the first read = %d, body %s", code, body)
	}
	if code, _ := env.do(t, client, http.MethodGet, path, ""); code != http.StatusTooManyRequests {
		t.Errorf("the second read = %d, want 429", code)
	}
	if code, _ := consumeReset(t, env, "whatever-token", theNewPassword); code != http.StatusTooManyRequests {
		t.Errorf("the write after the budget = %d, want 429", code)
	}
}

// TestPasswordReset_needsAnAdmin pins the guard on the issuing endpoint: signing
// in as an editor is not enough, and neither is signing in not at all.
func TestPasswordReset_needsAnAdmin(t *testing.T) {
	env := newHTTPEnv(t, 10)
	forgetful := env.mustCreate(t, "forgetful", auth.RoleViewer)
	env.mustCreate(t, "editor", auth.RoleEditor)

	if code, _ := issueReset(t, env, signInAs(t, env, "editor"), forgetful.UID); code != http.StatusForbidden {
		t.Errorf("an editor issuing a reset = %d, want 403", code)
	}
	if code, _ := issueReset(t, env, newClient(t), forgetful.UID); code != http.StatusUnauthorized {
		t.Errorf("an anonymous caller issuing a reset = %d, want 401", code)
	}
	if n := countResetTokens(t, env, forgetful.UID); n != 0 {
		t.Errorf("reset rows = %d, want 0", n)
	}
}
