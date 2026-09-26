package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/config"
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

// TestBuildPushService builds the handler with push off (no key needed, since
// the no-op sender is wired) and with a valid pair, and refuses an enabled
// section whose keys are not a pair.
func TestBuildPushService(t *testing.T) {
	t.Parallel()

	keys, err := push.GenerateKeys()
	if err != nil {
		t.Fatalf("GenerateKeys: %v", err)
	}
	other, err := push.GenerateKeys()
	if err != nil {
		t.Fatalf("GenerateKeys: %v", err)
	}
	tests := []struct {
		name    string
		push    config.PushConfig
		wantErr error
	}{
		{name: "disabled", push: config.PushConfig{}},
		{
			name: "enabled",
			push: config.PushConfig{Enabled: true, VAPID: config.PushVAPIDConfig{
				PublicKey: keys.PublicKey, PrivateKey: keys.PrivateKey, Subject: "mailto:ops@example.org",
			}},
		},
		{
			name: "enabled with a mismatched pair",
			push: config.PushConfig{Enabled: true, VAPID: config.PushVAPIDConfig{
				PublicKey: keys.PublicKey, PrivateKey: other.PrivateKey, Subject: "mailto:ops@example.org",
			}},
			wantErr: push.ErrInvalidConfig,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc, err := buildPushService(&config.Config{Push: tt.push}, push.NewStore(nil))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("buildPushService error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && svc == nil {
				t.Fatal("buildPushService returned no service")
			}
		})
	}
}
