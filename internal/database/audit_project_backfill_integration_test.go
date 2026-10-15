//go:build integration

package database_test

import (
	"os"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/review"
)

// bootstrapProject is the project migration 0120 files the pre-tenancy library
// actions under (project.BootstrapUID; internal/project is not imported here to
// keep the migration tests free of the store packages' wiring).
const bootstrapProject = "pj0000000000000000veselice"

// TestMigrate_auditProjectBackfill verifies migration 0120 through the readers
// that went empty without it. A database at 0119 holds the audit trail of a
// pre-tenancy library — entries with no project, older than the bootstrap
// project's row: a member's review answers and an album edit, a former
// member's label edit, sign-ins and account administration, a label the trail
// does not know, plus a project-scoped entry with no project written after the
// upgrade. After 0120 the bootstrap project's trail, its leaderboard and
// streak and its person filter see the library entries; the instance actions,
// the unknown label and the newer row stay instance-only; a second run changes
// nothing; and the down script takes the backfill back.
func TestMigrate_auditProjectBackfill(t *testing.T) {
	db := freshSchemaDB(t)
	ctx := t.Context()
	if _, err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrating a fresh schema: %v", err)
	}
	runDownScript(t, db, "0120")
	seedPreTenancyTrail(t, db)

	store := audit.NewStore(db.Pool())
	if n, err := store.Count(ctx, audit.Filter{ProjectUID: bootstrapProject}); err != nil || n != 0 {
		t.Fatalf("bootstrap trail before 0120 = %d entries (%v), want 0", n, err)
	}
	applied, err := db.Migrate(ctx)
	if err != nil {
		t.Fatalf("migrating 0119 to the newest file: %v", err)
	}
	if want := []string{"0120_audit_log_project_backfill.sql"}; !slices.Equal(applied, want) {
		t.Fatalf("applied %v, want %v", applied, want)
	}
	assertBackfilled(t, db)

	body, err := os.ReadFile(auditProjectBackfillMigration)
	if err != nil {
		t.Fatalf("reading %s: %v", auditProjectBackfillMigration, err)
	}
	if _, err := db.Pool().Exec(ctx, string(body)); err != nil {
		t.Fatalf("running %s again: %v", auditProjectBackfillMigration, err)
	}
	assertBackfilled(t, db)

	runDownScript(t, db, "0120")
	if n, err := store.Count(ctx, audit.Filter{ProjectUID: bootstrapProject}); err != nil || n != 0 {
		t.Errorf("bootstrap trail after 0120_down = %d entries (%v), want 0", n, err)
	}
	if n, err := store.Count(ctx, audit.Filter{InstanceOnly: true}); err != nil || n != 9 {
		t.Errorf("instance-only trail after 0120_down = %d entries (%v), want all 9", n, err)
	}
}

