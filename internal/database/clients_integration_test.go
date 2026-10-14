//go:build integration

package database_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
)

// TestNew_announcesTheApplicationName verifies every pooled connection carries
// the process's application_name — also over a DSN that asks for another one,
// because a renamed Kukátko would be invisible to the next release's check.
func TestNew_announcesTheApplicationName(t *testing.T) {
	_ = dbtest.New(t)
	db, err := database.New(t.Context(), config.DatabaseConfig{
		URL:          dsnWithParam(os.Getenv(dbtest.EnvTestDatabaseURL), "application_name", "not-kukatko"),
		MaxOpenConns: 2, MaxIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(db.Close)

	var got string
	if err := db.Pool().QueryRow(t.Context(), "SHOW application_name").Scan(&got); err != nil {
		t.Fatalf("reading application_name: %v", err)
	}
	if got != database.ApplicationName() || got != db.ApplicationName() {
		t.Errorf("application_name = %q, want %q", got, database.ApplicationName())
	}
	if !strings.HasPrefix(got, database.ApplicationNamePrefix) {
		t.Errorf("application_name = %q lacks the %q prefix", got, database.ApplicationNamePrefix)
	}
}

// TestOtherClients_seesOthersNotItself opens, next to the DB under test, a pool
// announcing another Kukátko's name and a bare connection announcing nothing
// (what Kukátko 0.31.0 looks like), and verifies OtherClients lists both and
// none of the DB's own sessions. Other sessions on the shared test database may
// show up too, so the assertions are by pid, not on the whole list.
func TestOtherClients_seesOthersNotItself(t *testing.T) {
	db := dbtest.New(t)
	ctx := t.Context()
	url := os.Getenv(dbtest.EnvTestDatabaseURL)

	other, err := database.NewWithApplicationName(ctx, config.DatabaseConfig{URL: url, MaxOpenConns: 1},
		database.ApplicationNamePrefix+"9.9.9 deadbeef")
	if err != nil {
		t.Fatalf("connecting the other kukatko: %v", err)
	}
	t.Cleanup(other.Close)
	otherPID := backendPID(t, other.Pool().QueryRow(ctx, "SELECT pg_backend_pid()"))

	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		t.Fatalf("parsing the test DSN: %v", err)
	}
	delete(cfg.RuntimeParams, "application_name")
	bare, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connecting the unnamed client: %v", err)
	}
	t.Cleanup(func() { _ = bare.Close(t.Context()) })
	barePID := backendPID(t, bare.QueryRow(ctx, "SELECT pg_backend_pid()"))
	ownPID := backendPID(t, db.Pool().QueryRow(ctx, "SELECT pg_backend_pid()"))

	clients, err := db.OtherClients(ctx)
	if err != nil {
		t.Fatalf("OtherClients: %v", err)
	}
	byPID := map[int32]database.Client{}
	for _, c := range clients {
		byPID[c.PID] = c
		if c.ApplicationName == db.ApplicationName() {
			t.Errorf("OtherClients listed one of the DB's own sessions: %s", c)
		}
	}
	if _, ok := byPID[ownPID]; ok {
		t.Errorf("OtherClients listed the DB's own session pid %d", ownPID)
	}
	if c, ok := byPID[otherPID]; !ok || !c.Kukatko() || c.Since.IsZero() {
		t.Errorf("the other kukatko (pid %d) = %+v, %v; want listed as Kukátko with its start", otherPID, c, ok)
	}
	if c, ok := byPID[barePID]; !ok || c.Kukatko() || c.ApplicationName != "" {
		t.Errorf("the unnamed client (pid %d) = %+v, %v; want listed, unnamed", barePID, c, ok)
	}
	if !slices.ContainsFunc(clients, func(c database.Client) bool { return c.PID == otherPID }) {
		t.Error("the other kukatko is missing from the list")
	}

	// The other pool sees the DB under test the same way round.
	back, err := other.OtherClients(ctx)
	if err != nil {
		t.Fatalf("OtherClients from the other pool: %v", err)
	}
	if !slices.ContainsFunc(back, func(c database.Client) bool { return c.PID == ownPID }) {
		t.Errorf("the other pool does not see pid %d of the DB under test in %v", ownPID, back)
	}
}

// TestSchemaStatus_freshThenMigrated verifies the schema status of an empty
// schema (nothing applied, everything pending, nothing created) and of the same
// schema once migrated (current = expected, nothing pending).
func TestSchemaStatus_freshThenMigrated(t *testing.T) {
	db := freshSchemaDB(t)
	ctx := t.Context()

	before, err := db.SchemaStatus(ctx)
	if err != nil {
		t.Fatalf("SchemaStatus on an empty schema: %v", err)
	}
	if before.Current != 0 || !before.Behind() || before.Expected == 0 {
		t.Errorf("empty schema status = %+v, want current 0, behind, expected > 0", before)
	}
	if _, err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	after, err := db.SchemaStatus(ctx)
	if err != nil {
		t.Fatalf("SchemaStatus after migrating: %v", err)
	}
	if after.Behind() || after.Current != after.Expected || after.Expected != before.Expected {
		t.Errorf("migrated schema status = %+v, want current = expected = %d", after, before.Expected)
	}
}

// backendPID scans the single pg_backend_pid() a query row returns.
func backendPID(t *testing.T, row pgx.Row) int32 {
	t.Helper()
	var pid int32
	if err := row.Scan(&pid); err != nil {
		t.Fatalf("reading pg_backend_pid: %v", err)
	}
	return pid
}
