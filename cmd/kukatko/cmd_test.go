package main

import (
	"bytes"
	"strings"
	"testing"
)

// executeCmd runs the root command with the given args, capturing combined
// stdout/stderr output for assertions.
func executeCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()

	cmd := newRootCmd()
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

	root := newRootCmd()
	want := map[string]bool{"serve": false, "version": false}
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
