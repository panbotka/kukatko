//go:build integration

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
)

// freshSchemaDSN creates an empty schema in the test database and returns a
// DSN whose search_path puts it first, so a command run against it sees a
// database that has never been migrated — the most extreme "behind" there is.
// The schema is dropped when the test ends.
func freshSchemaDSN(t *testing.T) string {
	t.Helper()
	admin := dbtest.New(t) // extensions exist in public, the test DSN is set
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatalf("random schema suffix: %v", err)
	}
	schema := "cli_fresh_" + hex.EncodeToString(suffix)
	ident := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Pool().Exec(t.Context(), "CREATE SCHEMA "+ident); err != nil {
		t.Fatalf("creating schema %s: %v", schema, err)
	}
	t.Cleanup(func() {
		if _, err := admin.Pool().Exec(context.Background(), "DROP SCHEMA IF EXISTS "+ident+" CASCADE"); err != nil {
			t.Errorf("dropping schema %s: %v", schema, err)
		}
	})
	dsn := os.Getenv(dbtest.EnvTestDatabaseURL)
	if !strings.Contains(dsn, "://") {
		return dsn + " search_path='" + schema + ",public'"
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "search_path=" + url.QueryEscape(schema+",public")
}

// appliedCount returns how many migrations the schema behind dsn records, 0
// when it has no schema_migrations at all.
func appliedCount(t *testing.T, dsn string) int64 {
	t.Helper()
	db, err := database.New(t.Context(), config.DatabaseConfig{URL: dsn, MaxOpenConns: 1})
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	defer db.Close()
	status, err := db.SchemaStatus(t.Context())
	if err != nil {
		t.Fatalf("SchemaStatus: %v", err)
	}
	return status.Current
}

// runKukatko runs the root command with args against dsn (through the env
// override, which beats any config file) and returns its error and stderr.
func runKukatko(t *testing.T, dsn string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("KUKATKO_DATABASE_URL", dsn)
	root := newRootCmd("kukatko")
	_, _, stderr := newCapturingCmd()
	root.SetOut(&strings.Builder{})
	root.SetErr(stderr)
	root.SetArgs(append([]string{"--config", t.TempDir() + "/none.yaml"}, args...))
	err := root.ExecuteContext(t.Context())
	return stderr.String(), err
}

// TestSubcommands_refuseABehindSchema runs subcommands that used to migrate as a
// side effect against a never-migrated schema: each must fail with
// errSchemaBehind and leave the schema exactly as unmigrated as it found it.
func TestSubcommands_refuseABehindSchema(t *testing.T) {
	dsn := freshSchemaDSN(t)
	for _, args := range [][]string{
		{"maintenance", "scan"},
		{"sidecar", "families"},
		{"restore", "repair"},
		{"maintenance", "nameless-subjects", "--project", "v"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, err := runKukatko(t, dsn, args...)
			if !errors.Is(err, errSchemaBehind) {
				t.Fatalf("err = %v, want errSchemaBehind", err)
			}
			if !strings.Contains(err.Error(), "database is at schema 0, this binary expects") ||
				!strings.Contains(err.Error(), "run `kukatko migrate`") {
				t.Errorf("err = %q, want the schema versions and the way out", err)
			}
			if n := appliedCount(t, dsn); n != 0 {
				t.Errorf("schema at %d after %v, want it left unmigrated", n, args)
			}
		})
	}
}

// TestMigrateCmd_refusesWhileAnotherClientIsConnected keeps a bare connection
// open (what a running Kukátko 0.31.0 looks like: no application_name, same
// user) and verifies `kukatko migrate` names it and refuses, and that --force
// then migrates the whole chain.
func TestMigrateCmd_refusesWhileAnotherClientIsConnected(t *testing.T) {
	dsn := freshSchemaDSN(t)
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parsing the DSN: %v", err)
	}
	delete(cfg.RuntimeParams, "application_name")
	old, err := pgx.ConnectConfig(t.Context(), cfg)
	if err != nil {
		t.Fatalf("connecting the stand-in old server: %v", err)
	}
	t.Cleanup(func() { _ = old.Close(context.Background()) })

	stderr, err := runKukatko(t, dsn, "migrate")
	if !errors.Is(err, errOtherClients) {
		t.Fatalf("migrate err = %v, want errOtherClients", err)
	}
	if !strings.Contains(stderr, "no application_name") || !strings.Contains(stderr, "other client(s)") {
		t.Errorf("stderr %q does not name the unnamed client", stderr)
	}
	if n := appliedCount(t, dsn); n != 0 {
		t.Errorf("schema at %d after a refused migrate, want 0", n)
	}

	if _, err := runKukatko(t, dsn, "migrate", "--force"); err != nil {
		t.Fatalf("migrate --force: %v", err)
	}
	if n := appliedCount(t, dsn); n == 0 {
		t.Error("migrate --force applied nothing")
	}
	// Now current: the same subcommand that refused before runs its gate clean.
	db, err := openCurrentDB(t.Context(), config.DatabaseConfig{URL: dsn, MaxOpenConns: 1})
	if err != nil {
		t.Fatalf("openCurrentDB on the migrated schema: %v", err)
	}
	db.Close()
}
