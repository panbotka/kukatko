package main

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/reset"
)

// unreachableDSN points at a port nothing listens on, so a command that gets as
// far as opening the database fails at once with a connection error. It is how a
// test tells "the guard stopped this" apart from "the guard let it through".
const unreachableDSN = "postgres://user:pass@127.0.0.1:1/kukatko?sslmode=disable&connect_timeout=1"

// executeResetCmd runs the root command with the given stdin and arguments,
// capturing combined output. Stdin is always a buffer, which is what makes the
// run non-interactive — the same thing a script or a cron job would give it.
func executeResetCmd(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()

	cmd := newRootCmd("kukatko")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	return buf.String(), cmd.Execute()
}

// TestMaintenanceCmd_hasResetChild verifies the wipe is reachable as
// `kukatko maintenance reset`.
func TestMaintenanceCmd_hasResetChild(t *testing.T) {
	t.Parallel()

	var found bool
	for _, sub := range newMaintenanceCmd().Commands() {
		if sub.Name() == "reset" {
			found = true
		}
	}
	if !found {
		t.Error("maintenance command has no reset subcommand")
	}
}

// TestResetCmd_flagsRegistered verifies every guard has a flag and that deleting
// is off by default.
func TestResetCmd_flagsRegistered(t *testing.T) {
	t.Parallel()

	flags := newMaintenanceResetCmd().Flags()
	for _, name := range []string{flagExecute, flagConfirmDatabase, flagConfirmBucket, flagForce, flagOrphanSweep} {
		if flags.Lookup(name) == nil {
			t.Errorf("reset command has no --%s flag", name)
		}
	}
	for _, name := range []string{flagExecute, flagForce, flagOrphanSweep} {
		if got := flags.Lookup(name).DefValue; got != "false" {
			t.Errorf("--%s defaults to %q, want false", name, got)
		}
	}
}

// TestResetCmd_refusesNonInteractiveWithoutForce verifies a wipe requested from
// something that is not a terminal is refused before anything is opened: the DSN
// is unreachable, so reaching the database at all would surface a different
// error.
func TestResetCmd_refusesNonInteractiveWithoutForce(t *testing.T) {
	t.Setenv("KUKATKO_DATABASE_URL", unreachableDSN)

	_, err := executeResetCmd(t, "kukatko\n", "maintenance", "reset", "--execute")
	if !errors.Is(err, errResetNotInteractive) {
		t.Errorf("reset --execute from a non-terminal = %v, want errResetNotInteractive", err)
	}
}

// TestResetCmd_dryRunNeedsNoForce verifies the non-interactive guard applies only
// to a run that would delete: a dry run from a script is allowed and gets as far
// as opening the database, which is where the unreachable DSN stops it.
func TestResetCmd_dryRunNeedsNoForce(t *testing.T) {
	t.Setenv("KUKATKO_DATABASE_URL", unreachableDSN)

	_, err := executeResetCmd(t, "", "maintenance", "reset")
	if errors.Is(err, errResetNotInteractive) {
		t.Fatal("a dry run was refused as non-interactive; only --execute needs --force")
	}
	if err == nil || !strings.Contains(err.Error(), "connecting to database") {
		t.Errorf("dry run error = %v, want a connection failure (proving the guard let it through)", err)
	}
}

// TestResetCmd_forcedRunPassesTheGuard verifies --force lets a non-interactive
// wipe past the refusal, so automation that really means it is not blocked.
func TestResetCmd_forcedRunPassesTheGuard(t *testing.T) {
	t.Setenv("KUKATKO_DATABASE_URL", unreachableDSN)

	_, err := executeResetCmd(t, "",
		"maintenance", "reset", "--execute", "--force", "--confirm-database", "kukatko")
	if errors.Is(err, errResetNotInteractive) {
		t.Fatal("--force did not lift the non-interactive refusal")
	}
	if err == nil || !strings.Contains(err.Error(), "connecting to database") {
		t.Errorf("forced run error = %v, want a connection failure", err)
	}
}

// TestConfirmDatabaseName verifies the typed confirmation is read from the flag
// when given and from the operator otherwise, and that typing nothing is not a
// confirmation.
func TestConfirmDatabaseName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		flags   resetFlags
		stdin   string
		want    string
		wantErr error
	}{
		{name: "from the flag", flags: resetFlags{confirm: "kukatko"}, want: "kukatko"},
		{name: "typed at the prompt", stdin: "kukatko\n", want: "kukatko"},
		{name: "typed with surrounding space", stdin: "  kukatko  \n", want: "kukatko"},
		{name: "a wrong name is still returned", stdin: "nope\n", want: "nope"},
		{name: "nothing typed", stdin: "\n", wantErr: errResetNotConfirmed},
		{name: "closed input", stdin: "", wantErr: errResetNotConfirmed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := &cobra.Command{}
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetIn(strings.NewReader(tt.stdin))

			got, err := confirmDatabaseName(cmd, tt.flags, bufio.NewScanner(cmd.InOrStdin()), "kukatko")
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("confirmDatabaseName() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("confirmDatabaseName() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("confirmDatabaseName() = %q, want %q", got, tt.want)
			}
			if tt.flags.confirm == "" && !strings.Contains(buf.String(), "kukatko") {
				t.Errorf("prompt %q does not name the database to type", buf.String())
			}
		})
	}
}

