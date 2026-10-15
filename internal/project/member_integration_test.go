//go:build integration

package project_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/project"
)

// seedUser creates an account with the given uid, username and display name.
func seedUser(t *testing.T, db *database.DB, uid, username, displayName string) {
	t.Helper()
	if err := auth.NewStore(db.Pool()).CreateUser(context.Background(), auth.User{
		UID: uid, Username: username, DisplayName: displayName, Email: username + "@example.test",
		PasswordHash: "x", Role: auth.RoleUser,
	}); err != nil {
		t.Fatalf("seeding user %s: %v", uid, err)
	}
}

// seedSubject creates a person with the given uid.
func seedSubject(t *testing.T, db *database.DB, uid string) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(),
		`INSERT INTO subjects (uid, slug, name, type, project_id) VALUES ($1, $2, $3, 'person', $4)`,
		uid, uid, uid, project.BootstrapUID); err != nil {
		t.Fatalf("seeding subject %s: %v", uid, err)
	}
}

// mustAdd adds userUID to projectUID with role, approved, or fails the test.
func mustAdd(t *testing.T, store *project.Store, meta audit.Meta, projectUID, userUID string, role project.Role) {
	t.Helper()
	if _, err := store.AddMember(context.Background(), meta, projectUID, userUID, role, true); err != nil {
		t.Fatalf("AddMember(%s, %s, %s): %v", projectUID, userUID, role, err)
	}
}

// TestAddMember verifies a new membership, its audit entry, the idempotent
// second add and the typed errors for bad input.
func TestAddMember(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()
	seedUser(t, db, "us_member_a", "anna", "Anna")

	m, err := store.AddMember(ctx, meta, project.BootstrapUID, "us_member_a", project.RoleCurator, false)
	if err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if m.ProjectUID != project.BootstrapUID || m.UserUID != "us_member_a" || m.Role != project.RoleCurator ||
		m.ApprovedAt != nil || m.SubjectUID != nil || m.Username != "anna" || m.DisplayName != "Anna" ||
		m.CreatedAt.IsZero() {
		t.Errorf("added member = %+v", m)
	}

	records := auditRecords(t, db, audit.ActionProjectMemberAdd)
	if len(records) != 1 {
		t.Fatalf("audit %s = %d rows, want 1", audit.ActionProjectMemberAdd, len(records))
	}
	rec := records[0]
	if rec.TargetType != project.MemberTargetType || rec.TargetUID == nil || *rec.TargetUID != "us_member_a" ||
		rec.Details["project_uid"] != project.BootstrapUID || rec.Details["role"] != "curator" ||
		rec.Details["approved"] != false {
		t.Errorf("audit record = %+v", rec)
	}

	// A second add leaves the row as it is, says so, and records nothing.
	_, err = store.AddMember(ctx, meta, project.BootstrapUID, "us_member_a", project.RoleAdmin, true)
	if !errors.Is(err, project.ErrAlreadyMember) {
		t.Fatalf("second AddMember error = %v, want ErrAlreadyMember", err)
	}
	again, err := store.Membership(ctx, project.BootstrapUID, "us_member_a")
	if err != nil || again.Role != project.RoleCurator || again.ApprovedAt != nil {
		t.Errorf("membership after the second add = %+v, %v; want unchanged", again, err)
	}
	if n := len(auditRecords(t, db, audit.ActionProjectMemberAdd)); n != 1 {
		t.Errorf("audit rows after the second add = %d, want 1", n)
	}

	approved, err := store.AddMember(ctx, meta, mustCreate(t, store, meta, "druhy", "Druhý").UID,
		"us_member_a", project.RoleViewer, true)
	if err != nil || approved.ApprovedAt == nil {
		t.Errorf("approved AddMember = %+v, %v; want approved_at set", approved, err)
	}
}

