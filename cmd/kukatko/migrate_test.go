package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/database"
)

// fakeSchema is a migrationTarget that reports a fixed schema status and a
// fixed list of other clients, recording whether the clients were asked for.
type fakeSchema struct {
	status     database.SchemaStatus
	statusErr  error
	clients    []database.Client
	clientsErr error
	asked      bool
}

// SchemaStatus returns the configured status or error.
func (f *fakeSchema) SchemaStatus(context.Context) (database.SchemaStatus, error) {
	return f.status, f.statusErr
}

// OtherClients records the call and returns the configured clients or error.
func (f *fakeSchema) OtherClients(context.Context) ([]database.Client, error) {
	f.asked = true
	return f.clients, f.clientsErr
}

// behind is a schema status two migrations short of the binary.
var behind = database.SchemaStatus{Current: 112, Expected: 114, Pending: []string{"0113_a.sql", "0114_b.sql"}}

// upToDate is a schema status with nothing pending.
var upToDate = database.SchemaStatus{Current: 114, Expected: 114}

// oldServer is how a running Kukátko 0.31.0 shows up: no application_name, the
// same database user.
var oldServer = database.Client{PID: 4242, User: "kukatko", Address: "172.18.0.5"}

// newCapturingCmd returns a command whose stdout and stderr land in separate
// buffers.
func newCapturingCmd() (cmd *cobra.Command, stdout, stderr *bytes.Buffer) {
	cmd = &cobra.Command{}
	stdout, stderr = &bytes.Buffer{}, &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	return cmd, stdout, stderr
}

func TestRequireCurrentSchema(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	tests := []struct {
		name     string
		db       *fakeSchema
		wantErr  error
		wantText []string
	}{
		{"up to date passes", &fakeSchema{status: upToDate}, nil, nil},
		{
			"ahead of the binary passes",
			&fakeSchema{status: database.SchemaStatus{Current: 120, Expected: 114}}, nil, nil,
		},
		{
			"behind refuses", &fakeSchema{status: behind}, errSchemaBehind,
			[]string{
				"database is at schema 112, this binary expects 114",
				"2 pending migration(s): 0113_a.sql … 0114_b.sql",
				"run `kukatko migrate` after stopping the old server",
			},
		},
		{"status fails", &fakeSchema{statusErr: boom}, boom, []string{"checking the database schema"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := requireCurrentSchema(t.Context(), tc.db)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			for _, want := range tc.wantText {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("err %q lacks %q", err, want)
				}
			}
			if tc.db.asked {
				t.Error("the schema gate asked for other clients; it only reads the schema")
			}
		})
	}
}

func TestCheckOtherClients(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	tests := []struct {
		name       string
		db         *fakeSchema
		force      bool
		wantErr    error
		wantAsked  bool
		wantStderr []string
	}{
		{"up to date is not checked", &fakeSchema{status: upToDate, clients: []database.Client{oldServer}},
			false, nil, false, nil},
		{"behind and alone migrates", &fakeSchema{status: behind}, false, nil, true, nil},
		{
			"behind with an old server refuses", &fakeSchema{status: behind, clients: []database.Client{oldServer}},
			false, errOtherClients, true,
			[]string{"schema 112", "would bring it to 114", "1 other client(s)", "pid 4242", "no application_name"},
		},
		{
			"--force migrates anyway", &fakeSchema{status: behind, clients: []database.Client{oldServer}},
			true, nil, true, []string{"pid 4242", "--force: migrating anyway"},
		},
		{"status fails", &fakeSchema{statusErr: boom}, false, boom, false, nil},
		{"listing fails", &fakeSchema{status: behind, clientsErr: boom}, true, boom, true, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd, stdout, stderr := newCapturingCmd()
			err := checkOtherClients(t.Context(), cmd, tc.db, tc.force)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.db.asked != tc.wantAsked {
				t.Errorf("asked for clients = %v, want %v", tc.db.asked, tc.wantAsked)
			}
			for _, want := range tc.wantStderr {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr %q lacks %q", stderr.String(), want)
				}
			}
			if len(tc.wantStderr) == 0 && stderr.Len() != 0 {
				t.Errorf("stderr = %q, want nothing", stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want nothing", stdout.String())
			}
		})
	}
}