// seedPreTenancyTrail writes, on a 0119 schema, a member alice (approved in the
// bootstrap project), a former member carol (no membership) and a maintainer,
// and nine audit entries with no project: eight one minute older than the
// bootstrap project's row, one a minute newer.
func seedPreTenancyTrail(t *testing.T, db *database.DB) {
	t.Helper()
	if _, err := db.Pool().Exec(t.Context(), `
		INSERT INTO users (uid, username, email, password_hash, role) VALUES
			('us_bf_alice', 'alice', 'alice@example.test', 'x', 'user'),
			('us_bf_carol', 'carol', 'carol@example.test', 'x', 'user'),
			('us_bf_maint', 'maint', 'maint@example.test', 'x', 'maintainer');
		INSERT INTO project_members (project_uid, user_uid, role, approved_at)
		VALUES ('`+bootstrapProject+`', 'us_bf_alice', 'curator', now());
		WITH v AS (SELECT created_at FROM projects WHERE uid = '`+bootstrapProject+`')
		INSERT INTO audit_log (actor_uid, action, target_type, target_uid, details, created_at)
		SELECT e.actor, e.action, e.target_type, e.target_uid, e.details::jsonb, v.created_at + e.shift
		FROM v, (VALUES
			('us_bf_alice', 'face.assign', 'markers', 'mk_1', '{"via": "review"}', interval '-1 minute'),
			('us_bf_alice', 'label.reject', 'photos', 'ph_1', '{"via": "review"}', interval '-1 minute'),
			('us_bf_alice', 'album.create', 'albums', 'al_1', '{}', interval '-1 minute'),
			('us_bf_carol', 'label.update', 'labels', 'lb_1', '{}', interval '-1 minute'),
			('us_bf_alice', 'passkey.login', 'users', 'us_bf_alice', '{}', interval '-1 minute'),
			('us_bf_maint', 'user.create', 'users', 'us_bf_carol', '{}', interval '-1 minute'),
			('us_bf_maint', 'user.approve', 'users', 'us_bf_carol', '{}', interval '-1 minute'),
			('us_bf_maint', 'legacy.unknown', 'users', 'us_bf_carol', '{}', interval '-1 minute'),
			('us_bf_alice', 'album.update', 'albums', 'al_1', '{}', interval '1 minute')
		) AS e (actor, action, target_type, target_uid, details, shift)`); err != nil {
		t.Fatalf("seeding the pre-tenancy trail: %v", err)
	}
}

// assertBackfilled fails the test unless the readers of a project's trail see
// exactly the four pre-tenancy library entries and the instance-only listing
// keeps the rest.
func assertBackfilled(t *testing.T, db *database.DB) {
	t.Helper()
	ctx := t.Context()
	store := audit.NewStore(db.Pool())

	project, err := store.List(ctx, audit.Filter{ProjectUID: bootstrapProject})
	if err != nil {
		t.Fatalf("listing the bootstrap trail: %v", err)
	}
	want := []string{"album.create", "face.assign", "label.reject", "label.update"}
	if got := recordActions(project); !slices.Equal(got, want) {
		t.Errorf("bootstrap trail = %v, want %v", got, want)
	}
	if n, err := store.Count(ctx, audit.Filter{ProjectUID: bootstrapProject}); err != nil || n != len(want) {
		t.Errorf("bootstrap trail count = %d (%v), want %d", n, err, len(want))
	}
	instance, err := store.List(ctx, audit.Filter{InstanceOnly: true})
	if err != nil {
		t.Fatalf("listing the instance trail: %v", err)
	}
	wantInstance := []string{"album.update", "legacy.unknown", "passkey.login", "user.approve", "user.create"}
	if got := recordActions(instance); !slices.Equal(got, wantInstance) {
		t.Errorf("instance-only trail = %v, want %v", got, wantInstance)
	}
	if uid, err := store.ProjectPerson(ctx, bootstrapProject, "carol"); err != nil || uid != "us_bf_carol" {
		t.Errorf("the former member carol in the bootstrap project = %q (%v), want us_bf_carol", uid, err)
	}

	board, err := review.NewLeaderboardStore(db.Pool()).Leaderboard(ctx, bootstrapProject, review.WindowAllTime)
	if err != nil {
		t.Fatalf("reading the bootstrap leaderboard: %v", err)
	}
	if len(board) != 1 {
		t.Fatalf("bootstrap leaderboard = %+v, want alice alone", board)
	}
	if e := board[0]; e.UserUID != "us_bf_alice" || e.YesCount != 1 || e.NoCount != 1 || e.Total != 2 {
		t.Errorf("alice on the bootstrap leaderboard = %+v, want 1 yes, 1 no, 2 total", e)
	}
	if board[0].StreakDays != 1 {
		t.Errorf("alice's bootstrap streak = %d days, want 1", board[0].StreakDays)
	}
}

// recordActions returns the actions of records in byte order.
func recordActions(records []audit.Record) []string {
	actions := make([]string, 0, len(records))
	for _, r := range records {
		actions = append(actions, r.Action)
	}
	slices.Sort(actions)
	return actions
}