// TestAddMember_errors verifies the typed errors of AddMember.
func TestAddMember_errors(t *testing.T) {
	store, db, meta := newStore(t)
	seedUser(t, db, "us_member_a", "anna", "Anna")

	tests := []struct {
		name       string
		projectUID string
		userUID    string
		role       project.Role
		wantErr    error
	}{
		{"unknown project", "pj_missing", "us_member_a", project.RoleViewer, project.ErrNotFound},
		{"unknown user", project.BootstrapUID, "us_missing", project.RoleViewer, project.ErrUserNotFound},
		{"maintainer is no project role", project.BootstrapUID, "us_member_a", "maintainer", project.ErrInvalidRole},
		{"empty role", project.BootstrapUID, "us_member_a", "", project.ErrInvalidRole},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.AddMember(context.Background(), meta, tt.projectUID, tt.userUID, tt.role, true)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("AddMember error = %v, want %v", err, tt.wantErr)
			}
		})
	}
	if n := len(auditRecords(t, db, audit.ActionProjectMemberAdd)); n != 0 {
		t.Errorf("failed adds recorded %d audit rows, want 0", n)
	}
}

// TestMembers verifies the listing is ordered by display name with the
// username standing in for an empty one, and the unknown/empty project cases.
func TestMembers(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()
	seedUser(t, db, "us_z", "zdena", "Zdena")
	seedUser(t, db, "us_b", "bob", "")
	seedUser(t, db, "us_a", "x_adam", "Adam")
	for _, uid := range []string{"us_z", "us_b", "us_a"} {
		mustAdd(t, store, meta, project.BootstrapUID, uid, project.RoleViewer)
	}

	members, err := store.Members(ctx, project.BootstrapUID)
	if err != nil {
		t.Fatalf("Members: %v", err)
	}
	got := make([]string, 0, len(members))
	for _, m := range members {
		got = append(got, m.UserUID)
	}
	if want := []string{"us_a", "us_b", "us_z"}; !slices.Equal(got, want) {
		t.Errorf("Members order = %v, want %v", got, want)
	}

	empty := mustCreate(t, store, meta, "prazdny", "Prázdný")
	none, err := store.Members(ctx, empty.UID)
	if err != nil || none == nil || len(none) != 0 {
		t.Errorf("Members(empty project) = %v, %v; want an empty slice", none, err)
	}
	if _, err := store.Members(ctx, "pj_missing"); !errors.Is(err, project.ErrNotFound) {
		t.Errorf("Members(unknown) error = %v, want ErrNotFound", err)
	}
}

// TestAddByIdentifier_withholdsUntilRevealed verifies an exact-match add makes
// an approved membership that reads without the account's name, last sign-in
// or picture and that the project's member list leaves out altogether — so the
// list after an add that found an account equals the list after one that found
// nobody —, that a repeated add leaves it as it is, and that RevealIdentity —
// the account entering the project — turns it into an ordinary, listed
// membership (and is a no-op on one).
func TestAddByIdentifier_withholdsUntilRevealed(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()
	seedUser(t, db, "us_a", "adam", "Adam")
	seedUser(t, db, "us_z", "zdena", "Zdena")
	mustAdd(t, store, meta, project.BootstrapUID, "us_z", project.RoleViewer)
	if _, err := db.Pool().Exec(ctx, `UPDATE users SET last_login_at = now() WHERE uid = 'us_a'`); err != nil {
		t.Fatalf("stamping a sign-in: %v", err)
	}

	before, err := store.Members(ctx, project.BootstrapUID)
	if err != nil {
		t.Fatalf("Members: %v", err)
	}
	for _, add := range []struct {
		identifier string
		want       project.AddOutcome
	}{{"nobody", project.OutcomeNoSuchAccount}, {"adam", project.OutcomeAdded}} {
		got, err := store.AddByIdentifier(ctx, meta, project.BootstrapUID, add.identifier, project.RoleEditor)
		if err != nil || got != add.want {
			t.Fatalf("AddByIdentifier(%q) = %q, %v; want %q", add.identifier, got, err, add.want)
		}
		after, err := store.Members(ctx, project.BootstrapUID)
		if err != nil {
			t.Fatalf("Members: %v", err)
		}
		if !slices.EqualFunc(before, after, func(a, b project.Member) bool { return a.UserUID == b.UserUID }) {
			t.Fatalf("Members after adding %q = %+v, want the list as before %+v", add.identifier, after, before)
		}
	}
	if got, err := store.AddByIdentifier(ctx, meta, project.BootstrapUID, "ADAM@example.test",
		project.RoleViewer); err != nil || got != project.OutcomeAlreadyMember {
		t.Fatalf("repeated AddByIdentifier = %q, %v; want already_member", got, err)
	}

	adam, err := store.Membership(ctx, project.BootstrapUID, "us_a")
	if err != nil {
		t.Fatalf("Membership(us_a): %v", err)
	}
	if !adam.IdentityWithheld || adam.Username != "" || adam.DisplayName != "" || adam.LastLoginAt != nil ||
		adam.HasPicture || adam.Role != project.RoleEditor || adam.ApprovedAt == nil {
		t.Errorf("withheld member = %+v, want an approved editor with no name, sign-in or picture", adam)
	}

	for range 2 {
		if err := store.RevealIdentity(ctx, project.BootstrapUID, "us_a"); err != nil {
			t.Fatalf("RevealIdentity: %v", err)
		}
	}
	revealed, err := store.Membership(ctx, project.BootstrapUID, "us_a")
	if err != nil || revealed.IdentityWithheld || revealed.Username != "adam" || revealed.DisplayName != "Adam" ||
		revealed.LastLoginAt == nil {
		t.Errorf("revealed member = %+v, %v; want adam with its name and sign-in", revealed, err)
	}
	if err := store.RevealIdentity(ctx, project.BootstrapUID, "us_nobody"); err != nil {
		t.Errorf("RevealIdentity(non-member) = %v, want nil", err)
	}
	listed, err := store.Members(ctx, project.BootstrapUID)
	if err != nil || len(listed) != len(before)+1 || listed[0].UserUID != "us_a" {
		t.Errorf("Members after the reveal = %+v, %v; want adam listed first by name", listed, err)
	}
}

