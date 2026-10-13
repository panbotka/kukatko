package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/rekey"
)

// fakeRekey answers every call with its canned values and records the calls.
type fakeRekey struct {
	preview rekey.Preview
	run     rekey.Run
	err     error
	calls   []string
	meta    audit.Meta
}

func (f *fakeRekey) Preview(context.Context, string, int) (rekey.Preview, error) {
	f.calls = append(f.calls, "preview")
	return f.preview, f.err
}

func (f *fakeRekey) Status(context.Context, string) (rekey.Run, error) {
	f.calls = append(f.calls, "status")
	return f.run, f.err
}

func (f *fakeRekey) answer(call string, meta audit.Meta) (rekey.Run, error) {
	f.calls = append(f.calls, call)
	f.meta = meta
	return f.run, f.err
}

func (f *fakeRekey) Start(_ context.Context, meta audit.Meta, _ string) (rekey.Run, error) {
	return f.answer("start", meta)
}

func (f *fakeRekey) Pause(_ context.Context, meta audit.Meta, _ string) (rekey.Run, error) {
	return f.answer("pause", meta)
}

func (f *fakeRekey) Resume(_ context.Context, meta audit.Meta, _ string) (rekey.Run, error) {
	return f.answer("resume", meta)
}

func (f *fakeRekey) Retry(_ context.Context, meta audit.Meta, _ string) (rekey.Run, error) {
	return f.answer("retry", meta)
}

// rekeyProject is the project the CLI tests move.
var rekeyProject = project.Project{UID: project.BootstrapUID, Slug: "v", Name: "Veselice"}

// runRekeyCaptured runs runRekey over fake and returns stdout, stderr and the
// error.
func runRekeyCaptured(t *testing.T, fake *fakeRekey, action rekeyAction) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(t.Context())
	err := runRekey(cmd, fake, rekeyProject, action)
	return stdout.String(), stderr.String(), err
}

// TestRootCmd_hasStorageRekey verifies the move is reachable as `kukatko
// storage rekey` with its action flags, every one off by default.
func TestRootCmd_hasStorageRekey(t *testing.T) {
	t.Parallel()
	cmd, _, err := newRootCmd("kukatko").Find([]string{"storage", "rekey"})
	if err != nil || cmd.Name() != "rekey" {
		t.Fatalf("Find(storage rekey) = %v, %v; want the rekey subcommand", cmd, err)
	}
	for _, name := range []string{flagDryRun, flagRekeyStatus, flagRekeyPause, flagRekeyResume, flagRekeyRetry} {
		flag := cmd.Flags().Lookup(name)
		if flag == nil || flag.DefValue != "false" {
			t.Errorf("flag --%s = %v, want a bool defaulting to false", name, flag)
		}
	}
}

// TestStorageRekeyCmd_flagsAreExclusive verifies two action flags fail before
// the config is even loaded — the DSN here points at nothing, so the error must
// be the guard's, not a connection error — and that the slug is required.
func TestStorageRekeyCmd_flagsAreExclusive(t *testing.T) {
	t.Setenv("KUKATKO_DATABASE_URL", unreachableDSN)
	_, err := executeResetCmd(t, "", "storage", "rekey", "v", "--status", "--pause")
	if !errors.Is(err, errRekeyFlagsExclusive) {
		t.Errorf("rekey --status --pause = %v, want errRekeyFlagsExclusive", err)
	}
	if _, err := executeResetCmd(t, "", "storage", "rekey"); err == nil {
		t.Error("rekey with no slug succeeded, want an argument error")
	}
}

// TestRunRekey_dryRun verifies a dry run prints the totals and the planned
// moves on stderr, nothing on stdout, and changes nothing.
func TestRunRekey_dryRun(t *testing.T) {
	t.Parallel()
	fake := &fakeRekey{preview: rekey.Preview{
		ProjectUID: project.BootstrapUID, TargetPrefix: "projects/pjv/", Photos: 3, Bytes: 3 << 20,
		First: []rekey.PreviewPhoto{
			{UID: "ph1", Original: rekey.Move{From: "2023/01/a.jpg", To: "projects/pjv/2023/01/a.jpg"}, Derived: 9},
		},
	}}
	stdout, stderr, err := runRekeyCaptured(t, fake, rekeyDryRun)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	for _, want := range []string{"project: v (Veselice)", "3 photo(s), 3.0 MiB", "under projects/pjv/",
		"2023/01/a.jpg → projects/pjv/2023/01/a.jpg (+9 derived", "nothing was changed"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("dry run stderr lacks %q:\n%s", want, stderr)
		}
	}
	if stdout != "" {
		t.Errorf("dry run wrote to stdout: %q", stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0] != "preview" {
		t.Errorf("calls = %v, want only the preview", fake.calls)
	}
}

