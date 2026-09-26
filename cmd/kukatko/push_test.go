package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/push"
)

// TestPushGenerateKeys prints a valid pair on stdout, as two env assignments
// and nothing else, and the secrecy reminder on stderr.
func TestPushGenerateKeys(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("kukatko")
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"push", "generate-keys"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("push generate-keys: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("stdout has %d lines, want 2:\n%s", len(lines), stdout.String())
	}
	public, okPublic := strings.CutPrefix(lines[0], "KUKATKO_PUSH_VAPID_PUBLIC_KEY=")
	private, okPrivate := strings.CutPrefix(lines[1], "KUKATKO_PUSH_VAPID_PRIVATE_KEY=")
	if !okPublic || !okPrivate {
		t.Fatalf("stdout is not the two env assignments:\n%s", stdout.String())
	}
	if err := push.ValidateKeys(public, private); err != nil {
		t.Fatalf("printed keys are not a valid pair: %v", err)
	}
	if !strings.Contains(stderr.String(), "never in a") || strings.Contains(stderr.String(), private) {
		t.Fatalf("stderr lacks the reminder or repeats the key:\n%s", stderr.String())
	}
}

// TestPushGenerateKeys_rejectsArgs guards against a typo being silently ignored.
func TestPushGenerateKeys_rejectsArgs(t *testing.T) {
	t.Parallel()

	if _, err := executeCmd(t, "push", "generate-keys", "extra"); err == nil {
		t.Fatal("push generate-keys accepted an argument")
	}
}