// TestWithdrawByIdentifier verifies the blind undo of an exact-match add: it
// removes a membership whose identity is withheld, never one the project
// already sees, refuses a blank identifier, and writes the same project entry
// for every outcome while the instance entry records what really happened.
func TestWithdrawByIdentifier(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()
	seedUser(t, db, "us_a", "adam", "Adam")
	seedUser(t, db, "us_v", "vera", "Vera")
	mustAdd(t, store, meta, project.BootstrapUID, "us_v", project.RoleViewer)
	if _, err := store.AddByIdentifier(ctx, meta, project.BootstrapUID, "adam", project.RoleAdmin); err != nil {
		t.Fatalf("AddByIdentifier: %v", err)
	}

	steps := []struct {
		identifier string
		want       project.WithdrawOutcome
	}{
		{"ADAM@example.test", project.OutcomeWithdrawn},
		{"adam", project.OutcomeNotWithheld},
		{"vera", project.OutcomeNotWithheld},
		{"nobody", project.OutcomeWithdrawNoSuchAccount},
	}
	for _, step := range steps {
		got, err := store.WithdrawByIdentifier(ctx, meta, project.BootstrapUID, step.identifier)
		if err != nil || got != step.want {
			t.Errorf("WithdrawByIdentifier(%q) = %q, %v; want %q", step.identifier, got, err, step.want)
		}
	}
	if _, err := store.Membership(ctx, project.BootstrapUID, "us_a"); !errors.Is(err, project.ErrNotMember) {
		t.Errorf("adam after the withdrawal: %v, want ErrNotMember", err)
	}
	if m, err := store.Membership(ctx, project.BootstrapUID, "us_v"); err != nil || m.Role != project.RoleViewer {
		t.Errorf("vera after a withdrawal naming her = %+v, %v; want her visible membership untouched", m, err)
	}
	if _, err := store.WithdrawByIdentifier(ctx, meta, project.BootstrapUID, "  "); !errors.Is(err,
		project.ErrEmptyIdentifier) {
		t.Errorf("WithdrawByIdentifier(blank) = %v, want ErrEmptyIdentifier", err)
	}

	requests := auditRecords(t, db, audit.ActionProjectMemberWithdrawRequest)
	if len(requests) != len(steps) {
		t.Fatalf("withdraw requests audited = %d, want %d", len(requests), len(steps))
	}
	for _, rec := range requests {
		if rec.TargetUID != nil && *rec.TargetUID != "" || len(rec.Details) != 1 ||
			rec.Details["project_uid"] != project.BootstrapUID {
			t.Errorf("project withdraw entry = %+v, want only the project", rec)
		}
	}
	resolves := auditRecords(t, db, audit.ActionProjectMemberWithdrawResolve)
	outcomes := make([]string, 0, len(resolves))
	for _, rec := range resolves {
		if rec.ActorUID != nil && *rec.ActorUID != "" {
			t.Errorf("instance withdraw entry %+v has an actor", rec)
		}
		outcome, _ := rec.Details["outcome"].(string)
		outcomes = append(outcomes, outcome)
	}
	slices.Sort(outcomes)
	if want := []string{"no_such_account", "not_withheld", "not_withheld", "withdrawn"}; !slices.Equal(outcomes, want) {
		t.Errorf("audited outcomes = %v, want %v", outcomes, want)
	}
}

