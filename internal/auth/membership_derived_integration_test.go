//go:build integration

package auth_test

import (
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/project"
)

// secondProject creates a project besides the bootstrap one, with no members.
func (e *linkEnv) secondProject(t *testing.T) *project.Project {
	t.Helper()
	p, err := project.NewStore(e.pool).Create(t.Context(), audit.Meta{},
		project.CreateParams{Slug: "druhy", Name: "Druhý"})
	if err != nil {
		t.Fatalf("creating the second project: %v", err)
	}
	return p
}

// unapprove takes every membership of uid back to "waiting".
func (e *linkEnv) unapprove(t *testing.T, uid string) {
	t.Helper()
	if _, err := e.pool.Exec(t.Context(),
		`UPDATE project_members SET approved_at = NULL WHERE user_uid = $1`, uid); err != nil {
		t.Fatalf("un-approving %s: %v", uid, err)
	}
}

// login reports the status of a password sign-in as username.
func (e *linkEnv) login(t *testing.T, username string) int {
	t.Helper()
	status, _ := e.do(t, newClient(t), http.MethodPost, "/api/v1/auth/login",
		loginJSON(username, "correct horse battery"))
	return status
}

// TestApproval_isDerivedFromAnyMembership verifies the account's approval is
// read off its memberships now that users.approved_at is gone: with every
// membership waiting the account is pending (listed by ?pending=true, refused
// at sign-in), an approved membership in any project — not only the bootstrap
// one — lets it in, and a maintainer is approved whatever its memberships say.
func TestApproval_isDerivedFromAnyMembership(t *testing.T) {
	env := newLinkEnv(t)
	user := env.seedUser(t, "waiting", auth.AccountViewer)
	env.unapprove(t, user.UID)

	got, err := env.svc.GetUser(t.Context(), user.UID)
	if err != nil || got.ApprovedAt != nil {
		t.Fatalf("with every membership waiting: approved %v (%v), want nil", got.ApprovedAt, err)
	}
	pending := true
	listed, err := env.svc.ListUsers(t.Context(), auth.UserFilter{Pending: &pending})
	if err != nil || len(listed) != 1 || listed[0].UID != user.UID {
		t.Errorf("pending listing = %v (%v), want only the waiting account", listed, err)
	}
	if status := env.login(t, "waiting"); status != http.StatusForbidden {
		t.Errorf("sign-in while waiting = %d, want 403", status)
	}

	b := env.secondProject(t)
	if _, err := project.NewStore(env.pool).AddMember(t.Context(), audit.Meta{}, b.UID, user.UID,
		project.RoleViewer, true); err != nil {
		t.Fatalf("adding the account to the second project: %v", err)
	}
	got, err = env.svc.GetUser(t.Context(), user.UID)
	if err != nil || got.ApprovedAt == nil {
		t.Fatalf("approved in the second project: approved %v (%v), want a time", got.ApprovedAt, err)
	}
	if status := env.login(t, "waiting"); status != http.StatusOK {
		t.Errorf("sign-in once approved somewhere = %d, want 200", status)
	}

	boss := env.seedUser(t, "boss", auth.AccountMaintainer)
	env.unapprove(t, boss.UID)
	got, err = env.svc.GetUser(t.Context(), boss.UID)
	if err != nil || got.ApprovedAt == nil || !got.ApprovedAt.Equal(got.CreatedAt) {
		t.Errorf("maintainer with no approved membership: approved %v (%v), want its creation %v",
			got.ApprovedAt, err, got.CreatedAt)
	}
}

// TestSubjectLink_anotherProjectsPersonIsRejected verifies the self-service
// link is checked against the request's project: a person of another project
// is nobody here, the same 400 as a UID that names nothing.
func TestSubjectLink_anotherProjectsPersonIsRejected(t *testing.T) {
	env := newLinkEnv(t)
	b := env.secondProject(t)
	if _, err := env.pool.Exec(t.Context(),
		`INSERT INTO subjects (uid, project_id, slug, name, type) VALUES ('sub_b', $1, 'b', 'B', 'person')`,
		b.UID); err != nil {
		t.Fatalf("inserting the second project's person: %v", err)
	}
	env.seedUser(t, "viewer", auth.AccountViewer)
	client := env.signIn(t, "viewer")

	status, body := env.do(t, client, http.MethodPut, apiV+"/auth/subject", `{"subject_uid":"sub_b"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("PUT /auth/subject with another project's person = %d, body %s", status, body)
	}
	if got := env.meSubject(t, client); got != nil {
		t.Fatalf("a rejected link stored %q, want no link", *got)
	}
}

// TestApproval_anAccountInNoProjectMaySignIn verifies an account that belongs
// to no project at all is waiting for nobody: it counts as approved (since its
// creation) and may sign in — which is how it reaches an invitation link.
func TestApproval_anAccountInNoProjectMaySignIn(t *testing.T) {
	env := newLinkEnv(t)
	user := env.seedUser(t, "loner", auth.AccountViewer)
	if _, err := env.pool.Exec(t.Context(), `DELETE FROM project_members WHERE user_uid = $1`, user.UID); err != nil {
		t.Fatalf("removing the memberships: %v", err)
	}
	got, err := env.svc.GetUser(t.Context(), user.UID)
	if err != nil || got.ApprovedAt == nil || !got.ApprovedAt.Equal(got.CreatedAt) {
		t.Errorf("account in no project: approved %v (%v), want its creation %v", got.ApprovedAt, err, got.CreatedAt)
	}
	if status := env.login(t, "loner"); status != http.StatusOK {
		t.Errorf("sign-in of an account in no project = %d, want 200", status)
	}
}
