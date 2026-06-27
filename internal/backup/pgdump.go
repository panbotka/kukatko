package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const (
	// pgDumpBinary is the pg_dump executable shelled out to for database dumps.
	pgDumpBinary = "pg_dump"
	// pgDatabaseEnv is the libpq environment variable that supplies the
	// connection. libpq expands a value containing "=" or a "postgresql://" URI
	// into a full connection string, so passing the DSN here keeps it out of the
	// process argument list (and thus out of `ps`), unlike a --dbname flag.
	pgDatabaseEnv = "PGDATABASE"
)

// ErrPgDumpMissing indicates the pg_dump executable is not installed or not on
// PATH, so database dumps cannot be taken.
var ErrPgDumpMissing = errors.New("backup: pg_dump not installed")

// pgDumper dumps a PostgreSQL database by shelling out to pg_dump in the custom,
// compressed archive format. The DSN is passed via the PGDATABASE environment
// variable rather than a command-line flag so the password never appears in the
// process argument list.
type pgDumper struct {
	dsn    string
	binary string
}

// compile-time assertion that pgDumper satisfies Dumper.
var _ Dumper = (*pgDumper)(nil)

// NewPgDumper returns a Dumper that streams pg_dump output for the database at
// dsn. The dsn is a libpq connection string or URI; it is only ever passed to
// pg_dump via the environment, never logged.
func NewPgDumper(dsn string) *pgDumper {
	return &pgDumper{dsn: dsn, binary: pgDumpBinary}
}

// PgDumpAvailable reports whether the pg_dump executable can be found on PATH.
func PgDumpAvailable() bool {
	_, err := exec.LookPath(pgDumpBinary)
	return err == nil
}

// dumpArgs returns the static pg_dump arguments: the custom compressed archive
// format, with ownership and privilege statements omitted so the dump restores
// cleanly into a fresh database with a possibly different owner role.
func dumpArgs() []string {
	return []string{"--format=custom", "--no-owner", "--no-privileges"}
}

// command builds the pg_dump *exec.Cmd for a dump, with the DSN supplied through
// the PGDATABASE environment variable so it stays out of the argument list.
func (d *pgDumper) command(ctx context.Context) *exec.Cmd {
	//nolint:gosec // G204: binary is the fixed "pg_dump" constant and the args are static.
	cmd := exec.CommandContext(ctx, d.binary, dumpArgs()...)
	cmd.Env = append(os.Environ(), pgDatabaseEnv+"="+d.dsn)
	return cmd
}

// Dump starts pg_dump and returns a reader over its standard output. Closing the
// reader waits for pg_dump to exit and returns its error (with captured stderr)
// when it failed. It returns ErrPgDumpMissing when pg_dump is not installed.
func (d *pgDumper) Dump(ctx context.Context) (io.ReadCloser, error) {
	if !PgDumpAvailable() {
		return nil, ErrPgDumpMissing
	}
	cmd := d.command(ctx)
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("backup: pg_dump stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("backup: starting pg_dump: %w", err)
	}
	return &dumpReader{stdout: stdout, cmd: cmd, stderr: stderr}, nil
}

// dumpReader streams pg_dump's stdout and, on Close, waits for the process and
// surfaces a non-zero exit as an error annotated with the captured stderr.
type dumpReader struct {
	stdout io.ReadCloser
	cmd    *exec.Cmd
	stderr *bytes.Buffer
}

// Read delegates to pg_dump's stdout pipe, passing io.EOF through unchanged so
// io.Copy terminates normally.
func (r *dumpReader) Read(p []byte) (int, error) {
	return r.stdout.Read(p) //nolint:wrapcheck // io.EOF must pass through verbatim.
}

// Close closes the stdout pipe and waits for pg_dump to exit, returning a wrapped
// error (including trimmed stderr) when the process failed. pg_dump does not echo
// the connection password to stderr, so the captured output is safe to surface.
func (r *dumpReader) Close() error {
	_ = r.stdout.Close()
	if err := r.cmd.Wait(); err != nil {
		if msg := strings.TrimSpace(r.stderr.String()); msg != "" {
			return fmt.Errorf("backup: pg_dump failed: %w: %s", err, msg)
		}
		return fmt.Errorf("backup: pg_dump failed: %w", err)
	}
	return nil
}
