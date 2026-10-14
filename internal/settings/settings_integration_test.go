//go:build integration

package settings_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/settings"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// migrationPath is the settings migration, read back by TestMigrationSeedsTheRow
// so the seed is proved from the file that ships it.
const migrationPath = "../database/migrations/0062_instance_settings.sql"

// seedProbeSchema is the throwaway schema TestMigrationSeedsTheRow builds the
// table in. The shared test database is truncated between cases — including the
// seeded row — so the seed cannot be observed in the public schema; applying the
// migration to an empty schema of its own shows what a fresh instance gets,
// whatever order the rest of the suite ran in.
const seedProbeSchema = "settings_seed_probe"

// newStore returns a settings.Store plus the auth store used to seed actors and
// the database handle, over a freshly truncated integration database.
func newStore(t *testing.T) (*settings.Store, *auth.Store, *database.DB) {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	return settings.NewStore(db.Pool()), auth.NewStore(db.Pool()), db
}

// makeAdmin inserts an admin account with the given uid/username and returns it.
func makeAdmin(t *testing.T, store *auth.Store, uid, username string) string {
	t.Helper()
	if err := store.CreateUser(context.Background(), auth.User{
		UID:          uid,
		Username:     username,
		Email:        username + "@example.test",
		PasswordHash: "x",
		Role:         auth.RoleUser,
	}.InProject(project.BootstrapUID, project.RoleAdmin)); err != nil {
		t.Fatalf("creating user %s: %v", username, err)
	}
	return uid
}

// updateEntry builds a settings.update audit entry stamped with actorUID.
func updateEntry(actorUID string) audit.Entry {
	return audit.Entry{ActorUID: actorUID, Action: audit.ActionSettingsUpdate, TargetType: "settings"}
}

// countAudit returns how many audit_log rows exist for the given action.
func countAudit(t *testing.T, db *database.DB, action string) int {
	t.Helper()
	n, err := audit.NewStore(db.Pool()).Count(context.Background(), audit.Filter{Action: action})
	if err != nil {
		t.Fatalf("counting audit %q: %v", action, err)
	}
	return n
}

// TestMigrationSeedsTheRow applies the settings migration to a throwaway schema
// and checks it leaves exactly one row, closed and empty: what every read on a
// fresh instance finds.
func TestMigrationSeedsTheRow(t *testing.T) {
	conn := probeConn(t)
	applyMigration(t, conn)
	ctx := t.Context()

	var rows int
	if err := conn.QueryRow(ctx,
		"SELECT count(*) FROM "+seedProbeSchema+".instance_settings",
	).Scan(&rows); err != nil {
		t.Fatalf("counting seeded rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("seeded rows = %d, want exactly 1", rows)
	}

	var enabled bool
	var secret, welcome string
	if err := conn.QueryRow(ctx,
		"SELECT registration_enabled, registration_secret, welcome_markdown"+
			" FROM "+seedProbeSchema+".instance_settings WHERE id = true",
	).Scan(&enabled, &secret, &welcome); err != nil {
		t.Fatalf("reading the seeded row: %v", err)
	}
	if enabled || secret != "" || welcome != "" {
		t.Fatalf("seeded row = {enabled:%v secret:%q welcome:%q}, want a closed, empty instance",
			enabled, secret, welcome)
	}
}

// probeConn opens a dedicated connection to the test database and gives it an
// empty schema of its own to build in. A dedicated connection, not the shared
// pool: the probe changes search_path for its session, and a pooled connection
// would carry that back to whoever borrows it next. The schema is dropped, and
// the connection closed, when the test ends.
func probeConn(t *testing.T) *pgx.Conn {
	t.Helper()
	url := os.Getenv(dbtest.EnvTestDatabaseURL)
	if url == "" {
		t.Skipf("%s not set; skipping integration test", dbtest.EnvTestDatabaseURL)
	}
	ctx := t.Context()

	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connecting to test database: %v", err)
	}
	// Registered before the schema drop below, so it runs after it: the drop
	// needs the connection still open.
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	if _, err := conn.Exec(ctx, "DROP SCHEMA IF EXISTS "+seedProbeSchema+" CASCADE"); err != nil {
		t.Fatalf("dropping stale probe schema: %v", err)
	}
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+seedProbeSchema); err != nil {
		t.Fatalf("creating probe schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := conn.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+seedProbeSchema+" CASCADE"); err != nil {
			t.Errorf("dropping probe schema: %v", err)
		}
	})
	// public stays on the path so the migration's users foreign key resolves;
	// the probe schema is first, so the new table lands there.
	if _, err := conn.Exec(ctx, "SET search_path TO "+seedProbeSchema+", public"); err != nil {
		t.Fatalf("setting probe search_path: %v", err)
	}
	return conn
}

