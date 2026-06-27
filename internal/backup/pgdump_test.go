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

func TestDumpArgs(t *testing.T) {
	t.Parallel()
	args := dumpArgs()
	for _, want := range []string{"--format=custom", "--no-owner", "--no-privileges"} {
		if !slices.Contains(args, want) {
			t.Errorf("dumpArgs() = %v, missing %q", args, want)
		}
	}
}

func TestPgDumper_command(t *testing.T) {
	t.Parallel()
	const dsn = "postgresql://user:secret@localhost:5432/kukatko"
	dumper := NewPgDumper(dsn)
	cmd := dumper.command(context.Background())

	if !strings.HasSuffix(cmd.Path, pgDumpBinary) && cmd.Args[0] != pgDumpBinary {
		t.Errorf("command path = %q, want pg_dump", cmd.Path)
	}
	if !slices.Contains(cmd.Args, "--format=custom") {
		t.Errorf("command args = %v, missing --format=custom", cmd.Args)
	}
	// The DSN must travel via PGDATABASE, never on the argument list (which would
	// expose the password in `ps`).
	for _, arg := range cmd.Args {
		if strings.Contains(arg, "secret") {
			t.Errorf("DSN leaked into argument %q; it must go via PGDATABASE", arg)
		}
	}
	wantEnv := pgDatabaseEnv + "=" + dsn
	if !slices.Contains(cmd.Env, wantEnv) {
		t.Errorf("command env missing %q", wantEnv)
	}
}

func TestPgDumper_Dump_missingBinary(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath(pgDumpBinary); err == nil {
		t.Skip("pg_dump is installed; cannot exercise the missing-binary path")
	}
	dumper := NewPgDumper("postgresql://localhost/db")
	if _, err := dumper.Dump(context.Background()); !errors.Is(err, ErrPgDumpMissing) {
		t.Errorf("Dump() without pg_dump = %v, want ErrPgDumpMissing", err)
	}
}

func TestPgDumper_Dump_realIfAvailable(t *testing.T) {
	t.Parallel()
	if !PgDumpAvailable() {
		t.Skip("pg_dump not installed; skipping real-dump check")
	}
	// Without a reachable database the dump must still start; the failure surfaces
	// on Close (process exit), which is what we assert here.
	dumper := NewPgDumper("postgresql://127.0.0.1:1/nonexistent?connect_timeout=1")
	reader, err := dumper.Dump(context.Background())
	if err != nil {
		// Starting may itself fail fast on some platforms; that is acceptable.
		return
	}
	_, _ = io.Copy(io.Discard, reader)
	if closeErr := reader.Close(); closeErr == nil {
		t.Error("Close() of a dump against an unreachable DB returned nil error")
	}
}
