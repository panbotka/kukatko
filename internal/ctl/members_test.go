package ctl

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// membersBody is a realistic GET /p/{slug}/members answer: an approved admin
// who signed in, and a member still waiting who never did.
const membersBody = `[
	{"user_uid":"us1","username":"anna","display_name":"Anna","role":"admin",
	 "approved_at":"2026-10-11T10:00:00Z","last_login_at":"2026-10-12T08:30:00Z","has_picture":true},
	{"user_uid":"us2","username":"bob","display_name":"","role":"viewer",
	 "approved_at":null,"last_login_at":null,"has_picture":false}
]`

// TestWriteMembers verifies the table shows role, approval state and last
// sign-in, a waiting member marked pending.
func TestWriteMembers(t *testing.T) {
	t.Parallel()
	list, err := DecodeMembers([]byte(membersBody))
	if err != nil || len(list) != 2 {
		t.Fatalf("DecodeMembers = %+v, %v", list, err)
	}
	var buf bytes.Buffer
	if err := WriteMembers(&buf, list); err != nil {
		t.Fatalf("WriteMembers: %v", err)
	}
	for _, want := range []string{"USERNAME", "LAST LOGIN", "anna", "admin", "approved", "pending",
		"2026-10-12 08:30"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("member table does not contain %q:\n%s", want, buf.String())
		}
	}
	buf.Reset()
	if err := WriteMembers(&buf, nil); err != nil || !strings.Contains(buf.String(), "no members") {
		t.Errorf("empty member table = %q, %v", buf.String(), err)
	}
	buf.Reset()
	if err := WriteMember(&buf, list[1]); err != nil || !strings.Contains(buf.String(), "pending") {
		t.Errorf("member detail = %q, %v", buf.String(), err)
	}
}

// TestClient_memberRoutes verifies each call's method, path and body, all
// under the project's own prefix.
func TestClient_memberRoutes(t *testing.T) {
	t.Parallel()
	var method, path, body string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})
	ctx := t.Context()
	steps := []struct {
		call                 func() error
		wantMethod, wantPath string
		wantBody             string
	}{
		{func() error { _, err := client.ListMembers(ctx, "club"); return err },
			"GET", "/api/v1/p/club/members", ""},
		{func() error { _, err := client.AddMember(ctx, "club", "Jan@Example.test", "editor"); return err },
			"POST", "/api/v1/p/club/members", `{"identifier":"Jan@Example.test","role":"editor"}`},
		{func() error { _, err := client.SetMemberRole(ctx, "club", "us2", "curator"); return err },
			"PATCH", "/api/v1/p/club/members/us2", `{"role":"curator"}`},
		{func() error { _, err := client.ApproveMember(ctx, "club", "us2"); return err },
			"POST", "/api/v1/p/club/members/us2/approve", ""},
		{func() error { return client.RemoveMember(ctx, "club", "us2") },
			"DELETE", "/api/v1/p/club/members/us2", ""},
	}
	for _, step := range steps {
		if err := step.call(); err != nil {
			t.Fatalf("%s %s: %v", step.wantMethod, step.wantPath, err)
		}
		if method != step.wantMethod || path != step.wantPath || strings.TrimSpace(body) != step.wantBody {
			t.Errorf("request = %s %s %s, want %s %s %s", method, path, body,
				step.wantMethod, step.wantPath, step.wantBody)
		}
	}
}

// TestClient_memberRoutesRefuseBlankInput verifies a missing project, account
// or member costs no round trip.
func TestClient_memberRoutesRefuseBlankInput(t *testing.T) {
	t.Parallel()
	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	})
	ctx := t.Context()
	if _, err := client.ListMembers(ctx, " "); !errors.Is(err, ErrNoProjectSlug) {
		t.Errorf("ListMembers without a project = %v, want ErrNoProjectSlug", err)
	}
	if _, err := client.AddMember(ctx, "club", "  ", "viewer"); !errors.Is(err, ErrNoIdentifier) {
		t.Errorf("AddMember without an identifier = %v, want ErrNoIdentifier", err)
	}
	if _, err := client.SetMemberRole(ctx, "club", "", "viewer"); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("SetMemberRole without a member = %v, want ErrEmptyUID", err)
	}
	if _, err := client.ApproveMember(ctx, "", "us1"); !errors.Is(err, ErrNoProjectSlug) {
		t.Errorf("ApproveMember without a project = %v, want ErrNoProjectSlug", err)
	}
	if err := client.RemoveMember(ctx, "club", ""); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("RemoveMember without a member = %v, want ErrEmptyUID", err)
	}
}

// TestClient_resolveMember verifies a member is found by username or uid in
// the project's list, and a miss is a typed error.
func TestClient_resolveMember(t *testing.T) {
	t.Parallel()
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/p/club/members" {
			t.Errorf("resolve asked %s, want the project's member list", r.URL.Path)
		}
		_, _ = w.Write([]byte(membersBody))
	})
	ctx := t.Context()
	if m, err := client.ResolveMember(ctx, "club", "bob"); err != nil || m.UserUID != "us2" {
		t.Errorf("ResolveMember(bob) = %+v, %v", m, err)
	}
	if m, err := client.ResolveMember(ctx, "club", "us1"); err != nil || m.Username != "anna" {
		t.Errorf("ResolveMember(us1) = %+v, %v", m, err)
	}
	if _, err := client.ResolveMember(ctx, "club", "carl"); !errors.Is(err, ErrMemberNotFound) {
		t.Errorf("ResolveMember(carl) = %v, want ErrMemberNotFound", err)
	}
}
