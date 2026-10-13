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
// their role, approval and active flag.
func TestMembershipsOf(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()
	seedUser(t, db, "us_member_a", "anna", "Anna")
	alpha := mustCreate(t, store, meta, "alfa", "Alfa")
	inactive := false
	if _, err := store.Update(ctx, meta, alpha.UID, project.UpdatePatch{Active: &inactive}); err != nil {
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
		{ProjectUID: alpha.UID, Slug: "alfa", Name: "Alfa", Active: false, Role: project.RoleViewer},
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
// audit, the no-op, ErrSubjectNotFound and the ON DELETE SET NULL of the link.
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