// TestLastAdminGuard_ignoresWithheldAdmins verifies an admin membership whose
// identity is withheld is no working admin, whether its account is enabled or
// disabled: the sole admin's removal is refused either way, so its answer does
// not tell the hidden account's state.
func TestLastAdminGuard_ignoresWithheldAdmins(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()
	seedUser(t, db, "us_admin", "admin", "Admin")
	seedUser(t, db, "us_hidden", "hidden", "Hidden")
	mustAdd(t, store, meta, project.BootstrapUID, "us_admin", project.RoleAdmin)
	if _, err := store.AddByIdentifier(ctx, meta, project.BootstrapUID, "hidden", project.RoleAdmin); err != nil {
		t.Fatalf("AddByIdentifier: %v", err)
	}

	for _, disabled := range []bool{false, true} {
		if _, err := db.Pool().Exec(ctx, `UPDATE users SET disabled = $2 WHERE uid = $1`, "us_hidden",
			disabled); err != nil {
			t.Fatalf("setting disabled: %v", err)
		}
		if err := store.RemoveMember(ctx, meta, project.BootstrapUID, "us_admin"); !errors.Is(err,
			project.ErrLastAdmin) {
			t.Errorf("removing the sole admin beside a withheld admin (disabled %v) = %v, want ErrLastAdmin",
				disabled, err)
		}
	}
}

// TestMembership_notMember verifies ErrNotMember for a non-member, an unknown
// account and an unknown project.
func TestMembership_notMember(t *testing.T) {
	store, db, _ := newStore(t)
	seedUser(t, db, "us_member_a", "anna", "Anna")

	for _, tt := range []struct{ projectUID, userUID string }{
		{project.BootstrapUID, "us_member_a"},
		{project.BootstrapUID, "us_missing"},
		{"pj_missing", "us_member_a"},
	} {
		if _, err := store.Membership(context.Background(), tt.projectUID, tt.userUID); !errors.Is(err,
			project.ErrNotMember) {
			t.Errorf("Membership(%s, %s) error = %v, want ErrNotMember", tt.projectUID, tt.userUID, err)
		}
	}
}

// TestMembershipsOf verifies an account's projects come ordered by name with
// their description, role, approval and active flag.
func TestMembershipsOf(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()
	seedUser(t, db, "us_member_a", "anna", "Anna")
	alpha := mustCreate(t, store, meta, "alfa", "Alfa")
	inactive, description := false, "Spolkové fotky"
	if _, err := store.Update(ctx, meta, alpha.UID, project.UpdatePatch{Active: &inactive,
		Description: &description}); err != nil {
		t.Fatalf("deactivating: %v", err)
	}
	mustAdd(t, store, meta, project.BootstrapUID, "us_member_a", project.RoleEditor)
	if _, err := store.AddMember(ctx, meta, alpha.UID, "us_member_a", project.RoleViewer, false); err != nil {
		t.Fatalf("AddMember(alfa): %v", err)
	}

	got, err := store.MembershipsOf(ctx, "us_member_a")
	if err != nil {
		t.Fatalf("MembershipsOf: %v", err)
	}
	want := []project.MembershipSummary{
		{
			ProjectUID: alpha.UID, Slug: "alfa", Name: "Alfa", Description: "Spolkové fotky", Active: false,
			Role: project.RoleViewer,
		},
		{
			ProjectUID: project.BootstrapUID, Slug: project.BootstrapSlug, Name: "Veselice", Active: true,
			Role: project.RoleEditor, Approved: true,
		},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("MembershipsOf = %+v, want %+v", got, want)
	}

	none, err := store.MembershipsOf(ctx, "us_missing")
	if err != nil || none == nil || len(none) != 0 {
		t.Errorf("MembershipsOf(unknown) = %v, %v; want an empty slice", none, err)
	}
}

