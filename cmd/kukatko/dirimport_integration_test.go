//go:build integration

package main

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// TestResolveUploader_onlyMembersOfTheTarget asserts the import's uploader —
// whose name the target project is shown on every imported photo — is always an
// approved member of that project: --uploader naming another project's member
// is refused, and the default (the bootstrap admin, then any maintainer) skips
// an account that is no member of the target, leaving the import ownerless
// rather than signed by a stranger.
func TestResolveUploader_onlyMembersOfTheTarget(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := projecttest.Seed(t, db.Pool(), projecttest.AsMaintainer(projecttest.SideA))
	users := auth.NewStore(db.Pool())
	cfg := &config.Config{}
	cfg.Auth.BootstrapAdminUsername = f.A.Username
	ctx := t.Context()

	tests := []struct {
		name, project, username, want string
		wantErr                       error
	}{
		{name: "B's member into B", project: f.B.ProjectUID, username: f.B.Username, want: f.B.UserUID},
		{name: "A's member into B", project: f.B.ProjectUID, username: f.A.Username, wantErr: errUploaderNotMember},
		{name: "nobody", project: f.B.ProjectUID, username: "nikdo-" + f.B.Token, wantErr: errNoUploader},
		{name: "default into A", project: f.A.ProjectUID, want: f.A.UserUID},
		{name: "default into B skips the non-member admin", project: f.B.ProjectUID, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveUploader(ctx, cfg, users, db.Pool(), tt.project, tt.username)
			if !errors.Is(err, tt.wantErr) || got != tt.want {
				t.Errorf("resolveUploader(%s, %q) = %q, %v; want %q, %v",
					tt.project, tt.username, got, err, tt.want, tt.wantErr)
			}
		})
	}
}
