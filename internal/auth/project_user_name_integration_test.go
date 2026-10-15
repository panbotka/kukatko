//go:build integration

package auth_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// TestProjectUserName_formerMember verifies a project names an account only
// while it belongs to the project: A's member is named in A, a nameless former
// member in B (which it never joined) and in A once it has left; an unknown
// uid is ErrUserNotFound.
func TestProjectUserName_formerMember(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := projecttest.Seed(t, db.Pool())
	store := auth.NewStore(db.Pool())
	ctx := t.Context()

	if name, former, err := store.ProjectUserName(ctx, f.A.ProjectUID, f.A.UserUID); err != nil || former || name == "" {
		t.Errorf("A's member in A = %q former=%v, %v; want named", name, former, err)
	}
	if name, former, err := store.ProjectUserName(ctx, f.B.ProjectUID, f.A.UserUID); err != nil || !former || name != "" {
		t.Errorf("A's member in B = %q former=%v, %v; want a nameless former member", name, former, err)
	}
	if _, err := db.Pool().Exec(ctx, `DELETE FROM project_members WHERE project_uid = $1 AND user_uid = $2`,
		f.A.ProjectUID, f.A.UserUID); err != nil {
		t.Fatalf("removing A's member: %v", err)
	}
	if name, former, err := store.ProjectUserName(ctx, f.A.ProjectUID, f.A.UserUID); err != nil || !former || name != "" {
		t.Errorf("A's removed member in A = %q former=%v, %v; want a nameless former member", name, former, err)
	}
	if _, _, err := store.ProjectUserName(ctx, f.A.ProjectUID, projecttest.MissingUID("us")); !errors.Is(err, auth.ErrUserNotFound) {
		t.Errorf("unknown uid: err = %v, want ErrUserNotFound", err)
	}
}
