package main

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// membersListBody is a two-member GET /p/{slug}/members answer.
const membersListBody = `[{"user_uid":"us1","username":"anna","display_name":"Anna","role":"admin",` +
	`"approved_at":"2026-10-11T10:00:00Z","last_login_at":null,"has_picture":false},` +
	`{"user_uid":"us2","username":"bob","display_name":"Bob","role":"viewer","approved_at":null,` +
	`"last_login_at":null,"has_picture":false}]`

// memberServer starts a fake of the /p/{slug}/members routes, recording every
// request, and returns the ctl config path and the recorded requests.
func memberServer(t *testing.T) (string, func() []projectRequest) {
	t.Helper()
	var (
		mu   sync.Mutex
		seen []projectRequest
	)
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, projectRequest{r.Method, r.URL.Path, string(body)})
		mu.Unlock()
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/members"):
			_, _ = w.Write([]byte(membersListBody))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/members"):
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"status":"accepted"}`))
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPatch && strings.Contains(string(body), `"admin"`):
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"self","message":"an admin cannot change their own role"}`))
		default:
			_, _ = w.Write([]byte(`{"user_uid":"us2","username":"bob","display_name":"Bob","role":"editor",` +
				`"approved_at":"2026-10-13T10:00:00Z","last_login_at":null,"has_picture":false}`))
		}
	})
	return configPath, func() []projectRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]projectRequest(nil), seen...)
	}
}

// TestCtlMembers_list verifies the member table and that the members of the
// context's project are listed, the persistent --project overriding it.
func TestCtlMembers_list(t *testing.T) {
	configPath, seen := memberServer(t)

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "members", "list")
	if err != nil {
		t.Fatalf("members list: %v", err)
	}
	if got := lastRequest(t, seen); got.path != "/api/v1/p/fam/members" {
		t.Errorf("context project request = %+v, want the context project's members", got)
	}
	for _, want := range []string{"USERNAME", "anna", "admin", "pending"} {
		if !strings.Contains(out, want) {
			t.Errorf("member table does not contain %q:\n%s", want, out)
		}
	}
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "members", "list", "--project", "club"); err != nil {
		t.Fatalf("members list --project club: %v", err)
	}
	if got := lastRequest(t, seen); got.path != "/api/v1/p/club/members" {
		t.Errorf("--project request = %+v, want club's members", got)
	}
}

// TestCtlMembers_add verifies the identifier and role become the body and the
// confirmation does not claim the account exists.
func TestCtlMembers_add(t *testing.T) {
	configPath, seen := memberServer(t)

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "members", "add", "jan@example.test",
		"--role", "editor", "--project", "club")
	if err != nil {
		t.Fatalf("members add: %v", err)
	}
	got := lastRequest(t, seen)
	if got.method != http.MethodPost || got.path != "/api/v1/p/club/members" ||
		got.body != `{"identifier":"jan@example.test","role":"editor"}` {
		t.Errorf("add request = %+v", got)
	}
	if !strings.Contains(out, "if jan@example.test names an account") {
		t.Errorf("add output = %q, want the neutral confirmation", out)
	}
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "members", "add", "jan"); err == nil {
		t.Error("members add without --role succeeded")
	}
}

// TestCtlMembers_roleApproveRemove verifies the username is resolved to a uid
// in the project's list and each change goes to that member's route, and a
// refusal reports the server's code.
func TestCtlMembers_roleApproveRemove(t *testing.T) {
	configPath, seen := memberServer(t)

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "members", "role", "bob", "editor")
	if err != nil {
		t.Fatalf("members role: %v", err)
	}
	if got := lastRequest(t, seen); got.method != http.MethodPatch || got.path != "/api/v1/p/fam/members/us2" ||
		got.body != `{"role":"editor"}` || !strings.Contains(out, "editor") {
		t.Errorf("role request = %+v, output %q", got, out)
	}

	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "members", "approve", "bob"); err != nil {
		t.Fatalf("members approve: %v", err)
	}
	if got := lastRequest(t, seen); got.method != http.MethodPost || got.path != "/api/v1/p/fam/members/us2/approve" {
		t.Errorf("approve request = %+v", got)
	}

	out, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "members", "remove", "bob")
	if err != nil {
		t.Fatalf("members remove: %v", err)
	}
	if got := lastRequest(t, seen); got.method != http.MethodDelete || got.path != "/api/v1/p/fam/members/us2" ||
		!strings.Contains(out, "removed bob from fam") {
		t.Errorf("remove request = %+v, output %q", got, out)
	}

	_, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "members", "role", "anna", "admin")
	if err == nil || !strings.Contains(err.Error(), "self") {
		t.Errorf("a refused role change = %v, want the server's code", err)
	}
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "members", "approve", "carl"); err == nil ||
		!strings.Contains(err.Error(), "no such member") {
		t.Errorf("approve of a non-member = %v, want a no-such-member error", err)
	}
}
