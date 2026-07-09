package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// executeCmd runs the root command with the given args, capturing combined
// stdout/stderr output for assertions.
func executeCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()

	cmd := newRootCmd("kukatko")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)

	err := cmd.Execute()
	return buf.String(), err
}

// TestRootCmd_subcommandsRegistered verifies the expected subcommands are wired
// onto the root command.
func TestRootCmd_subcommandsRegistered(t *testing.T) {
	t.Parallel()

	root := newRootCmd("kukatko")
	want := map[string]bool{
		"serve": false, "version": false, "import": false, "backup": false, "restore": false,
		"ctl": false,
	}
	for _, c := range root.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("subcommand %q not registered on root", name)
		}
	}
}

// TestImportCmd_hasPhotoPrismChild verifies the import command exposes the
// photoprism subcommand.
func TestImportCmd_hasPhotoPrismChild(t *testing.T) {
	t.Parallel()

	var found bool
	for _, c := range newImportCmd().Commands() {
		if c.Name() == "photoprism" {
			found = true
		}
	}
	if !found {
		t.Error("import command has no photoprism subcommand")
	}
}

// TestRestoreCmd_hasChildren verifies the restore command exposes the
// list/db/originals/verify subcommands.
func TestRestoreCmd_hasChildren(t *testing.T) {
	t.Parallel()

	want := map[string]bool{"list": false, "db": false, "originals": false, "verify": false}
	for _, c := range newRestoreCmd().Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("restore command has no %q subcommand", name)
		}
	}
}

// TestRestoreDB_requiresConfirmation verifies the destructive database restore
// refuses to run without the explicit --yes flag, even when configured.
func TestRestoreDB_requiresConfirmation(t *testing.T) {
	// t.Setenv forbids t.Parallel; the env is restored after the test.
	t.Setenv("KUKATKO_DATABASE_URL", "postgres://user:pass@localhost:5432/db?sslmode=disable")
	t.Setenv("KUKATKO_BACKUP_S3_ENDPOINT", "http://localhost:9000")
	t.Setenv("KUKATKO_BACKUP_S3_BUCKET", "backups")

	if _, err := executeCmd(t, "restore", "db"); !errors.Is(err, errRestoreNotConfirmed) {
		t.Errorf("restore db without --yes error = %v, want errRestoreNotConfirmed", err)
	}
}

// TestRestoreList_requiresConfiguration verifies a restore aborts cleanly when no
// S3 destination is configured.
func TestRestoreList_requiresConfiguration(t *testing.T) {
	t.Setenv("KUKATKO_DATABASE_URL", "postgres://user:pass@localhost:5432/db?sslmode=disable")
	t.Setenv("KUKATKO_BACKUP_S3_ENDPOINT", "")
	t.Setenv("KUKATKO_BACKUP_S3_BUCKET", "")

	if _, err := executeCmd(t, "restore", "list"); !errors.Is(err, errRestoreNotConfigured) {
		t.Errorf("restore list unconfigured error = %v, want errRestoreNotConfigured", err)
	}
}

// TestVersionCmd_output verifies the version command prints version and commit.
func TestVersionCmd_output(t *testing.T) {
	t.Parallel()

	out, err := executeCmd(t, "version")
	if err != nil {
		t.Fatalf("version command returned error: %v", err)
	}
	for _, want := range []string{"kukatko", "commit:"} {
		if !strings.Contains(out, want) {
			t.Errorf("version output %q does not contain %q", out, want)
		}
	}
}

// TestCmd_unknownCommand verifies an unknown subcommand is rejected.
func TestCmd_unknownCommand(t *testing.T) {
	t.Parallel()

	if _, err := executeCmd(t, "does-not-exist"); err == nil {
		t.Error("expected error for unknown command, got nil")
	}
}

// TestServeCmd_rejectsArgs verifies the serve command accepts no positional
// arguments.
func TestServeCmd_rejectsArgs(t *testing.T) {
	t.Parallel()

	if _, err := executeCmd(t, "serve", "unexpected"); err == nil {
		t.Error("expected error for unexpected serve argument, got nil")
	}
}