// TestSetRole verifies a role change and its audit, the silent no-op, and the
// typed errors — the last-admin guard above all.
func TestSetRole(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()
	seedUser(t, db, "us_admin_1", "admin1", "Admin 1")
	seedUser(t, db, "us_admin_2", "admin2", "Admin 2")
	seedUser(t, db, "us_member_a", "anna", "Anna")
	mustAdd(t, store, meta, project.BootstrapUID, "us_admin_1", project.RoleAdmin)
	mustAdd(t, store, meta, project.BootstrapUID, "us_member_a", project.RoleViewer)

	m, err := store.SetRole(ctx, meta, project.BootstrapUID, "us_member_a", project.RoleEditor)
	if err != nil || m.Role != project.RoleEditor {
		t.Fatalf("SetRole(editor) = %+v, %v", m, err)
	}
	records := auditRecords(t, db, audit.ActionProjectMemberRole)
	if len(records) != 1 || records[0].Details["role"] != "editor" ||
		records[0].Details["previous_role"] != "viewer" || records[0].Details["project_uid"] != project.BootstrapUID {
		t.Fatalf("role audit = %+v", records)
	}
	if _, err := store.SetRole(ctx, meta, project.BootstrapUID, "us_member_a", project.RoleEditor); err != nil {
		t.Fatalf("SetRole(same): %v", err)
	}
	if n := len(auditRecords(t, db, audit.ActionProjectMemberRole)); n != 1 {
		t.Errorf("a no-op role change recorded an audit row (%d rows)", n)
	}

	// The only admin cannot step down …
	_, err = store.SetRole(ctx, meta, project.BootstrapUID, "us_admin_1", project.RoleEditor)
	if !errors.Is(err, project.ErrLastAdmin) {
		t.Fatalf("demoting the only admin error = %v, want ErrLastAdmin", err)
	}
	// … until a second one exists; then the second is the last.
	mustAdd(t, store, meta, project.BootstrapUID, "us_admin_2", project.RoleAdmin)
	if _, err := store.SetRole(ctx, meta, project.BootstrapUID, "us_admin_1", project.RoleEditor); err != nil {
		t.Fatalf("demoting one of two admins: %v", err)
	}
	if _, err := store.SetRole(ctx, meta, project.BootstrapUID, "us_admin_2",
		project.RoleViewer); !errors.Is(err, project.ErrLastAdmin) {
		t.Errorf("demoting the remaining admin error = %v, want ErrLastAdmin", err)
	}

	for _, tt := range []struct {
		name, projectUID, userUID string
		role                      project.Role
		wantErr                   error
	}{
		{"not a member", project.BootstrapUID, "us_missing", project.RoleViewer, project.ErrNotMember},
		{"unknown project", "pj_missing", "us_member_a", project.RoleViewer, project.ErrNotFound},
		{"invalid role", project.BootstrapUID, "us_member_a", "maintainer", project.ErrInvalidRole},
	} {
		if _, err := store.SetRole(ctx, meta, tt.projectUID, tt.userUID, tt.role); !errors.Is(err, tt.wantErr) {
			t.Errorf("%s: SetRole error = %v, want %v", tt.name, err, tt.wantErr)
		}
	}
}

// TestApprove verifies approval, its audit, the idempotent re-approval and
// ErrNotMember.
func TestApprove(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()
	seedUser(t, db, "us_member_a", "anna", "Anna")
	if _, err := store.AddMember(ctx, meta, project.BootstrapUID, "us_member_a", project.RoleViewer,
		false); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	m, err := store.Approve(ctx, meta, project.BootstrapUID, "us_member_a")
	if err != nil || m.ApprovedAt == nil {
		t.Fatalf("Approve = %+v, %v; want approved_at set", m, err)
	}
	records := auditRecords(t, db, audit.ActionProjectMemberApprove)
	if len(records) != 1 || records[0].Details["role"] != "viewer" ||
		records[0].Details["project_uid"] != project.BootstrapUID {
		t.Fatalf("approve audit = %+v", records)
	}

	again, err := store.Approve(ctx, meta, project.BootstrapUID, "us_member_a")
	if err != nil || again.ApprovedAt == nil || !again.ApprovedAt.Equal(*m.ApprovedAt) {
		t.Errorf("re-Approve = %+v, %v; want the original approval time", again, err)
	}
	if n := len(auditRecords(t, db, audit.ActionProjectMemberApprove)); n != 1 {
		t.Errorf("re-approval recorded an audit row (%d rows)", n)
	}
	if _, err := store.Approve(ctx, meta, project.BootstrapUID, "us_missing"); !errors.Is(err,
		project.ErrNotMember) {
		t.Errorf("Approve(non-member) error = %v, want ErrNotMember", err)
	}
}