// TestRunRekey_status verifies the report of a run — progress, bytes, the last
// errors — and that only a failed run ends the command in an error.
func TestRunRekey_status(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	errorsSeen := make([]rekey.RunError, 0, 7)
	for i := range 7 {
		errorsSeen = append(errorsSeen, rekey.RunError{Key: "2023/01/" + string(rune('a'+i)) + ".jpg",
			Message: "copy does not match", At: at})
	}
	tests := []struct {
		state   rekey.State
		wantErr error
	}{
		{rekey.StateRunning, nil},
		{rekey.StatePaused, nil},
		{rekey.StateDone, nil},
		{rekey.StateFailed, errRekeyFailed},
	}
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			t.Parallel()
			fake := &fakeRekey{run: rekey.Run{
				UID: "rk1", State: tt.state, TargetPrefix: "projects/pjv/", TotalPhotos: 200, DonePhotos: 50,
				TotalBytes: 4 << 30, DoneBytes: 1 << 30, ErrorCount: 7, Errors: errorsSeen,
				StartedAt: at, UpdatedAt: at,
			}}
			stdout, stderr, err := runRekeyCaptured(t, fake, rekeyStatus)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Errorf("status of a %s run = %v, want %v", tt.state, err, tt.wantErr)
			}
			for _, want := range []string{"run rk1: " + string(tt.state), "50 of 200 moved (25.0%)",
				"1.0 GiB of 4.0 GiB", "errors: 7", "2023/01/g.jpg: copy does not match"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("status stderr lacks %q:\n%s", want, stderr)
				}
			}
			if strings.Contains(stderr, "2023/01/b.jpg") {
				t.Errorf("status lists more than the last %d errors:\n%s", rekeyShownErrors, stderr)
			}
			if stdout != "" {
				t.Errorf("status wrote to stdout: %q", stdout)
			}
		})
	}
}

// TestRunRekey_actions verifies each flag drives its own call, the mutating
// ones attributed to the operator, and that a refusal is the command's error.
func TestRunRekey_actions(t *testing.T) {
	t.Parallel()
	for action, call := range map[rekeyAction]string{
		rekeyStart: "start", rekeyPause: "pause", rekeyResume: "resume", rekeyRetry: "retry",
	} {
		fake := &fakeRekey{run: rekey.Run{UID: "rk1", State: rekey.StateRunning}}
		if _, _, err := runRekeyCaptured(t, fake, action); err != nil {
			t.Errorf("%s: %v", call, err)
		}
		if len(fake.calls) != 1 || fake.calls[0] != call {
			t.Errorf("action %q made calls %v, want one %s", action, fake.calls, call)
		}
		if !strings.Contains(fake.meta.UserAgent, "kukatko storage rekey") {
			t.Errorf("%s audited as %q, want the CLI named", call, fake.meta.UserAgent)
		}
	}
	fake := &fakeRekey{err: rekey.ErrRunActive}
	if _, _, err := runRekeyCaptured(t, fake, rekeyStart); !errors.Is(err, rekey.ErrRunActive) {
		t.Errorf("start of an active move = %v, want ErrRunActive", err)
	}
}

func TestReadRekeyAction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args []string
		want rekeyAction
		err  error
	}{
		{nil, rekeyStart, nil},
		{[]string{"--dry-run"}, rekeyDryRun, nil},
		{[]string{"--status"}, rekeyStatus, nil},
		{[]string{"--retry"}, rekeyRetry, nil},
		{[]string{"--dry-run", "--retry"}, "", errRekeyFlagsExclusive},
	}
	for _, tt := range tests {
		cmd := newStorageRekeyCmd()
		if err := cmd.ParseFlags(tt.args); err != nil {
			t.Fatalf("ParseFlags(%v): %v", tt.args, err)
		}
		got, err := readRekeyAction(cmd)
		if got != tt.want || !errors.Is(err, tt.err) {
			t.Errorf("readRekeyAction(%v) = %q, %v; want %q, %v", tt.args, got, err, tt.want, tt.err)
		}
	}
}

func TestPercent(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		done, total int64
		want        string
	}{{0, 0, "100%"}, {1, 3, "33.3%"}, {3, 3, "100.0%"}} {
		if got := percent(tt.done, tt.total); got != tt.want {
			t.Errorf("percent(%d, %d) = %q, want %q", tt.done, tt.total, got, tt.want)
		}
	}
}
