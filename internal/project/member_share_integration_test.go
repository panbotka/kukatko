//go:build integration

package project_test

import (
	"context"
	"testing"

	"github.com/panbotka/kukatko/internal/project"
)

// TestSharesProject verifies the profile-visibility rule: two accounts share a
// project only as approved members, neither withheld, of one active project —
// the named one when a project is given, any otherwise. A pending member, a
// withheld one, a member of another project, a deactivated shared project and
// an unknown uid all answer false.
func TestSharesProject(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()
	other, err := store.Create(ctx, meta, project.CreateParams{Slug: "jinde", Name: "Jinde"})
	if err != nil {
		t.Fatalf("creating the other project: %v", err)
	}
	closed, err := store.Create(ctx, meta, project.CreateParams{Slug: "zavreny", Name: "Zavřený"})
	if err != nil {
		t.Fatalf("creating the closed project: %v", err)
	}
	for _, uid := range []string{"us_a", "us_a2", "us_b", "us_ab", "us_pending", "us_withheld", "us_c1", "us_c2"} {
		seedUser(t, db, uid, uid, uid)
	}
	mustAdd(t, store, meta, project.BootstrapUID, "us_a", project.RoleViewer)
	mustAdd(t, store, meta, project.BootstrapUID, "us_a2", project.RoleViewer)
	mustAdd(t, store, meta, other.UID, "us_b", project.RoleViewer)
	mustAdd(t, store, meta, project.BootstrapUID, "us_ab", project.RoleViewer)
	mustAdd(t, store, meta, other.UID, "us_ab", project.RoleViewer)
	if _, err := store.AddMember(ctx, meta, project.BootstrapUID, "us_pending", project.RoleViewer, false); err != nil {
		t.Fatalf("adding the pending member: %v", err)
	}
	if _, err := store.AddByIdentifier(ctx, meta, project.BootstrapUID, "us_withheld", project.RoleViewer); err != nil {
		t.Fatalf("adding the withheld member: %v", err)
	}
	mustAdd(t, store, meta, closed.UID, "us_c1", project.RoleViewer)
	mustAdd(t, store, meta, closed.UID, "us_c2", project.RoleViewer)
	inactive := false
	if _, err := store.Update(ctx, meta, closed.UID, project.UpdatePatch{Active: &inactive}); err != nil {
		t.Fatalf("deactivating the closed project: %v", err)
	}

	tests := []struct {
		name            string
		project         string
		viewer, account string
		want            bool
	}{
		{"members of one project", "", "us_a", "us_a2", true},
		{"members of that project, asked there", project.BootstrapUID, "us_a", "us_a2", true},
		{"members of one project, asked in another", other.UID, "us_a", "us_a2", false},
		{"the shared project is the other one", "", "us_ab", "us_b", true},
		{"asked in the project the account is not in", project.BootstrapUID, "us_ab", "us_b", false},
		{"members of different projects", "", "us_a", "us_b", false},
		{"a pending account", "", "us_a", "us_pending", false},
		{"a pending viewer", "", "us_pending", "us_a", false},
		{"a withheld account", "", "us_a", "us_withheld", false},
		{"a withheld viewer", "", "us_withheld", "us_a", false},
		{"a deactivated shared project", "", "us_c1", "us_c2", false},
		{"no such account", "", "us_a", "us_nobody", false},
		{"one member with itself", project.BootstrapUID, "us_a", "us_a", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.SharesProject(ctx, tt.project, tt.viewer, tt.account)
			if err != nil {
				t.Fatalf("SharesProject: %v", err)
			}
			if got != tt.want {
				t.Errorf("SharesProject(%q, %s, %s) = %v, want %v", tt.project, tt.viewer, tt.account, got, tt.want)
			}
		})
	}
}

// TestMembership_aPendingMemberPromisesNoPicture verifies has_picture follows
// the rule GET /users/{uid}/avatar applies: an account with an uploaded
// picture reads without one while its membership is pending — the avatar
// route would refuse its readers — and with one once it is approved.
func TestMembership_aPendingMemberPromisesNoPicture(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()
	seedUser(t, db, "us_p", "pavel", "Pavel")
	const seedPicture = `INSERT INTO user_pictures (user_uid, kind, image)
	VALUES ('us_p', 'upload', '\xffd8'::bytea)`
	if _, err := db.Pool().Exec(ctx, seedPicture); err != nil {
		t.Fatalf("seeding the picture: %v", err)
	}
	if _, err := store.AddMember(ctx, meta, project.BootstrapUID, "us_p", project.RoleViewer, false); err != nil {
		t.Fatalf("adding the pending member: %v", err)
	}
	pending, err := store.Membership(ctx, project.BootstrapUID, "us_p")
	if err != nil || pending.HasPicture {
		t.Fatalf("pending membership = %+v, %v; want no picture", pending, err)
	}
	if _, err := store.Approve(ctx, meta, project.BootstrapUID, "us_p"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	approved, err := store.Membership(ctx, project.BootstrapUID, "us_p")
	if err != nil || !approved.HasPicture {
		t.Fatalf("approved membership = %+v, %v; want the picture", approved, err)
	}
}