// TestRemoveMember verifies removal and its audit, the last-admin guard and
// ErrNotMember.
func TestRemoveMember(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()
	seedUser(t, db, "us_admin_1", "admin1", "Admin 1")
	seedUser(t, db, "us_member_a", "anna", "Anna")
	mustAdd(t, store, meta, project.BootstrapUID, "us_admin_1", project.RoleAdmin)
	mustAdd(t, store, meta, project.BootstrapUID, "us_member_a", project.RoleCurator)

	if err := store.RemoveMember(ctx, meta, project.BootstrapUID, "us_member_a"); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	if _, err := store.Membership(ctx, project.BootstrapUID, "us_member_a"); !errors.Is(err,
		project.ErrNotMember) {
		t.Errorf("membership after removal error = %v, want ErrNotMember", err)
	}
	records := auditRecords(t, db, audit.ActionProjectMemberRemove)
	if len(records) != 1 || records[0].Details["role"] != "curator" ||
		records[0].Details["project_uid"] != project.BootstrapUID || *records[0].TargetUID != "us_member_a" {
		t.Fatalf("remove audit = %+v", records)
	}

	if err := store.RemoveMember(ctx, meta, project.BootstrapUID, "us_admin_1"); !errors.Is(err,
		project.ErrLastAdmin) {
		t.Errorf("removing the only admin error = %v, want ErrLastAdmin", err)
	}
	if err := store.RemoveMember(ctx, meta, project.BootstrapUID, "us_member_a"); !errors.Is(err,
		project.ErrNotMember) {
		t.Errorf("removing a non-member error = %v, want ErrNotMember", err)
	}
	if n := len(auditRecords(t, db, audit.ActionProjectMemberRemove)); n != 1 {
		t.Errorf("refused removals recorded audit rows (%d rows)", n)
	}
}

// TestSetSubject verifies setting and clearing the "this is me" person, the
// audit, the no-op, ErrSubjectNotFound (a uid naming nothing and another
// project's subject alike) and the ON DELETE SET NULL of the link.
func TestSetSubject(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()
	seedUser(t, db, "us_member_a", "anna", "Anna")
	seedSubject(t, db, "sbj_anna")
	mustAdd(t, store, meta, project.BootstrapUID, "us_member_a", project.RoleViewer)

	subject := "sbj_anna"
	m, err := store.SetSubject(ctx, meta, project.BootstrapUID, "us_member_a", &subject)
	if err != nil || m.SubjectUID == nil || *m.SubjectUID != subject {
		t.Fatalf("SetSubject = %+v, %v", m, err)
	}
	records := auditRecords(t, db, audit.ActionProjectMemberSubject)
	if len(records) != 1 || records[0].Details["subject_uid"] != subject ||
		records[0].Details["previous_subject_uid"] != nil || records[0].Details["role"] != "viewer" {
		t.Fatalf("subject audit = %+v", records)
	}
	if _, err := store.SetSubject(ctx, meta, project.BootstrapUID, "us_member_a", &subject); err != nil {
		t.Fatalf("SetSubject(same): %v", err)
	}
	if n := len(auditRecords(t, db, audit.ActionProjectMemberSubject)); n != 1 {
		t.Errorf("a no-op subject change recorded an audit row (%d rows)", n)
	}

	missing := "sbj_missing"
	if _, err := store.SetSubject(ctx, meta, project.BootstrapUID, "us_member_a", &missing); !errors.Is(err,
		project.ErrSubjectNotFound) {
		t.Errorf("SetSubject(unknown subject) error = %v, want ErrSubjectNotFound", err)
	}
	foreign := seedForeignSubject(t, store, db, meta)
	if _, err := store.SetSubject(ctx, meta, project.BootstrapUID, "us_member_a", &foreign); !errors.Is(err,
		project.ErrSubjectNotFound) {
		t.Errorf("SetSubject(another project's subject) error = %v, want ErrSubjectNotFound", err)
	}
	if n := len(auditRecords(t, db, audit.ActionProjectMemberSubject)); n != 1 {
		t.Errorf("refused subject changes recorded audit rows (%d rows, want 1)", n)
	}
	if _, err := store.SetSubject(ctx, meta, project.BootstrapUID, "us_missing", &subject); !errors.Is(err,
		project.ErrNotMember) {
		t.Errorf("SetSubject(non-member) error = %v, want ErrNotMember", err)
	}

	// Deleting the person unlinks the membership instead of failing or deleting it.
	if _, err := db.Pool().Exec(ctx, `DELETE FROM subjects WHERE uid = $1`, subject); err != nil {
		t.Fatalf("deleting the subject: %v", err)
	}
	after, err := store.Membership(ctx, project.BootstrapUID, "us_member_a")
	if err != nil || after.SubjectUID != nil {
		t.Errorf("membership after deleting its subject = %+v, %v; want subject nil", after, err)
	}

	cleared, err := store.SetSubject(ctx, meta, project.BootstrapUID, "us_member_a", nil)
	if err != nil || cleared.SubjectUID != nil {
		t.Errorf("SetSubject(nil) = %+v, %v", cleared, err)
	}
}

