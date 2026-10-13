//go:build integration

package auth_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/project"
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

// approve lets the waiting account uid into the bootstrap project the way a
// project admin now does it — through the membership (internal/memberapi over
// project.Store.Approve); the account's own approval is derived from it. The
// instance-wide approval route is gone.
func approve(t *testing.T, env *httpEnv, uid string) {
	t.Helper()
	if _, err := project.NewStore(env.db.Pool()).Approve(t.Context(), audit.Meta{},
		project.BootstrapUID, uid); err != nil {
		t.Fatalf("approving %s: %v", uid, err)
	}
}

// TestApprove_instanceRouteIsGone proves the instance-wide approval endpoint
// no longer exists: approval is a membership's, and only a project admin of
// that project gives it (/p/{project}/members/{user_uid}/approve). The account
// stays waiting.
func TestApprove_instanceRouteIsGone(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "boss", auth.AccountMaintainer)
	admin := signInAs(t, env, "boss")
	waiting := waitingAccount(t, env, "newcomer")

	status, body := env.do(t, admin, http.MethodPost, "/api/v1/admin/users/"+waiting.UID+"/approve", "")
	if status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
		t.Errorf("POST /admin/users/{uid}/approve = %d, want 404/405 (body %s)", status, body)
	}
	user, err := env.store.GetUserByUID(t.Context(), waiting.UID)
	if err != nil || user.ApprovedAt != nil {
		t.Errorf("account after the removed route = approved %v (%v), want still waiting", user.ApprovedAt, err)
	}
}

// TestListUsers_pendingFilter covers the listing filter an administrator uses to
// find the accounts waiting for them: pending=true lists only those,
// pending=false only the ones already in, an absent parameter everybody, and a
// value that is not a boolean is refused rather than quietly ignored.
func TestListUsers_pendingFilter(t *testing.T) {
	env := newHTTPEnv(t, 10)
	// A maintainer: the account roster is the system administrator's.
	env.mustCreate(t, "boss", auth.AccountMaintainer)
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
	approve(t, env, waiting.UID)
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
