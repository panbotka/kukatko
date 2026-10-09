//go:build integration

package auth_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
)

// renameUser sends one rename as client and returns the status and body.
func renameUser(t *testing.T, env *httpEnv, client *http.Client, uid, username string) (int, []byte) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"username": username})
	if err != nil {
		t.Fatalf("encoding the rename body: %v", err)
	}
	return env.do(t, client, http.MethodPut, "/api/v1/admin/users/"+uid+"/username", string(body))
}

// renameMail returns the queued `username_changed` messages, oldest first.
func renameMail(t *testing.T, env *httpEnv) []mailPayload {
	t.Helper()
	var out []mailPayload
	for _, m := range queuedMail(t, env) {
		if m.Template == mailer.TemplateUsernameChanged {
			out = append(out, m)
		}
	}
	return out
}

// storedUsername reads the account's username straight from the store.
func storedUsername(t *testing.T, env *httpEnv, uid string) string {
	t.Helper()
	user, err := env.store.GetUserByUID(t.Context(), uid)
	if err != nil {
		t.Fatalf("re-reading account %s: %v", uid, err)
	}
	return user.Username
}

// TestRename_renamesAuditsAndMails is the happy path: the administrator renames
// an account, the name comes back normalized exactly as on creation, the stored
// row agrees, the rename is in the audit trail with both names, and the person
// has a message on the queue telling them the name to sign in with.
func TestRename_renamesAuditsAndMails(t *testing.T) {
	env := newHTTPEnv(t, 10)
	boss := env.mustCreate(t, "boss", auth.RoleAdmin)
	admin := signInAs(t, env, "boss")
	silly := env.mustCreate(t, "xxx_jan_xxx", auth.RoleViewer)

	status, body := renameUser(t, env, admin, silly.UID, "  Jan.Novak ")
	if status != http.StatusOK {
		t.Fatalf("PUT username = %d, body %s", status, body)
	}
	var renamed accountFlags
	if err := json.Unmarshal(body, &renamed); err != nil {
		t.Fatalf("decoding the renamed account: %v", err)
	}
	if renamed.UID != silly.UID || renamed.Username != "jan.novak" {
		t.Errorf("answered %s/%q, want %s/%q", renamed.UID, renamed.Username, silly.UID, "jan.novak")
	}
	if got := storedUsername(t, env, silly.UID); got != "jan.novak" {
		t.Errorf("stored username = %q, want %q", got, "jan.novak")
	}

	var actor, target, details string
	if err := env.db.Pool().QueryRow(t.Context(),
		`SELECT actor_uid, target_uid, details::text FROM audit_log WHERE action = $1`,
		audit.ActionUserRename).Scan(&actor, &target, &details); err != nil {
		t.Fatalf("reading the rename audit entry: %v", err)
	}
	if actor != boss.UID || target != silly.UID {
		t.Errorf("audit actor/target = %s/%s, want %s/%s", actor, target, boss.UID, silly.UID)
	}
	for _, want := range []string{`"old_username": "xxx_jan_xxx"`, `"new_username": "jan.novak"`} {
		if !strings.Contains(details, want) {
			t.Errorf("audit details %s lack %s", details, want)
		}
	}

	mails := renameMail(t, env)
	if len(mails) != 1 {
		t.Fatalf("queued rename mails = %d, want 1", len(mails))
	}
	if mails[0].To != "xxx_jan_xxx@example.test" {
		t.Errorf("rename mail to %q, want the account's address", mails[0].To)
	}
	var data mailer.UsernameChangedData
	if err := json.Unmarshal(mails[0].Data, &data); err != nil {
		t.Fatalf("decoding the rename mail data: %v", err)
	}
	if data.Username != "jan.novak" || data.SignInURL != testSignInURL {
		t.Errorf("rename mail data = %+v, want the new name and %q", data, testSignInURL)
	}
}

// TestRename_sameNameIsANoOp pins that renaming an account to the name it
// already has (after normalization) answers 200 with the account unchanged and
// leaves neither an audit entry nor a mail behind.
func TestRename_sameNameIsANoOp(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "boss", auth.RoleAdmin)
	admin := signInAs(t, env, "boss")
	jan := env.mustCreate(t, "jan", auth.RoleViewer)

	status, body := renameUser(t, env, admin, jan.UID, " JAN ")
	if status != http.StatusOK {
		t.Fatalf("renaming to the same name = %d, want 200 (body %s)", status, body)
	}
	if n := countAudit(t, env, audit.ActionUserRename); n != 0 {
		t.Errorf("%q audit entries = %d, want 0", audit.ActionUserRename, n)
	}
	if n := len(renameMail(t, env)); n != 0 {
		t.Errorf("queued rename mails = %d, want 0", n)
	}
}

