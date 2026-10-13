//go:build integration

package project_test

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/project"
)

// auditProjectIDs returns the project_id of every audit row recorded for
// action, oldest first ("" for a NULL).
func auditProjectIDs(t *testing.T, db *database.DB, action string) []string {
	t.Helper()
	rows, err := db.Pool().Query(context.Background(),
		`SELECT COALESCE(project_id, '') FROM audit_log WHERE action = $1 ORDER BY id`, action)
	if err != nil {
		t.Fatalf("reading audit rows of %s: %v", action, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scanning an audit row: %v", err)
		}
		out = append(out, id)
	}
	return out
}

// TestCreate_seedsSettingsAndCreator verifies a created project gets its
// settings row with the closed defaults, and that a creator becomes its
// approved admin with an audit row — both rows stamped with the new project.
func TestCreate_seedsSettingsAndCreator(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()

	created, err := store.Create(ctx, meta, project.CreateParams{Slug: "klub", Name: "Klub", CreatorUID: actorUID})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	var enabled bool
	var secret, welcome string
	if err := db.Pool().QueryRow(ctx, `SELECT registration_enabled, registration_secret, welcome_markdown
		FROM instance_settings WHERE project_id = $1`, created.UID).Scan(&enabled, &secret, &welcome); err != nil ||
		enabled || secret != "" || welcome != "" {
		t.Errorf("settings row = %v %q %q, %v; want registration closed and empty texts", enabled, secret, welcome, err)
	}
	member, err := store.Membership(ctx, created.UID, actorUID)
	if err != nil || member.Role != project.RoleAdmin || member.ApprovedAt == nil {
		t.Errorf("creator membership = %+v, %v; want an approved admin", member, err)
	}
	for _, action := range []string{audit.ActionProjectCreate, audit.ActionProjectMemberAdd} {
		if got := auditProjectIDs(t, db, action); len(got) != 1 || got[0] != created.UID {
			t.Errorf("%s project ids = %v, want [%s]", action, got, created.UID)
		}
	}

	bare := mustCreate(t, store, meta, "prazdny", "Prázdný")
	if members, err := store.Members(ctx, bare.UID); err != nil || len(members) != 0 {
		t.Errorf("members of a project created without a creator = %+v, %v; want none", members, err)
	}
}

// TestCreate_unknownCreator verifies a creator naming no account rolls the
// whole creation back with ErrUserNotFound.
func TestCreate_unknownCreator(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()

	_, err := store.Create(ctx, meta, project.CreateParams{Slug: "nikdo", Name: "Nikdo", CreatorUID: "usnobody"})
	if !errors.Is(err, project.ErrUserNotFound) {
		t.Fatalf("Create with an unknown creator = %v, want ErrUserNotFound", err)
	}
	if _, err := store.GetBySlug(ctx, "nikdo"); !errors.Is(err, project.ErrNotFound) {
		t.Errorf("GetBySlug after the failed create = %v, want ErrNotFound", err)
	}
	if got := auditProjectIDs(t, db, audit.ActionProjectCreate); len(got) != 0 {
		t.Errorf("project.create rows = %v, want none", got)
	}
}