func TestWarnOtherClients(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		db       *fakeSchema
		wantLogs []string
	}{
		{"up to date logs nothing", &fakeSchema{status: upToDate, clients: []database.Client{oldServer}}, nil},
		{"behind and alone logs nothing", &fakeSchema{status: behind}, nil},
		{
			"behind with an old server logs at error level",
			&fakeSchema{status: behind, clients: []database.Client{oldServer}},
			[]string{"level=ERROR", "other clients are connected", "schema=112", "expected=114", "pid 4242"},
		},
		{
			"a failing check is a warning, not a stop",
			&fakeSchema{status: behind, clientsErr: errors.New("permission denied")},
			[]string{"level=WARN", "permission denied"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			warnOtherClients(t.Context(), slog.New(slog.NewTextHandler(&buf, nil)), tc.db)
			for _, want := range tc.wantLogs {
				if !strings.Contains(buf.String(), want) {
					t.Errorf("log %q lacks %q", buf.String(), want)
				}
			}
			if len(tc.wantLogs) == 0 && buf.Len() != 0 {
				t.Errorf("log = %q, want nothing", buf.String())
			}
		})
	}
}

// TestMigrateCmd_helpAndForce verifies the migrate command documents that it
// is the only schema-changing command besides serve, and carries --force.
func TestMigrateCmd_helpAndForce(t *testing.T) {
	t.Parallel()
	cmd := newMigrateCmd()
	for _, want := range []string{"Only `migrate` and `serve` change the schema", "application_name", "--force"} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("migrate help lacks %q", want)
		}
	}
	flag := cmd.Flags().Lookup("force")
	if flag == nil || flag.DefValue != "false" {
		t.Fatalf("--force = %+v, want a boolean defaulting to false", flag)
	}
}

// TestRestoreDBCmd_helpSaysItNeverMigrates verifies the restore help names the
// way through a dump older than the binary.
func TestRestoreDBCmd_helpSaysItNeverMigrates(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"never migrates", "kukatko migrate", "kukatko restore repair"} {
		if !strings.Contains(newRestoreDBCmd().Long, want) {
			t.Errorf("restore db help lacks %q", want)
		}
	}
}

// fakeStartDB is a startMigrator: a fakeSchema that also records Migrate.
type fakeStartDB struct {
	fakeSchema
	migrateErr error
	migrated   bool
}

// Migrate records the call and returns the configured error.
func (f *fakeStartDB) Migrate(context.Context) ([]string, error) {
	f.migrated = true
	return f.status.Pending, f.migrateErr
}

// TestMigrateOnStart verifies serve migrates even with an old server still
// connected (it only warns), and wraps a failing migration.
func TestMigrateOnStart(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	db := &fakeStartDB{fakeSchema: fakeSchema{status: behind, clients: []database.Client{oldServer}}}
	if err := migrateOnStart(t.Context(), logger, db); err != nil {
		t.Fatalf("migrateOnStart: %v", err)
	}
	if !db.migrated || !strings.Contains(buf.String(), "pid 4242") {
		t.Errorf("migrated = %v, log %q; want a migration after the warning", db.migrated, buf.String())
	}

	boom := errors.New("boom")
	failing := &fakeStartDB{fakeSchema: fakeSchema{status: behind}, migrateErr: boom}
	if err := migrateOnStart(t.Context(), logger, failing); !errors.Is(err, boom) ||
		!strings.Contains(err.Error(), "applying migrations") {
		t.Errorf("err = %v, want boom wrapped as applying migrations", err)
	}
}