// TestRename_refusesATakenName covers the 409: a name another account holds is
// refused whether it is typed identically or differs only in letter case — and
// also against a legacy row stored in mixed case, which the unique index alone
// would not see. Nothing changes on a refusal.
func TestRename_refusesATakenName(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "boss", auth.RoleAdmin)
	admin := signInAs(t, env, "boss")
	jan := env.mustCreate(t, "jan", auth.RoleViewer)
	env.mustCreate(t, "eva", auth.RoleViewer)
	legacy := env.mustCreate(t, "legacy", auth.RoleViewer)
	if _, err := env.db.Pool().Exec(t.Context(),
		`UPDATE users SET username = 'Petr' WHERE uid = $1`, legacy.UID); err != nil {
		t.Fatalf("storing a mixed-case legacy username: %v", err)
	}

	for _, taken := range []string{"eva", "EVA", "petr", "boss"} {
		status, body := renameUser(t, env, admin, jan.UID, taken)
		if status != http.StatusConflict {
			t.Errorf("renaming to %q = %d, want 409 (body %s)", taken, status, body)
			continue
		}
		if !strings.Contains(string(body), "username already taken") {
			t.Errorf("renaming to %q answered %s, want the taken message", taken, body)
		}
	}
	if got := storedUsername(t, env, jan.UID); got != "jan" {
		t.Errorf("stored username after refusals = %q, want %q", got, "jan")
	}
	if n := countAudit(t, env, audit.ActionUserRename); n != 0 {
		t.Errorf("%q audit entries = %d, want 0", audit.ActionUserRename, n)
	}
	if n := len(renameMail(t, env)); n != 0 {
		t.Errorf("queued rename mails = %d, want 0", n)
	}
}

// TestRename_refusesAnInvalidName covers the 400s, which name the field so the
// client can put the message under it: an empty (whitespace-only) name and one
// over MaxUsernameLen runes, the same rules creation applies.
func TestRename_refusesAnInvalidName(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "boss", auth.RoleAdmin)
	admin := signInAs(t, env, "boss")
	jan := env.mustCreate(t, "jan", auth.RoleViewer)

	tests := []struct {
		name     string
		username string
		want     string
	}{
		{name: "empty", username: "   ", want: auth.ErrUsernameRequired.Error()},
		{name: "over-long", username: strings.Repeat("ř", auth.MaxUsernameLen+1), want: auth.ErrUsernameTooLong.Error()},
	}
	for _, tt := range tests {
		status, body := renameUser(t, env, admin, jan.UID, tt.username)
		if status != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body %s)", tt.name, status, body)
			continue
		}
		if !strings.Contains(string(body), tt.want) {
			t.Errorf("%s: body %s, want %q", tt.name, body, tt.want)
		}
	}
	if status, body := env.do(t, admin, http.MethodPut,
		"/api/v1/admin/users/"+jan.UID+"/username", "{not json"); status != http.StatusBadRequest {
		t.Errorf("malformed body = %d, want 400 (body %s)", status, body)
	}
	if got := storedUsername(t, env, jan.UID); got != "jan" {
		t.Errorf("stored username after refusals = %q, want %q", got, "jan")
	}
}