// TestConfirmBucketName verifies the bucket confirmation is read from the flag
// when given and from the operator otherwise, that typing nothing is not a
// confirmation, and that a store with no bucket asks nothing — while still
// carrying a stray typed name through, so the service can refuse a run aimed at a
// bucket this store does not have.
func TestConfirmBucketName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		flags   resetFlags
		bucket  string
		stdin   string
		want    string
		wantErr error
	}{
		{name: "from the flag", flags: resetFlags{confirmStore: "kukatko-dev"}, bucket: "kukatko-dev", want: "kukatko-dev"},
		{name: "typed at the prompt", bucket: "kukatko-dev", stdin: "kukatko-dev\n", want: "kukatko-dev"},
		{name: "typed with surrounding space", bucket: "kukatko-dev", stdin: "  kukatko-dev \n", want: "kukatko-dev"},
		{name: "a wrong name is still returned", bucket: "kukatko-dev", stdin: "kotrzina-photos\n", want: "kotrzina-photos"},
		{name: "nothing typed", bucket: "kukatko-dev", stdin: "\n", wantErr: errResetBucketNotConfirmed},
		{name: "closed input", bucket: "kukatko-dev", stdin: "", wantErr: errResetBucketNotConfirmed},
		{name: "no bucket configured", stdin: "", want: ""},
		{
			name:  "no bucket configured, one typed anyway",
			flags: resetFlags{confirmStore: "kotrzina-photos"},
			want:  "kotrzina-photos",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := &cobra.Command{}
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetIn(strings.NewReader(tt.stdin))

			got, err := confirmBucketName(cmd, tt.flags, bufio.NewScanner(cmd.InOrStdin()), tt.bucket)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("confirmBucketName() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("confirmBucketName() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("confirmBucketName() = %q, want %q", got, tt.want)
			}
			if tt.bucket == "" && buf.Len() != 0 {
				t.Errorf("a store with no bucket still prompted: %q", buf.String())
			}
		})
	}
}

// TestConfirmTarget_bothAnswersFromOneStream verifies the two prompts read
// consecutive lines of the same input. Giving each prompt its own bufio.Scanner
// would lose the second answer to the first scanner's read-ahead buffer, which
// looks exactly like an operator who typed nothing.
func TestConfirmTarget_bothAnswersFromOneStream(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetIn(strings.NewReader("kukatko\nkukatko-dev\n"))

	database, bucket, err := confirmTarget(cmd, resetFlags{},
		reset.Target{Database: "kukatko", Bucket: "kukatko-dev"})
	if err != nil {
		t.Fatalf("confirmTarget() error = %v", err)
	}
	if database != "kukatko" || bucket != "kukatko-dev" {
		t.Errorf("confirmTarget() = (%q, %q), want (kukatko, kukatko-dev)", database, bucket)
	}
	for _, want := range []string{"database name (kukatko)", "bucket name (kukatko-dev)"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("prompts %q do not include %q", buf.String(), want)
		}
	}
}

