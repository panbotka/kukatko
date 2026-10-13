//go:build integration

package announcementapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// TestAnnouncement_publishedInAIsInvisibleInB: a banner the system
// administrator publishes in A is read by A's member and not by B's, whose
// project still answers "nothing published"; clearing it in B leaves A's up.
func TestAnnouncement_publishedInAIsInvisibleInB(t *testing.T) {
	env := newEnv(t)
	f := projecttest.Seed(t, env.db.Pool())
	maintainer := env.login(t, "boss", auth.AccountMaintainer)

	resp := env.mustDo(t, maintainer, http.MethodPut, f.PathFor(projecttest.SideA, "/announcement"),
		[]byte(`{"message":"Jen pro A","level":"info"}`))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT in A status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	read := func(which projecttest.Side) string {
		t.Helper()
		client := f.ClientFor(t, env.server, which)
		resp := f.Get(t, client, f.PathFor(which, "/announcement"))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET in %s status = %d, want 200", which, resp.StatusCode)
		}
		var body struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decoding %s's announcement: %v", which, err)
		}
		return body.Message
	}
	if got := read(projecttest.SideA); got != "Jen pro A" {
		t.Fatalf("A's member reads %q, want A's banner", got)
	}
	if got := read(projecttest.SideB); got != "" {
		t.Fatalf("B's member reads %q, want nothing published", got)
	}

	resp = env.mustDo(t, maintainer, http.MethodDelete, f.PathFor(projecttest.SideB, "/announcement"), nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE in B status = %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()
	if got := read(projecttest.SideA); got != "Jen pro A" {
		t.Fatalf("clearing B took A's banner down: A reads %q", got)
	}
}