// TestRename_roleBoundaries covers who may rename: an admin may not rename a
// maintainer account (the maintainer boundary, 403), a maintainer may, every
// role below admin is refused by the route guard (403), an unknown account is
// 404, and an administrator may rename their own account.
func TestRename_roleBoundaries(t *testing.T) {
	env := newHTTPEnv(t, 10)
	boss := env.mustCreate(t, "boss", auth.RoleAdmin)
	env.mustCreate(t, "root", auth.RoleMaintainer)
	keeper := env.mustCreate(t, "keeper", auth.RoleMaintainer)
	jan := env.mustCreate(t, "jan", auth.RoleViewer)
	admin := signInAs(t, env, "boss")
	maintainer := signInAs(t, env, "root")

	if status, body := renameUser(t, env, admin, keeper.UID, "kept"); status != http.StatusForbidden {
		t.Errorf("admin renaming a maintainer = %d, want 403 (body %s)", status, body)
	}
	if got := storedUsername(t, env, keeper.UID); got != "keeper" {
		t.Errorf("maintainer renamed by an admin to %q", got)
	}
	if status, body := renameUser(t, env, maintainer, keeper.UID, "kept"); status != http.StatusOK {
		t.Errorf("maintainer renaming a maintainer = %d, want 200 (body %s)", status, body)
	}

	for _, role := range []auth.Role{auth.RoleViewer, auth.RoleCurator, auth.RoleEditor} {
		name := "lower-" + string(role)
		env.mustCreate(t, name, role)
		client := signInAs(t, env, name)
		if status, body := renameUser(t, env, client, jan.UID, "renamed-by-"+string(role)); status != http.StatusForbidden {
			t.Errorf("%s renaming = %d, want 403 (body %s)", role, status, body)
		}
	}
	if got := storedUsername(t, env, jan.UID); got != "jan" {
		t.Errorf("a non-admin renamed the account to %q", got)
	}

	if status, body := renameUser(t, env, admin, "us-doesnotexist", "ghost"); status != http.StatusNotFound {
		t.Errorf("renaming an unknown account = %d, want 404 (body %s)", status, body)
	}

	if status, body := renameUser(t, env, admin, boss.UID, "chief"); status != http.StatusOK {
		t.Fatalf("admin renaming themselves = %d, want 200 (body %s)", status, body)
	}
	if status, body := env.do(t, admin, http.MethodGet, "/api/v1/auth/me", ""); status != http.StatusOK ||
		!strings.Contains(string(body), `"username":"chief"`) {
		t.Errorf("GET /auth/me after renaming oneself = %d %s, want 200 with the new name", status, body)
	}
}

// TestRename_keepsEverythingKeyedOnUID is the "nothing breaks" guarantee. The
// structural half reads the catalog: every foreign key into users references
// users.uid — sessions, API tokens and passkeys among them — and none the
// username, so a rename cannot orphan anything. The behavioural half proves it
// on a live account: its open session keeps working without signing in again,
// its API token still authenticates, its passkey still belongs to it, and the
// password sign-in now takes the new name only.
func TestRename_keepsEverythingKeyedOnUID(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "boss", auth.RoleAdmin)
	admin := signInAs(t, env, "boss")
	jan := env.mustCreate(t, "jan", auth.RoleViewer)
	janClient := signInAs(t, env, "jan")

	assertUsersReferencedOnlyByUID(t, env)

	_, plaintext, err := env.svc.CreateAPIToken(t.Context(), jan, auth.CreateAPITokenInput{Name: "script"},
		audit.Meta{ActorUID: jan.UID}.Entry(audit.ActionAPITokenCreate, "api_tokens", "", nil))
	if err != nil {
		t.Fatalf("minting jan's API token: %v", err)
	}
	passkey := auth.Passkey{
		ID: "pk-rename-test", UserUID: jan.UID, Name: "phone", CreatedAt: jan.CreatedAt,
		Credential: webauthn.Credential{
			ID: []byte("rename-credential"), PublicKey: []byte{1, 2, 3},
			Authenticator: webauthn.Authenticator{AAGUID: make([]byte, 16)},
		},
	}
	if err := env.store.CreatePasskeyAudited(t.Context(), passkey,
		audit.Meta{ActorUID: jan.UID}.Entry(audit.ActionPasskeyRegister, "passkeys", "", nil)); err != nil {
		t.Fatalf("storing jan's passkey: %v", err)
	}

	if status, body := renameUser(t, env, admin, jan.UID, "jan.novak"); status != http.StatusOK {
		t.Fatalf("PUT username = %d, body %s", status, body)
	}

	// The session opened under the old name is still the same signed-in person.
	status, body := env.do(t, janClient, http.MethodGet, "/api/v1/auth/me", "")
	if status != http.StatusOK || !strings.Contains(string(body), `"username":"jan.novak"`) {
		t.Errorf("GET /auth/me on the pre-rename session = %d %s, want 200 with the new name", status, body)
	}
	owner, _, err := env.svc.AuthenticateAPIToken(t.Context(), plaintext)
	if err != nil || owner.UID != jan.UID || owner.Username != "jan.novak" {
		t.Errorf("API token after the rename = %s/%q, %v; want %s/jan.novak", owner.UID, owner.Username, err, jan.UID)
	}
	stored, err := env.store.GetPasskeyByCredentialID(t.Context(), []byte("rename-credential"))
	if err != nil || stored.UserUID != jan.UID {
		t.Errorf("passkey after the rename belongs to %q (%v), want %s", stored.UserUID, err, jan.UID)
	}

	if status, _ := env.do(t, newClient(t), http.MethodPost, "/api/v1/auth/login",
		loginJSON("jan", testPassword)); status != http.StatusUnauthorized {
		t.Errorf("login with the old name = %d, want 401", status)
	}
	if status, body := env.do(t, newClient(t), http.MethodPost, "/api/v1/auth/login",
		loginJSON("jan.novak", testPassword)); status != http.StatusOK {
		t.Errorf("login with the new name = %d, want 200 (body %s)", status, body)
	}
}