// TestMutations_stampTheProject verifies every membership and project
// mutation's audit row carries the project it touched.
func TestMutations_stampTheProject(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()
	seedUser(t, db, "usstamp", "stamp", "")
	p := mustCreate(t, store, meta, "razitko", "Razítko")

	mustAdd(t, store, meta, p.UID, actorUID, project.RoleAdmin)
	if _, err := store.AddMember(ctx, meta, p.UID, "usstamp", project.RoleViewer, false); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if _, err := store.Approve(ctx, meta, p.UID, "usstamp"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if _, err := store.SetRole(ctx, meta, p.UID, "usstamp", project.RoleEditor); err != nil {
		t.Fatalf("SetRole: %v", err)
	}
	if err := store.RemoveMember(ctx, meta, p.UID, "usstamp"); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	name := "Nové"
	if _, err := store.Update(ctx, meta, p.UID, project.UpdatePatch{Name: &name}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, err := store.SetStoragePrefix(ctx, meta, p.UID, "projects/x/"); err != nil {
		t.Fatalf("SetStoragePrefix: %v", err)
	}
	for _, action := range []string{
		audit.ActionProjectCreate, audit.ActionProjectMemberApprove, audit.ActionProjectMemberRole,
		audit.ActionProjectMemberRemove, audit.ActionProjectUpdate, audit.ActionProjectStoragePrefix,
	} {
		if got := auditProjectIDs(t, db, action); len(got) != 1 || got[0] != p.UID {
			t.Errorf("%s project ids = %v, want [%s]", action, got, p.UID)
		}
	}
	if got := auditProjectIDs(t, db, audit.ActionProjectMemberAdd); len(got) != 2 || got[0] != p.UID || got[1] != p.UID {
		t.Errorf("project.member.add project ids = %v, want two rows of %s", got, p.UID)
	}
}

// TestOverviews verifies the per-project counts: members, photos (archived
// included) and their bytes, each project counting only its own rows.
func TestOverviews(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()
	seedUser(t, db, "usover", "over", "")
	other := mustCreate(t, store, meta, "druhy", "Druhý")
	mustAdd(t, store, meta, other.UID, actorUID, project.RoleAdmin)
	mustAdd(t, store, meta, other.UID, "usover", project.RoleViewer)
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO photos (uid, project_id, file_hash, file_path, file_size, archived_at) VALUES
		('phover00000000000000001', $1, 'o1', 'a.jpg', 100, NULL),
		('phover00000000000000002', $1, 'o2', 'b.jpg', 50, now()),
		('phover00000000000000003', $2, 'o3', 'c.jpg', 7, NULL)`, project.BootstrapUID, other.UID); err != nil {
		t.Fatalf("seeding photos: %v", err)
	}

	list, err := store.Overviews(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("Overviews = %+v, %v; want two projects", list, err)
	}
	// Ordered by name: "Druhý" before "Veselice".
	if got := list[0]; got.UID != other.UID || got.MembersCount != 2 || got.PhotosCount != 1 || got.OriginalsBytes != 7 {
		t.Errorf("other overview = %+v", got)
	}
	if got := list[1]; got.UID != project.BootstrapUID || got.MembersCount != 0 || got.PhotosCount != 2 ||
		got.OriginalsBytes != 150 {
		t.Errorf("bootstrap overview = %+v", got)
	}

	one, err := store.OverviewOf(ctx, other.UID)
	if err != nil || one.Slug != "druhy" || one.MembersCount != 2 {
		t.Errorf("OverviewOf = %+v, %v", one, err)
	}
	if _, err := store.OverviewOf(ctx, "pjnope"); !errors.Is(err, project.ErrNotFound) {
		t.Errorf("OverviewOf(unknown) = %v, want ErrNotFound", err)
	}
}

// TestAllMemberships verifies the instance-wide membership map: every account
// with its projects in name order, none for an account without memberships.
func TestAllMemberships(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()
	seedUser(t, db, "usall", "all", "")
	seedUser(t, db, "uslone", "lone", "")
	other := mustCreate(t, store, meta, "aaa", "Aaa")
	mustAdd(t, store, meta, project.BootstrapUID, "usall", project.RoleEditor)
	if _, err := store.AddMember(ctx, meta, other.UID, "usall", project.RoleViewer, false); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	all, err := store.AllMemberships(ctx)
	if err != nil {
		t.Fatalf("AllMemberships: %v", err)
	}
	got := all["usall"]
	if len(got) != 2 || got[0].Slug != "aaa" || got[0].Approved || got[1].Slug != project.BootstrapSlug ||
		got[1].Role != project.RoleEditor || !got[1].Approved {
		t.Errorf("memberships of usall = %+v", got)
	}
	if _, ok := all["uslone"]; ok {
		t.Error("an account without memberships has a key")
	}
}