// seedForeignSubject creates a second project holding one person and returns
// that person's uid — a subject that exists, just not in the bootstrap project.
func seedForeignSubject(t *testing.T, store *project.Store, db *database.DB, meta audit.Meta) string {
	t.Helper()
	ctx := context.Background()
	other, err := store.Create(ctx, meta, project.CreateParams{Slug: "cizi", Name: "Cizí"})
	if err != nil {
		t.Fatalf("creating the foreign project: %v", err)
	}
	const subjectUID = "sbj_foreign"
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO subjects (uid, slug, name, type, project_id) VALUES ($1, $2, $3, 'person', $4)`,
		subjectUID, subjectUID, subjectUID, other.UID); err != nil {
		t.Fatalf("seeding the foreign subject: %v", err)
	}
	return subjectUID
}

// TestTouchLastProject verifies the last project is remembered without an audit
// entry, and the typed errors.
func TestTouchLastProject(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()
	seedUser(t, db, "us_member_a", "anna", "Anna")
	other := mustCreate(t, store, meta, "jiny", "Jiný")

	if err := store.TouchLastProject(ctx, "us_member_a", other.UID); err != nil {
		t.Fatalf("TouchLastProject: %v", err)
	}
	var last *string
	if err := db.Pool().QueryRow(ctx, `SELECT last_project_uid FROM users WHERE uid = 'us_member_a'`).
		Scan(&last); err != nil {
		t.Fatalf("reading last_project_uid: %v", err)
	}
	if last == nil || *last != other.UID {
		t.Errorf("last_project_uid = %v, want %s", last, other.UID)
	}
	if err := store.TouchLastProject(ctx, "us_missing", other.UID); !errors.Is(err, project.ErrUserNotFound) {
		t.Errorf("TouchLastProject(unknown user) error = %v, want ErrUserNotFound", err)
	}
	if err := store.TouchLastProject(ctx, "us_member_a", "pj_missing"); !errors.Is(err, project.ErrNotFound) {
		t.Errorf("TouchLastProject(unknown project) error = %v, want ErrNotFound", err)
	}
	records, err := audit.NewStore(db.Pool()).List(ctx, audit.Filter{Limit: 100})
	if err != nil {
		t.Fatalf("listing audit: %v", err)
	}
	for _, rec := range records {
		if rec.TargetUID != nil && *rec.TargetUID == "us_member_a" {
			t.Errorf("TouchLastProject was audited: %+v", rec)
		}
	}
}

// TestMembership_cascades verifies deleting the account or the project removes
// its memberships.
func TestMembership_cascades(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()
	seedUser(t, db, "us_member_a", "anna", "Anna")
	seedUser(t, db, "us_member_b", "bara", "Bára")
	other := mustCreate(t, store, meta, "jiny", "Jiný")
	mustAdd(t, store, meta, project.BootstrapUID, "us_member_a", project.RoleViewer)
	mustAdd(t, store, meta, other.UID, "us_member_b", project.RoleViewer)

	if _, err := db.Pool().Exec(ctx, `DELETE FROM users WHERE uid = 'us_member_a'`); err != nil {
		t.Fatalf("deleting the user: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `DELETE FROM projects WHERE uid = $1`, other.UID); err != nil {
		t.Fatalf("deleting the project: %v", err)
	}
	var left int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM project_members`).Scan(&left); err != nil {
		t.Fatalf("counting memberships: %v", err)
	}
	if left != 0 {
		t.Errorf("%d membership(s) survived their account or project", left)
	}
}