// assertUsersReferencedOnlyByUID reads every foreign key that points into the
// users table and fails unless each references users.uid — and unless the
// tables holding sessions, API tokens and passkeys are among them, so the
// assertion cannot pass vacuously.
func assertUsersReferencedOnlyByUID(t *testing.T, env *httpEnv) {
	t.Helper()
	rows, err := env.db.Pool().Query(t.Context(), `
		SELECT c.conrelid::regclass::text, a.attname
		FROM pg_constraint c
		JOIN pg_attribute a ON a.attrelid = c.confrelid AND a.attnum = ANY (c.confkey)
		WHERE c.contype = 'f' AND c.confrelid = 'users'::regclass`)
	if err != nil {
		t.Fatalf("reading the foreign keys into users: %v", err)
	}
	defer rows.Close()
	referencing := map[string]bool{}
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			t.Fatalf("scanning a foreign key: %v", err)
		}
		if column != "uid" {
			t.Errorf("%s references users.%s, want users.uid — a rename would orphan it", table, column)
		}
		referencing[table] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating the foreign keys: %v", err)
	}
	for _, table := range []string{"sessions", "api_tokens", "passkey_credentials"} {
		if !referencing[table] {
			t.Errorf("%s holds no foreign key into users; got %v", table, referencing)
		}
	}
}

// TestRename_mailIsSkippedNotFailed covers the two cases in which no message is
// scheduled and the rename succeeds anyway: an account whose address is a
// .invalid placeholder, and an instance with mail switched off. In both the
// placeholder address itself is left as it was.
func TestRename_mailIsSkippedNotFailed(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "boss", auth.RoleAdmin)
	admin := signInAs(t, env, "boss")
	jan := env.mustCreate(t, "jan", auth.RoleViewer)
	const placeholder = "jan-us123@kukatko.invalid"
	if _, err := env.db.Pool().Exec(t.Context(),
		`UPDATE users SET email = $2 WHERE uid = $1`, jan.UID, placeholder); err != nil {
		t.Fatalf("giving jan a placeholder address: %v", err)
	}

	if status, body := renameUser(t, env, admin, jan.UID, "jan.novak"); status != http.StatusOK {
		t.Fatalf("renaming a placeholder account = %d, body %s", status, body)
	}
	renamed, err := env.store.GetUserByUID(t.Context(), jan.UID)
	if err != nil {
		t.Fatalf("re-reading jan: %v", err)
	}
	if renamed.Email != placeholder {
		t.Errorf("placeholder address rewritten to %q, want it untouched", renamed.Email)
	}

	eva := env.mustCreate(t, "eva", auth.RoleViewer)
	mailOff := auth.NewRename(auth.RenameConfig{
		Service: env.svc,
		Mail:    mailjob.NewEnqueuer(mailjob.EnqueuerConfig{Enabled: false}),
	})
	user, err := mailOff.Rename(t.Context(), eva.UID, "eva.novakova", auth.RoleAdmin,
		audit.Meta{ActorUID: eva.UID}.Entry(audit.ActionUserRename, "users", "", nil))
	if err != nil || user.Username != "eva.novakova" {
		t.Fatalf("renaming with mail off = %q, %v; want eva.novakova", user.Username, err)
	}

	if n := len(renameMail(t, env)); n != 0 {
		t.Errorf("queued rename mails = %d, want 0", n)
	}
	if n := countAudit(t, env, audit.ActionUserRename); n != 2 {
		t.Errorf("%q audit entries = %d, want 2 — both renames happened", audit.ActionUserRename, n)
	}
}