// TestConfiguredBucket verifies the name an operator is asked to type comes from
// the backend that is actually in use: a bucket left in the config while the
// filesystem backend runs is not a bucket anyone is about to empty, and must not
// become a confirmable one.
func TestConfiguredBucket(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		backend string
		bucket  string
		want    string
	}{
		{name: "r2 backend", backend: config.StorageBackendR2, bucket: "kukatko-dev", want: "kukatko-dev"},
		{name: "fs backend with a leftover bucket", backend: config.StorageBackendFS, bucket: "kotrzina-photos"},
		{name: "unset backend", bucket: "kotrzina-photos"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &config.Config{}
			cfg.Storage.Backend = tt.backend
			cfg.Storage.R2.Bucket = tt.bucket
			if got := configuredBucket(cfg); got != tt.want {
				t.Errorf("configuredBucket() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestInteractiveInput verifies only a character device counts as a terminal: a
// buffer, a pipe and /dev/null are all non-interactive.
func TestInteractiveInput(t *testing.T) {
	t.Parallel()

	if interactiveInput(strings.NewReader("")) {
		t.Error("a string reader was reported as interactive")
	}
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("opening %s: %v", os.DevNull, err)
	}
	defer func() { _ = devNull.Close() }()
	if interactiveInput(devNull) {
		t.Errorf("%s was reported as interactive", os.DevNull)
	}
	regular, err := os.Open("resetcmd.go")
	if err != nil {
		t.Fatalf("opening a regular file: %v", err)
	}
	defer func() { _ = regular.Close() }()
	if interactiveInput(regular) {
		t.Error("a regular file was reported as interactive")
	}
}

// TestOperatorIdentity verifies the audit trail gets a user@host identity, and
// falls back to a placeholder rather than an empty string.
func TestOperatorIdentity(t *testing.T) {
	t.Setenv("USER", "operator")
	t.Setenv("LOGNAME", "")

	got := operatorIdentity()
	if !strings.HasPrefix(got, "operator@") || strings.HasSuffix(got, "@") {
		t.Errorf("operatorIdentity() = %q, want operator@<host>", got)
	}

	t.Setenv("USER", "")
	if got := operatorIdentity(); !strings.HasPrefix(got, unknownIdentity+"@") {
		t.Errorf("operatorIdentity() without USER = %q, want %s@<host>", got, unknownIdentity)
	}
}

// TestPrintResetPreflight verifies the operator is shown the target, the
// non-empty catalogue tables, the objects per prefix and what will be preserved.
func TestPrintResetPreflight(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	printResetPreflight(cmd, reset.Preflight{
		Target:     reset.Target{Host: "localhost", Port: 5432, Database: "kukatko"},
		Connection: reset.Connection{Database: "kukatko", ServerPort: 5432},
		Counts: reset.Counts{
			Catalogue: []reset.TableCount{{Table: "photos", Rows: 280}, {Table: "albums"}},
			Preserved: []reset.TableCount{{Table: "users", Rows: 2}},
		},
		Storage: reset.StoragePlan{
			Referenced: reset.PrefixCounts{Originals: 280, Thumbnails: 2240, Sidecars: 280},
		},
	})

	out := buf.String()
	for _, want := range []string{
		"localhost:5432/kukatko", "local socket", "photos", "280", "2240",
		"orphan sweep off", "preserved (never touched)", "users",
		"local filesystem (no bucket)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("preflight output does not mention %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "albums") {
		t.Errorf("preflight lists albums, which holds no rows:\n%s", out)
	}
}

// TestPrintResetPreflight_sweep verifies a sweep additionally reports what the
// store holds and how many foreign keys it will leave alone.
func TestPrintResetPreflight_sweep(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	printResetStoragePlan(cmd, reset.Target{Database: "kukatko", Bucket: "kukatko-dev"}, reset.StoragePlan{
		Referenced: reset.PrefixCounts{Originals: 2},
		Stored:     reset.PrefixCounts{Originals: 5, Thumbnails: 1},
		Foreign:    3,
		Sweep:      true,
	})

	out := buf.String()
	for _, want := range []string{"bucket kukatko-dev", "orphan sweep on", "5 original", "3 key(s) outside"} {
		if !strings.Contains(out, want) {
			t.Errorf("sweep plan output does not mention %q:\n%s", want, out)
		}
	}
}

// TestPrintResetResult verifies the before/after summary is printed, a table that
// survived the truncation is flagged, and the preserved counts are shown as the
// proof that the accounts and the trail are still there.
func TestPrintResetResult(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	printResetResult(cmd, reset.Result{
		Before: reset.Counts{Catalogue: []reset.TableCount{{Table: "photos", Rows: 280}}},
		After: reset.Counts{
			Catalogue: []reset.TableCount{{Table: "photos", Rows: 1}},
			Preserved: []reset.TableCount{{Table: "users", Rows: 2}, {Table: "audit_log", Rows: 9}},
		},
		Storage: reset.StorageResult{
			Deleted: 12, Missing: 3, Foreign: 1, Failed: 1,
			Failures:          []string{"2024/05/a.jpg: access denied"},
			ThumbCacheCleared: 4,
		},
	})

	out := buf.String()
	for _, want := range []string{
		"12 object(s) deleted", "access denied", "cleared for 4 file hash(es)",
		"280 row(s) before, 1 row(s) after", "WARNING: photos still holds 1",
		"users=2 audit_log=9",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("result output does not mention %q:\n%s", want, out)
		}
	}
}

// TestPrintResetResult_abortedRun verifies a run that never reached the
// truncation prints what happened in the store without claiming a before/after
// comparison it does not have.
func TestPrintResetResult_abortedRun(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	printResetResult(cmd, reset.Result{Storage: reset.StorageResult{Failed: 2}})

	out := buf.String()
	if !strings.Contains(out, "2 failed") {
		t.Errorf("aborted output does not report the failures:\n%s", out)
	}
	if strings.Contains(out, "row(s) after") {
		t.Errorf("aborted output claims an after count it never measured:\n%s", out)
	}
}

// TestPrintResetResult_guardedRun verifies a run a guard stopped before it did
// anything prints no summary at all, so an all-zero report cannot bury the error
// that explains why nothing happened.
func TestPrintResetResult_guardedRun(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	printResetResult(cmd, reset.Result{})

	if buf.Len() != 0 {
		t.Errorf("a guarded run printed a summary:\n%s", buf.String())
	}
}

// TestServerAddrOrSocket verifies an empty server address — what Postgres reports
// over a Unix socket — is rendered as something readable.
func TestServerAddrOrSocket(t *testing.T) {
	t.Parallel()

	if got := serverAddrOrSocket(""); got != "local socket" {
		t.Errorf("serverAddrOrSocket(\"\") = %q, want local socket", got)
	}
	if got := serverAddrOrSocket("10.0.0.5"); got != "10.0.0.5" {
		t.Errorf("serverAddrOrSocket(10.0.0.5) = %q, want it unchanged", got)
	}
}
