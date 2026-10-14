//go:build integration

package project_test

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/project"
)

// TestRequireApprovedMembers verifies the one "may be named in this project"
// check: approved members pass (repeats and an empty list too), while a pending
// member, a member of another project only, a maintainer outside the project,
// an exact-match add that has not entered yet and a uid with no account are all
// the same ErrNotMember.
func TestRequireApprovedMembers(t *testing.T) {
	store, db, meta := newStore(t)
	ctx := context.Background()
	other, err := store.Create(ctx, meta, project.CreateParams{Slug: "jinde", Name: "Jinde"})
	if err != nil {
		t.Fatalf("creating the other project: %v", err)
	}
	for _, uid := range []string{"us_ok", "us_ok2", "us_pending", "us_elsewhere", "us_admin", "us_withheld"} {
		seedUser(t, db, uid, uid, uid)
	}
	mustAdd(t, store, meta, project.BootstrapUID, "us_ok", project.RoleViewer)
	mustAdd(t, store, meta, project.BootstrapUID, "us_ok2", project.RoleAdmin)
	if _, err := store.AddMember(ctx, meta, project.BootstrapUID, "us_pending", project.RoleViewer, false); err != nil {
		t.Fatalf("adding the pending member: %v", err)
	}
	mustAdd(t, store, meta, other.UID, "us_elsewhere", project.RoleAdmin)
	if _, err := store.AddByIdentifier(ctx, meta, project.BootstrapUID, "us_withheld", project.RoleViewer); err != nil {
		t.Fatalf("adding the withheld member: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `UPDATE users SET role = 'maintainer' WHERE uid = 'us_admin'`); err != nil {
		t.Fatalf("promoting the maintainer: %v", err)
	}

	tests := []struct {
		name    string
		uids    []string
		wantErr error
	}{
		{name: "nobody asked", uids: nil},
		{name: "approved members, one repeated", uids: []string{"us_ok", "us_ok2", "us_ok"}},
		{name: "pending member", uids: []string{"us_ok", "us_pending"}, wantErr: project.ErrNotMember},
		{name: "member of another project", uids: []string{"us_elsewhere"}, wantErr: project.ErrNotMember},
		{name: "maintainer outside the project", uids: []string{"us_admin"}, wantErr: project.ErrNotMember},
		{name: "member whose identity is withheld", uids: []string{"us_withheld"}, wantErr: project.ErrNotMember},
		{name: "no such account", uids: []string{"us_nobody"}, wantErr: project.ErrNotMember},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := project.RequireApprovedMembers(ctx, db.Pool(), project.BootstrapUID, tt.uids...)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("RequireApprovedMembers(%v) = %v, want %v", tt.uids, err, tt.wantErr)
			}
		})
	}
}
