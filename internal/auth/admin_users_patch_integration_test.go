//go:build integration

package auth_test

import (
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
)

// TestAdminUsers_patchKeepsWhatItDoesNotName pins PATCH /admin/users/{uid} as a
// patch: an edit made from a record loaded before another administrator blocked
// and promoted the account sends only the name it changed, and the block and
// the promotion survive it. An empty body writes nothing — no audit entry, no
// updated_at bump — and an address the patch does name must still be valid.
func TestAdminUsers_patchKeepsWhatItDoesNotName(t *testing.T) {
	env := newHTTPEnv(t, 50)
	env.mustCreate(t, "root", auth.AccountMaintainer)
	admin := env.loginClient(t, "root")
	target := env.mustCreate(t, "jana", auth.AccountViewer)
	path := "/api/v1/admin/users/" + target.UID

	// Another administrator, meanwhile: blocks the account and promotes it.
	if status, data := env.do(t, admin, http.MethodPost, path+"/disable", ""); status != http.StatusOK {
		t.Fatalf("POST disable = %d (body %s)", status, data)
	}
	if status, data := env.do(t, admin, http.MethodPatch, path,
		adminUserBody(t, map[string]any{"instance_role": "maintainer"})); status != http.StatusOK {
		t.Fatalf("PATCH instance_role = %d (body %s)", status, data)
	}

	// The stale dialog renames the person and sends nothing else.
	status, data := env.do(t, admin, http.MethodPatch, path,
		adminUserBody(t, map[string]any{"display_name": "Jana N"}))
	if status != http.StatusOK {
		t.Fatalf("PATCH display_name = %d (body %s)", status, data)
	}
	got := decodeUser(t, data)
	if got["display_name"] != "Jana N" || got["disabled"] != true || got["instance_role"] != "maintainer" ||
		got["email"] != target.Email {
		t.Errorf("after a name-only patch = %v, want the new name, still blocked, still maintainer, same e-mail", got)
	}
	stored, err := env.store.GetUserByUID(t.Context(), target.UID)
	if err != nil {
		t.Fatalf("reading the account back: %v", err)
	}
	if !stored.Disabled || stored.Role != auth.RoleMaintainer {
		t.Errorf("stored disabled = %v role = %q, want true maintainer", stored.Disabled, stored.Role)
	}

	recs, err := audit.NewStore(env.db.Pool()).List(t.Context(),
		audit.Filter{Action: audit.ActionUserUpdate, TargetUID: target.UID, Limit: 10})
	if err != nil || len(recs) != 2 {
		t.Fatalf("user.update audit = %d rows (%v), want 2", len(recs), err)
	}
	for _, rec := range recs {
		if _, named := rec.Details["disabled"]; named {
			t.Errorf("a patch that never sent the flag is audited with one: %v", rec.Details)
		}
	}

	if status, data := env.do(t, admin, http.MethodPatch, path, "{}"); status != http.StatusOK {
		t.Fatalf("empty PATCH = %d (body %s)", status, data)
	}
	after, err := env.store.GetUserByUID(t.Context(), target.UID)
	if err != nil {
		t.Fatalf("reading the account back: %v", err)
	}
	if !after.UpdatedAt.Equal(stored.UpdatedAt) {
		t.Errorf("empty PATCH moved updated_at %v → %v", stored.UpdatedAt, after.UpdatedAt)
	}
	if recs, _ := audit.NewStore(env.db.Pool()).List(t.Context(),
		audit.Filter{Action: audit.ActionUserUpdate, TargetUID: target.UID, Limit: 10}); len(recs) != 2 {
		t.Errorf("empty PATCH wrote an audit entry: %d rows, want 2", len(recs))
	}

	if status, _ := env.do(t, admin, http.MethodPatch, path,
		adminUserBody(t, map[string]any{"email": ""})); status != http.StatusBadRequest {
		t.Errorf("PATCH email \"\" = %d, want 400", status)
	}
}
