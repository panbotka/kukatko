package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// fakeMigrator is a schemaMigrator that reports a fixed pending list and
// records whether Migrate ran.
type fakeMigrator struct {
	pending    []string
	pendingErr error
	migrateErr error
	migrated   bool
}

// Pending returns the configured pending list or error.
func (f *fakeMigrator) Pending(context.Context) ([]string, error) {
	return f.pending, f.pendingErr
}

// Migrate records the call and returns the pending list as applied.
func (f *fakeMigrator) Migrate(context.Context) ([]string, error) {
	f.migrated = true
	return f.pending, f.migrateErr
}

// newCapturingCmd returns a command whose stdout and stderr land in separate
// buffers.
func newCapturingCmd() (cmd *cobra.Command, stdout, stderr *bytes.Buffer) {
	cmd = &cobra.Command{}
	stdout, stderr = &bytes.Buffer{}, &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	return cmd, stdout, stderr
}

func TestMigrateAsSideEffect_behindAnnouncesOnStderr(t *testing.T) {
	t.Parallel()
	cmd, stdout, stderr := newCapturingCmd()
	db := &fakeMigrator{pending: []string{"0100_projects.sql", "0101_x.sql", "0114_y.sql"}}

	if err := migrateAsSideEffect(t.Context(), cmd, db); err != nil {
		t.Fatalf("migrateAsSideEffect: %v", err)
	}
	if !db.migrated {
		t.Error("Migrate did not run")
	}
	got := stderr.String()
	for _, want := range []string{"3 pending migration", "0100_projects.sql", "0114_y.sql"} {
		if !strings.Contains(got, want) {
			t.Errorf("stderr %q lacks %q", got, want)
		}
	}
	if strings.Count(got, "\n") != 1 {
		t.Errorf("stderr %q, want exactly one line", got)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout %q, want nothing (the notice belongs on stderr)", stdout.String())
	}
}

func TestMigrateAsSideEffect_upToDateIsSilent(t *testing.T) {
	t.Parallel()
	cmd, stdout, stderr := newCapturingCmd()
	db := &fakeMigrator{}

	if err := migrateAsSideEffect(t.Context(), cmd, db); err != nil {
		t.Fatalf("migrateAsSideEffect: %v", err)
	}
	if !db.migrated {
		t.Error("Migrate did not run")
	}
	if stdout.Len()+stderr.Len() != 0 {
		t.Errorf("printed %q / %q on an up-to-date database, want nothing", stdout.String(), stderr.String())
	}
}

func TestMigrateAsSideEffect_errors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	tests := []struct {
		name        string
		db          *fakeMigrator
		wantMigrate bool
		wantText    string
	}{
		{"pending fails", &fakeMigrator{pendingErr: boom}, false, "checking pending migrations"},
		{"migrate fails", &fakeMigrator{migrateErr: boom}, true, "applying migrations"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd, _, _ := newCapturingCmd()
			err := migrateAsSideEffect(t.Context(), cmd, tc.db)
			if !errors.Is(err, boom) || !strings.Contains(err.Error(), tc.wantText) {
				t.Errorf("err = %v, want %q wrapping boom", err, tc.wantText)
			}
			if tc.db.migrated != tc.wantMigrate {
				t.Errorf("migrated = %v, want %v", tc.db.migrated, tc.wantMigrate)
			}
		})
	}
}