// applyMigration runs the settings migration file verbatim over conn, so the
// seed is proved from the SQL that ships rather than from a copy of it.
func applyMigration(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	sql, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("reading %s: %v", migrationPath, err)
	}
	if _, err := conn.Exec(t.Context(), string(sql)); err != nil {
		t.Fatalf("applying %s: %v", migrationPath, err)
	}
}

// TestRoundTrip writes all three values, reads them back, and checks the write
// is an upsert (a second Set replaces the single row) that audits each change.
func TestRoundTrip(t *testing.T) {
	store, users, db := newStore(t)
	ctx := context.Background()
	admin := makeAdmin(t, users, "se_admin", "admin")

	saved, err := store.Set(ctx, project.BootstrapUID, settings.Update{
		RegistrationEnabled: true,
		RegistrationSecret:  "rodina2026",
		WelcomeMarkdown:     "# Vítej v Kukátku",
	}, admin, updateEntry(admin))
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !saved.RegistrationEnabled || saved.RegistrationSecret != "rodina2026" ||
		saved.WelcomeMarkdown != "# Vítej v Kukátku" || saved.UpdatedByUID != admin || saved.UpdatedAt.IsZero() {
		t.Fatalf("unexpected saved settings: %+v", saved)
	}

	got, err := store.Get(ctx, project.BootstrapUID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != saved {
		t.Fatalf("Get = %+v, want %+v", got, saved)
	}

	replaced, err := store.Set(ctx, project.BootstrapUID, settings.Update{
		RegistrationEnabled: false,
		RegistrationSecret:  "",
		WelcomeMarkdown:     "",
	}, admin, updateEntry(admin))
	if err != nil {
		t.Fatalf("Set replace: %v", err)
	}
	if replaced.RegistrationEnabled || replaced.RegistrationSecret != "" || replaced.WelcomeMarkdown != "" {
		t.Fatalf("unexpected replaced settings: %+v", replaced)
	}
	if n := countRows(t, db); n != 1 {
		t.Fatalf("instance_settings row count = %d, want 1 (upsert)", n)
	}
	if n := countAudit(t, db, audit.ActionSettingsUpdate); n != 2 {
		t.Fatalf("settings.update audit rows = %d, want 2", n)
	}
}

// TestGetWithoutRowReturnsDefaults: after a truncation removes the seeded row a
// read still answers, with registration closed — the anonymous sign-in screen
// must never be blocked by a missing settings row.
func TestGetWithoutRowReturnsDefaults(t *testing.T) {
	store, _, db := newStore(t)
	if n := countRows(t, db); n != 0 {
		t.Fatalf("row count after truncation = %d, want 0", n)
	}
	got, err := store.Get(context.Background(), project.BootstrapUID)
	if err != nil {
		t.Fatalf("Get with no row: %v", err)
	}
	if got != (settings.Settings{}) {
		t.Fatalf("Get with no row = %+v, want the zero-value defaults", got)
	}
}

// TestSetRejectsSecretlessRegistration refuses to open registration without a
// secret, and leaves the stored settings untouched.
func TestSetRejectsSecretlessRegistration(t *testing.T) {
	store, users, db := newStore(t)
	ctx := context.Background()
	admin := makeAdmin(t, users, "se_lock", "lock")

	for _, secret := range []string{"", "   "} {
		if _, err := store.Set(ctx, project.BootstrapUID, settings.Update{
			RegistrationEnabled: true,
			RegistrationSecret:  secret,
		}, admin, updateEntry(admin)); !errors.Is(err, settings.ErrSecretRequired) {
			t.Fatalf("Set(enabled, secret=%q) error = %v, want ErrSecretRequired", secret, err)
		}
	}
	if n := countRows(t, db); n != 0 {
		t.Fatalf("rejected update wrote %d rows, want 0", n)
	}
	if n := countAudit(t, db, audit.ActionSettingsUpdate); n != 0 {
		t.Fatalf("rejected update wrote %d audit rows, want 0", n)
	}
}

// TestActorCascadesToNull checks that deleting the administrator leaves the
// settings standing with an empty actor, per ON DELETE SET NULL — losing the
// account must not close registration on everybody else.
func TestActorCascadesToNull(t *testing.T) {
	store, users, db := newStore(t)
	ctx := context.Background()
	admin := makeAdmin(t, users, "se_gone", "gone")

	if _, err := store.Set(ctx, project.BootstrapUID, settings.Update{
		RegistrationEnabled: true,
		RegistrationSecret:  "rodina2026",
	}, admin, updateEntry(admin)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, "DELETE FROM users WHERE uid = $1", admin); err != nil {
		t.Fatalf("deleting the administrator: %v", err)
	}
	got, err := store.Get(ctx, project.BootstrapUID)
	if err != nil {
		t.Fatalf("Get after actor delete: %v", err)
	}
	if !got.RegistrationEnabled || got.RegistrationSecret != "rodina2026" || got.UpdatedByUID != "" {
		t.Fatalf("after actor delete = %+v, want the settings kept and an empty actor", got)
	}
}

// countRows returns the number of rows in the instance_settings table.
func countRows(t *testing.T, db *database.DB) int {
	t.Helper()
	var n int
	if err := db.Pool().QueryRow(context.Background(), "SELECT count(*) FROM instance_settings").Scan(&n); err != nil {
		t.Fatalf("counting instance_settings: %v", err)
	}
	return n
}

// TestSettingsArePerProject: each project has its own row. A write to one
// project leaves another's settings at their defaults, and the second project's
// own write adds a row instead of replacing the first.
func TestSettingsArePerProject(t *testing.T) {
	store, users, db := newStore(t)
	ctx := context.Background()
	admin := makeAdmin(t, users, "se_projects", "projects")
	const other = "pjsettingsotherproject0001"
	if _, err := db.Pool().Exec(ctx,
		"INSERT INTO projects (uid, slug, name) VALUES ($1, 'settings-other', 'Other')", other); err != nil {
		t.Fatalf("creating the second project: %v", err)
	}

	if _, err := store.Set(ctx, project.BootstrapUID, settings.Update{
		RegistrationEnabled: true, RegistrationSecret: "veselice", WelcomeMarkdown: "Ahoj",
	}, admin, updateEntry(admin)); err != nil {
		t.Fatalf("Set bootstrap: %v", err)
	}
	untouched, err := store.Get(ctx, other)
	if err != nil {
		t.Fatalf("Get other: %v", err)
	}
	if untouched != (settings.Settings{}) {
		t.Fatalf("other project's settings = %+v, want the defaults", untouched)
	}

	if _, err := store.Set(ctx, other, settings.Update{WelcomeMarkdown: "Jiná knihovna"},
		admin, updateEntry(admin)); err != nil {
		t.Fatalf("Set other: %v", err)
	}
	if n := countRows(t, db); n != 2 {
		t.Fatalf("instance_settings row count = %d, want 2 (one per project)", n)
	}
	bootstrap, err := store.Get(ctx, project.BootstrapUID)
	if err != nil {
		t.Fatalf("Get bootstrap: %v", err)
	}
	if !bootstrap.RegistrationEnabled || bootstrap.RegistrationSecret != "veselice" || bootstrap.WelcomeMarkdown != "Ahoj" {
		t.Fatalf("bootstrap settings after the other project's write = %+v", bootstrap)
	}
}

// TestFooterRoundTrip: a sent footer line is stored trimmed, an update that
// leaves it out keeps it (nil = untouched), and an empty one clears it — while
// the registration values beside it are replaced every time.
func TestFooterRoundTrip(t *testing.T) {
	store, users, _ := newStore(t)
	ctx := context.Background()
	admin := makeAdmin(t, users, "se_footer", "footer")

	saved, err := store.Set(ctx, project.BootstrapUID, settings.Update{
		WelcomeMarkdown: "Ahoj", FooterText: new("  Provozuje spolek  "),
	}, admin, updateEntry(admin))
	if err != nil {
		t.Fatalf("Set with footer: %v", err)
	}
	if saved.FooterText != "Provozuje spolek" {
		t.Fatalf("saved footer = %q, want the trimmed line", saved.FooterText)
	}

	kept, err := store.Set(ctx, project.BootstrapUID, settings.Update{WelcomeMarkdown: "Nazdar"},
		admin, updateEntry(admin))
	if err != nil {
		t.Fatalf("Set without footer: %v", err)
	}
	if kept.FooterText != "Provozuje spolek" || kept.WelcomeMarkdown != "Nazdar" {
		t.Fatalf("after an update without the footer = %+v, want the line kept", kept)
	}

	cleared, err := store.Set(ctx, project.BootstrapUID, settings.Update{FooterText: new("")},
		admin, updateEntry(admin))
	if err != nil {
		t.Fatalf("Set clearing the footer: %v", err)
	}
	if cleared.FooterText != "" {
		t.Fatalf("cleared footer = %q, want none", cleared.FooterText)
	}

	if _, err := store.Set(ctx, project.BootstrapUID, settings.Update{FooterText: new("a\nb")},
		admin, updateEntry(admin)); !errors.Is(err, settings.ErrFooterMultiline) {
		t.Fatalf("Set with a two-line footer error = %v, want ErrFooterMultiline", err)
	}
}

// footerMigrationPath is the migration that backfills the bootstrap project's
// footer line, re-run by TestFooterMigrationBackfillsOnlyTheBootstrap.
const footerMigrationPath = "../database/migrations/0118_instance_settings_footer.sql"

// TestFooterMigrationBackfillsOnlyTheBootstrap re-runs migration 0118 over rows
// of its own: the bootstrap project's empty line becomes the sentence its pages
// always showed, another project's stays empty, and a line an admin wrote is
// never overwritten by a re-run.
func TestFooterMigrationBackfillsOnlyTheBootstrap(t *testing.T) {
	store, users, db := newStore(t)
	ctx := context.Background()
	admin := makeAdmin(t, users, "se_backfill", "backfill")
	const other = "pjsettingsfooterproject001"
	if _, err := db.Pool().Exec(ctx,
		"INSERT INTO projects (uid, slug, name) VALUES ($1, 'settings-footer', 'Other')", other); err != nil {
		t.Fatalf("creating the second project: %v", err)
	}
	for _, uid := range []string{project.BootstrapUID, other} {
		if _, err := store.Set(ctx, uid, settings.Update{FooterText: new("")}, admin, updateEntry(admin)); err != nil {
			t.Fatalf("seeding %s: %v", uid, err)
		}
	}
	sql, err := os.ReadFile(footerMigrationPath)
	if err != nil {
		t.Fatalf("reading %s: %v", footerMigrationPath, err)
	}
	runMigration := func() {
		t.Helper()
		if _, err := db.Pool().Exec(ctx, string(sql)); err != nil {
			t.Fatalf("applying %s: %v", footerMigrationPath, err)
		}
	}
	footerOf := func(uid string) string {
		t.Helper()
		got, err := store.Get(ctx, uid)
		if err != nil {
			t.Fatalf("Get %s: %v", uid, err)
		}
		return got.FooterText
	}

	runMigration()
	if got := footerOf(project.BootstrapUID); got != "Provozuje SDH Veselice" {
		t.Fatalf("bootstrap footer after the migration = %q, want the old sentence", got)
	}
	if got := footerOf(other); got != "" {
		t.Fatalf("other project's footer after the migration = %q, want none", got)
	}

	if _, err := store.Set(ctx, project.BootstrapUID, settings.Update{FooterText: new("Vlastní text")},
		admin, updateEntry(admin)); err != nil {
		t.Fatalf("writing a custom footer: %v", err)
	}
	runMigration()
	if got := footerOf(project.BootstrapUID); got != "Vlastní text" {
		t.Fatalf("bootstrap footer after a re-run = %q, want the admin's line kept", got)
	}
}
