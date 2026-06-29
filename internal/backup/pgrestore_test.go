package backup

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestRestoreArgs(t *testing.T) {
	t.Parallel()
	args := restoreArgs("kukatko")
	for _, want := range []string{
		"--format=custom", "--clean", "--if-exists",
		"--no-owner", "--no-privileges", "--single-transaction", "--dbname=kukatko",
	} {
		if !slices.Contains(args, want) {
			t.Errorf("restoreArgs() = %v, missing %q", args, want)
		}
	}
}

func TestConnEnv(t *testing.T) {
	t.Parallel()
	const dsn = "postgresql://user:secret@db.example.com:5433/kukatko"
	env, database, err := connEnv(dsn)
	if err != nil {
		t.Fatalf("connEnv() error = %v", err)
	}
	if database != "kukatko" {
		t.Errorf("database = %q, want kukatko", database)
	}
	wants := []string{
		"PGHOST=db.example.com",
		"PGPORT=5433",
		"PGUSER=user",
		"PGPASSWORD=secret",
		"PGDATABASE=kukatko",
	}
	for _, want := range wants {
		if !slices.Contains(env, want) {
			t.Errorf("connEnv() env = %v, missing %q", env, want)
		}
	}
}

func TestConnEnv_invalid(t *testing.T) {
	t.Parallel()
	if _, _, err := connEnv("://nonsense"); !errors.Is(err, ErrInvalidDSN) {
		t.Errorf("connEnv(bad) error = %v, want ErrInvalidDSN", err)
	}
}

func TestPgRestorer_command(t *testing.T) {
	t.Parallel()
	const dsn = "postgresql://user:secret@localhost:5432/kukatko"
	restorer := NewPgRestorer(dsn)
	cmd, err := restorer.command(context.Background())
	if err != nil {
		t.Fatalf("command() error = %v", err)
	}
	if !strings.HasSuffix(cmd.Path, pgRestoreBinary) && cmd.Args[0] != pgRestoreBinary {
		t.Errorf("command path = %q, want pg_restore", cmd.Path)
	}
	// The password must travel via PGPASSWORD in the environment, never on the
	// argument list (which would expose it in `ps`).
	for _, arg := range cmd.Args {
		if strings.Contains(arg, "secret") {
			t.Errorf("password leaked into argument %q; it must go via PGPASSWORD", arg)
		}
	}
	if !slices.Contains(cmd.Env, "PGPASSWORD=secret") {
		t.Error("command env missing PGPASSWORD")
	}
	if !slices.Contains(cmd.Args, "--dbname=kukatko") {
		t.Errorf("command args = %v, missing --dbname=kukatko", cmd.Args)
	}
}

func TestPgRestorer_command_invalidDSN(t *testing.T) {
	t.Parallel()
	if _, err := NewPgRestorer("://nope").command(context.Background()); !errors.Is(err, ErrInvalidDSN) {
		t.Errorf("command() error = %v, want ErrInvalidDSN", err)
	}
}

func TestPgRestorer_Restore_missingBinary(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath(pgRestoreBinary); err == nil {
		t.Skip("pg_restore is installed; cannot exercise the missing-binary path")
	}
	restorer := NewPgRestorer("postgresql://user:pass@localhost/db")
	if err := restorer.Restore(context.Background(), strings.NewReader("")); !errors.Is(err, ErrPgRestoreMissing) {
		t.Errorf("Restore() without pg_restore = %v, want ErrPgRestoreMissing", err)
	}
}

func TestPgRestorer_Restore_realIfAvailable(t *testing.T) {
	t.Parallel()
	if !PgRestoreAvailable() {
		t.Skip("pg_restore not installed; skipping real-restore check")
	}
	// Feeding garbage (not a valid custom-format archive) into pg_restore against
	// an unreachable database must fail, exercising the streamed run + error path
	// without needing a live database.
	restorer := NewPgRestorer("postgresql://127.0.0.1:1/nonexistent?connect_timeout=1")
	err := restorer.Restore(context.Background(), io.NopCloser(strings.NewReader("not-an-archive")))
	if err == nil {
		t.Error("Restore() of garbage against an unreachable DB returned nil error")
	}
}