// disableUser marks the account uid disabled, as a maintainer's disable does.
func disableUser(t *testing.T, db *database.DB, uid string) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(),
		`UPDATE users SET disabled = true WHERE uid = $1`, uid); err != nil {
		t.Fatalf("disabling %s: %v", uid, err)
	}
}

// TestLastAdminGuard_countsOnlyWorkingAdmins verifies the last-admin guard
// counts only approved admins whose account is not disabled: a pending or a
// disabled "admin" row does not let the last working admin step down or be
// removed, while two working admins may lose one, and taking the role from a
// non-working admin is always allowed.
func TestLastAdminGuard_countsOnlyWorkingAdmins(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()
	seedUser(t, db, "us_working", "working", "Working")
	seedUser(t, db, "us_pending", "pending", "Pending")
	seedUser(t, db, "us_disabled", "disabled", "Disabled")
	mustAdd(t, store, meta, project.BootstrapUID, "us_working", project.RoleAdmin)
	if _, err := store.AddMember(ctx, meta, project.BootstrapUID, "us_pending", project.RoleAdmin,
		false); err != nil {
		t.Fatalf("AddMember(pending admin): %v", err)
	}
	mustAdd(t, store, meta, project.BootstrapUID, "us_disabled", project.RoleAdmin)
	disableUser(t, db, "us_disabled")

	// (a) one approved admin plus a pending one: demoting the approved one fails.
	if _, err := store.SetRole(ctx, meta, project.BootstrapUID, "us_working",
		project.RoleEditor); !errors.Is(err, project.ErrLastAdmin) {
		t.Errorf("demoting the last working admin beside a pending one: error = %v, want ErrLastAdmin", err)
	}
	// (b) one approved admin plus a disabled one: removing the approved one fails.
	if err := store.RemoveMember(ctx, meta, project.BootstrapUID, "us_working"); !errors.Is(err,
		project.ErrLastAdmin) {
		t.Errorf("removing the last working admin beside a disabled one: error = %v, want ErrLastAdmin", err)
	}
	if m, err := store.Membership(ctx, project.BootstrapUID, "us_working"); err != nil || m.Role != project.RoleAdmin {
		t.Fatalf("working admin after the refusals = %+v, %v; want still admin", m, err)
	}
	for _, action := range []string{audit.ActionProjectMemberRole, audit.ActionProjectMemberRemove} {
		if n := len(auditRecords(t, db, action)); n != 0 {
			t.Errorf("refused changes recorded %d %s audit rows", n, action)
		}
	}

	// Neither the pending nor the disabled admin governs anything, so taking
	// their role away never needs another admin.
	if _, err := store.SetRole(ctx, meta, project.BootstrapUID, "us_pending", project.RoleViewer); err != nil {
		t.Errorf("demoting a pending admin: %v", err)
	}
	if err := store.RemoveMember(ctx, meta, project.BootstrapUID, "us_disabled"); err != nil {
		t.Errorf("removing a disabled admin: %v", err)
	}

	// (c) two approved admins: demoting one succeeds, then the other is the last.
	if _, err := store.Approve(ctx, meta, project.BootstrapUID, "us_pending"); err != nil {
		t.Fatalf("Approve(us_pending): %v", err)
	}
	if _, err := store.SetRole(ctx, meta, project.BootstrapUID, "us_pending", project.RoleAdmin); err != nil {
		t.Fatalf("promoting the second admin: %v", err)
	}
	if _, err := store.SetRole(ctx, meta, project.BootstrapUID, "us_working", project.RoleEditor); err != nil {
		t.Errorf("demoting one of two working admins: %v", err)
	}
	if _, err := store.SetRole(ctx, meta, project.BootstrapUID, "us_pending",
		project.RoleEditor); !errors.Is(err, project.ErrLastAdmin) {
		t.Errorf("demoting the remaining working admin: error = %v, want ErrLastAdmin", err)
	}
}
