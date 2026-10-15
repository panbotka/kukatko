//go:build integration

package notifystanding_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/notifystanding"
	"github.com/panbotka/kukatko/internal/project"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// offProjectUID is a second project, deactivated, that some accounts belong to.
const offProjectUID = "pjoff"

// TestInStanding_rule walks the standing rule over real rows: an approved
// member of an active project is in standing; a pending member, a member whose
// identity is withheld (an exact-match add it has not entered), a disabled
// account, an account of another project only, a member of a deactivated
// project and a maintainer who is no member are not — except that the
// registration-waiting kind reaches an enabled maintainer of an active project
// whether a member or not.
func TestInStanding_rule(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	ctx := t.Context()
	type statement struct {
		sql  string
		args []any
	}
	seed := []statement{
		{`INSERT INTO projects (uid, slug, name, active) VALUES ($1, 'off', 'Off', false)`, []any{offProjectUID}},
	}
	accounts := []struct {
		uid, role, project string
		approved, disabled bool
		withheld           bool
	}{
		{uid: "us-member", role: "user", project: project.BootstrapUID, approved: true},
		{uid: "us-withheld", role: "user", project: project.BootstrapUID, approved: true, withheld: true},
		{uid: "us-maint-withheld", role: "maintainer", project: project.BootstrapUID, approved: true, withheld: true},
		{uid: "us-pending", role: "user", project: project.BootstrapUID},
		{uid: "us-disabled", role: "user", project: project.BootstrapUID, approved: true, disabled: true},
		{uid: "us-elsewhere", role: "user", project: offProjectUID, approved: true},
		{uid: "us-maint", role: "maintainer"},
		{uid: "us-maint-in", role: "maintainer", project: project.BootstrapUID, approved: true},
		{uid: "us-maint-off", role: "maintainer", disabled: true},
	}
	for _, a := range accounts {
		seed = append(seed, statement{`INSERT INTO users (uid, username, email, password_hash, role, disabled) VALUES ($1, $2, $3, 'x', $4, $5)`,
			[]any{a.uid, a.uid, a.uid + "@example.test", a.role, a.disabled}})
		if a.project != "" {
			seed = append(seed, statement{`INSERT INTO project_members
				(project_uid, user_uid, role, approved_at, identity_withheld)
				VALUES ($1, $2, 'viewer', CASE WHEN $3::boolean THEN now() END, $4)`,
				[]any{a.project, a.uid, a.approved, a.withheld}})
		}
	}
	for _, s := range seed {
		if _, err := pool.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("seeding %q: %v", s.sql, err)
		}
	}

	tagged, pending := string(notification.KindTagged), string(notification.KindRegistrationPending)
	tests := []struct {
		user, project, kind string
		want                bool
	}{
		{user: "us-member", project: project.BootstrapUID, kind: tagged, want: true},
		{user: "us-pending", project: project.BootstrapUID, kind: tagged},
		{user: "us-withheld", project: project.BootstrapUID, kind: tagged},
		{user: "us-withheld", project: project.BootstrapUID, kind: pending},
		{user: "us-maint-withheld", project: project.BootstrapUID, kind: tagged},
		{user: "us-maint-withheld", project: project.BootstrapUID, kind: pending, want: true},
		{user: "us-disabled", project: project.BootstrapUID, kind: tagged},
		{user: "us-elsewhere", project: project.BootstrapUID, kind: tagged},
		{user: "us-elsewhere", project: offProjectUID, kind: tagged},
		{user: "us-maint", project: project.BootstrapUID, kind: tagged},
		{user: "us-maint-in", project: project.BootstrapUID, kind: tagged, want: true},
		{user: "us-maint", project: project.BootstrapUID, kind: pending, want: true},
		{user: "us-maint", project: offProjectUID, kind: pending},
		{user: "us-maint-off", project: project.BootstrapUID, kind: pending},
		{user: "us-pending", project: project.BootstrapUID, kind: pending},
		{user: "us-member", project: "pjmissing", kind: tagged},
		{user: "us-member", project: "", kind: tagged},
	}
	checker := notifystanding.NewChecker(pool)
	for _, tt := range tests {
		to := notifystanding.Addressee{UserUID: tt.user, ProjectUID: tt.project, Kind: tt.kind}
		got, err := checker.InStanding(ctx, to)
		if err != nil {
			t.Fatalf("InStanding(%+v): %v", to, err)
		}
		if got != tt.want {
			t.Errorf("InStanding(%+v) = %v, want %v", to, got, tt.want)
		}
	}

	// The SQL fragment producers embed agrees with the Go-side question.
	var members []string
	rows, err := pool.Query(ctx, `SELECT u.uid FROM users u WHERE `+
		notifystanding.MemberSQL("u.uid", "$1")+` ORDER BY u.uid`, project.BootstrapUID)
	if err != nil {
		t.Fatalf("querying with MemberSQL: %v", err)
	}
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		members = append(members, uid)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading: %v", err)
	}
	if len(members) != 2 || members[0] != "us-maint-in" || members[1] != "us-member" {
		t.Errorf("MemberSQL selects %v, want [us-maint-in us-member]", members)
	}
}
