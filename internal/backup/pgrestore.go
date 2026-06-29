package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	// pgRestoreBinary is the pg_restore executable shelled out to for restores.
	pgRestoreBinary = "pg_restore"
)

// Sentinel errors for pg_restore.
var (
	// ErrPgRestoreMissing indicates the pg_restore executable is not installed or
	// not on PATH, so database restores cannot run.
	ErrPgRestoreMissing = errors.New("backup: pg_restore not installed")
	// ErrInvalidDSN indicates the database connection string could not be parsed
	// into the host/user/password/database components pg_restore needs.
	ErrInvalidDSN = errors.New("backup: invalid database DSN")
)

// Restorer restores a database from a streamed archive. The archive (a pg_dump
// custom-format dump) is read sequentially from the supplied reader, so it can
// be piped straight from S3 without buffering the whole dump.
type Restorer interface {
	// Restore reads a custom-format dump from archive and restores it into the
	// target database. It returns a non-nil error if the restore fails.
	Restore(ctx context.Context, archive io.Reader) error
}

// pgRestorer restores a PostgreSQL database by shelling out to pg_restore,
// reading the custom-format archive from standard input. The DSN is parsed into
// individual libpq environment variables (PGHOST, PGPORT, PGUSER, PGPASSWORD,
// PGDATABASE) so the password never appears in the process argument list — only
// the bare database name is passed via --dbname, which is not a secret.
type pgRestorer struct {
	dsn    string
	binary string
}

// compile-time assertion that pgRestorer satisfies Restorer.
var _ Restorer = (*pgRestorer)(nil)

// NewPgRestorer returns a Restorer that pipes a custom-format dump into
// pg_restore for the database at dsn. The dsn is a libpq connection string or
// URI; it is only ever passed to pg_restore via the environment, never logged.
func NewPgRestorer(dsn string) *pgRestorer {
	return &pgRestorer{dsn: dsn, binary: pgRestoreBinary}
}

// PgRestoreAvailable reports whether the pg_restore executable can be found on
// PATH.
func PgRestoreAvailable() bool {
	_, err := exec.LookPath(pgRestoreBinary)
	return err == nil
}

// restoreArgs returns the static pg_restore arguments. The custom format is read
// from stdin; --clean --if-exists drops existing objects (ignoring absent ones)
// before recreating them so the restore overwrites the current database;
// --no-owner --no-privileges match the dump's options; and --single-transaction
// wraps the whole restore in one transaction so an interrupted or failing
// restore rolls back cleanly instead of leaving the database half-restored.
func restoreArgs(database string) []string {
	return []string{
		"--format=custom",
		"--clean",
		"--if-exists",
		"--no-owner",
		"--no-privileges",
		"--single-transaction",
		"--dbname=" + database,
	}
}

// connEnv parses the DSN and returns the libpq environment variables that carry
// the connection (so the password stays out of the argument list) together with
// the bare database name for --dbname. It returns ErrInvalidDSN if the DSN
// cannot be parsed or names no database.
func connEnv(dsn string) (env []string, database string, err error) {
	cfg, parseErr := pgx.ParseConfig(dsn)
	if parseErr != nil {
		return nil, "", fmt.Errorf("%w: %w", ErrInvalidDSN, parseErr)
	}
	if cfg.Database == "" {
		return nil, "", fmt.Errorf("%w: no database name", ErrInvalidDSN)
	}
	env = []string{
		"PGHOST=" + cfg.Host,
		"PGPORT=" + strconv.Itoa(int(cfg.Port)),
		"PGUSER=" + cfg.User,
		"PGPASSWORD=" + cfg.Password,
		"PGDATABASE=" + cfg.Database,
	}
	return env, cfg.Database, nil
}

// command builds the pg_restore *exec.Cmd for a restore, with the connection
// supplied through libpq environment variables so the password stays off the
// argument list. It returns ErrInvalidDSN if the DSN cannot be parsed.
func (r *pgRestorer) command(ctx context.Context) (*exec.Cmd, error) {
	env, database, err := connEnv(r.dsn)
	if err != nil {
		return nil, err
	}
	//nolint:gosec // G204: binary is the fixed "pg_restore" constant and the args are static.
	cmd := exec.CommandContext(ctx, r.binary, restoreArgs(database)...)
	cmd.Env = append(os.Environ(), env...)
	return cmd, nil
}

// Restore pipes the custom-format dump in archive into pg_restore and waits for
// it to finish, returning a wrapped error (including trimmed stderr) when the
// process fails. It returns ErrPgRestoreMissing when pg_restore is not installed
// and ErrInvalidDSN when the configured DSN cannot be parsed. pg_restore does
// not echo the connection password to stderr, so captured output is safe to
// surface.
func (r *pgRestorer) Restore(ctx context.Context, archive io.Reader) error {
	if !PgRestoreAvailable() {
		return ErrPgRestoreMissing
	}
	cmd, err := r.command(ctx)
	if err != nil {
		return err
	}
	cmd.Stdin = archive
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if runErr := cmd.Run(); runErr != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("backup: pg_restore failed: %w: %s", runErr, msg)
		}
		return fmt.Errorf("backup: pg_restore failed: %w", runErr)
	}
	return nil
}
