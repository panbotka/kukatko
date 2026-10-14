package database

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationFS holds the embedded SQL migration files applied on startup.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// migrationsDir is the directory inside migrationFS that holds the SQL files.
const migrationsDir = "migrations"

// schemaMigrationsDDL creates the bookkeeping table that records which
// migrations have already been applied. It is created by the runner (not by a
// migration) so an empty database can be bootstrapped.
const schemaMigrationsDDL = `CREATE TABLE IF NOT EXISTS schema_migrations (
	version    BIGINT PRIMARY KEY,
	name       TEXT NOT NULL,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`

// migrationNamePattern matches migration filenames of the form 0001_init.sql:
// a numeric version, an underscore, a lowercase snake-case name, and the .sql
// extension.
var migrationNamePattern = regexp.MustCompile(`^(\d+)_([a-z0-9_]+)\.sql$`)

// Sentinel errors returned while loading migrations so callers (and tests) can
// match them with errors.Is.
var (
	// errBadMigrationName indicates a migration filename does not match the
	// required NNNN_name.sql pattern.
	errBadMigrationName = errors.New("database: invalid migration filename (want NNNN_name.sql)")
	// errDuplicateMigration indicates two migrations declare the same version.
	errDuplicateMigration = errors.New("database: duplicate migration version")
)

// migration is a single embedded SQL migration with its parsed version and the
// raw SQL to execute.
type migration struct {
	version  int64
	name     string
	filename string
	sql      string
}

// migrationLockKey is the fixed pg_advisory_lock key every migrator takes for
// the whole run. Any constant works as long as nothing else in the database uses
// it; this one spells "kukatko" in ASCII digits so it is recognisable in
// pg_locks.
const migrationLockKey int64 = 0x6b756b61746b6f

// Migrate applies every embedded migration not yet recorded in
// schema_migrations, in ascending version order, each within its own
// transaction. It is idempotent: already-applied migrations are skipped. The
// filenames applied during this call are returned (empty when the database is
// already up to date). Errors from loading, recording, or executing a migration
// are returned wrapped.
//
// The whole run holds a session-level advisory lock (migrationLockKey) on one
// dedicated connection, so two migrators started at once — a `serve` start and a
// CLI subcommand, say — serialise: the second waits, then reads the version list
// the first one left behind and finds nothing to do, instead of racing the first
// one's CREATE TABLE IF NOT EXISTS and failing on a duplicate catalogue row. A
// session lock (not pg_advisory_xact_lock) because every file commits in its own
// transaction; the lock has to outlive them all.
//
// The waiting is unbounded on purpose. A lock_timeout (on the advisory lock or on
// the files' own ALTERs) was considered and rejected: a migration queued behind a
// long query would then fail, and a `serve` start that fails restarts, turning
// one slow upgrade into a restart loop that retries the same wait forever. A
// stuck upgrade is visible in pg_stat_activity and is the operator's call.
func Migrate(ctx context.Context, pool *pgxpool.Pool) (applied []string, err error) {
	migrations, err := loadMigrations(migrationFS, migrationsDir)
	if err != nil {
		return nil, err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring migration connection: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockKey); err != nil {
		return nil, fmt.Errorf("taking migration lock: %w", err)
	}
	defer func() {
		err = errors.Join(err, releaseMigrationLock(conn.Conn()))
	}()

	if err := ensureSchemaMigrationsTable(ctx, conn); err != nil {
		return nil, err
	}
	versions, err := appliedVersions(ctx, conn)
	if err != nil {
		return nil, err
	}
	return applyPending(ctx, conn, migrations, versions)
}

// releaseMigrationLock gives the advisory lock back. It runs on a fresh
// background context so a cancelled run still unlocks; if even that fails, the
// connection is closed, which ends the session and drops the lock with it — a
// pooled connection must never go back to the pool still holding it, or every
// later migrator would wait on a lock nobody is going to release.
func releaseMigrationLock(conn *pgx.Conn) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", migrationLockKey); err != nil {
		if closeErr := conn.Close(ctx); closeErr != nil {
			return errors.Join(fmt.Errorf("releasing migration lock: %w", err),
				fmt.Errorf("closing migration connection: %w", closeErr))
		}
		return nil
	}
	return nil
}

// Pending returns the filenames of the embedded migrations the database has not
// applied yet, in the order Migrate would apply them. It is SchemaStatusOf's
// Pending list; see there for the contract.
func Pending(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	status, err := SchemaStatusOf(ctx, pool)
	if err != nil {
		return nil, err
	}
	return status.Pending, nil
}

// SchemaStatus is where a database's schema stands relative to the migrations
// this binary embeds.
type SchemaStatus struct {
	// Current is the highest migration version the database has recorded, 0
	// when it has recorded none (or has no schema_migrations table at all).
	Current int64
	// Expected is the highest version this binary embeds — the version the
	// database is at once Migrate has run.
	Expected int64
	// Pending lists the embedded migrations not yet recorded, in the order
	// Migrate would apply them.
	Pending []string
}

// Behind reports whether the database lacks a migration this binary embeds.
// A database ahead of the binary (Current > Expected, nothing pending) is not
// behind: that is an older binary on a newer schema, which no migration fixes.
func (s SchemaStatus) Behind() bool {
	return len(s.Pending) > 0
}

// SchemaStatusOf reads which embedded migrations the database has applied. It
// only reads: a database without schema_migrations reports every migration
// pending and creates nothing. It takes no lock, so it is a snapshot — good for
// refusing to run against a stale schema or for telling an operator what a
// migration is about to change, not for deciding inside Migrate what to apply.
func SchemaStatusOf(ctx context.Context, pool *pgxpool.Pool) (SchemaStatus, error) {
	migrations, err := loadMigrations(migrationFS, migrationsDir)
	if err != nil {
		return SchemaStatus{}, err
	}
	// Looked up in current_schema(), where Migrate's CREATE TABLE IF NOT EXISTS
	// would put it, not through the whole search_path.
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass(
		quote_ident(current_schema()) || '.schema_migrations') IS NOT NULL`).Scan(&exists); err != nil {
		return SchemaStatus{}, fmt.Errorf("checking for schema_migrations: %w", err)
	}
	versions := map[int64]bool{}
	if exists {
		if versions, err = appliedVersions(ctx, pool); err != nil {
			return SchemaStatus{}, err
		}
	}
	return schemaStatusFrom(migrations, versions), nil
}

// schemaStatusFrom compares the embedded migrations (version-sorted, as
// loadMigrations returns them) with the set of versions a database recorded.
func schemaStatusFrom(migrations []migration, applied map[int64]bool) SchemaStatus {
	var status SchemaStatus
	for version := range applied {
		status.Current = max(status.Current, version)
	}
	for _, mig := range migrations {
		status.Expected = max(status.Expected, mig.version)
		if !applied[mig.version] {
			status.Pending = append(status.Pending, mig.filename)
		}
	}
	return status
}

// querier is the slice of pgx both a pooled connection and the pool itself
// provide: what the bookkeeping reads and the per-file transactions need.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Begin(ctx context.Context) (pgx.Tx, error)
}

// loadMigrations reads every "*.sql" file from dir in fsys, parses each filename
// into a version and name, sorts the result by ascending version, and validates
// that versions are unique. The returned slice is the canonical ordered set of
// migrations to apply. Non-SQL files and subdirectories are ignored.
func loadMigrations(fsys fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("reading migrations dir %q: %w", dir, err)
	}

	migrations := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		mig, err := readMigration(fsys, dir, entry.Name())
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, mig)
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})
	if err := validateMigrations(migrations); err != nil {
		return nil, err
	}
	return migrations, nil
}

// readMigration parses filename and reads its SQL body from fsys, returning the
// assembled migration or a wrapped error.
func readMigration(fsys fs.FS, dir, filename string) (migration, error) {
	version, name, err := parseMigrationFilename(filename)
	if err != nil {
		return migration{}, err
	}
	content, err := fs.ReadFile(fsys, path.Join(dir, filename))
	if err != nil {
		return migration{}, fmt.Errorf("reading migration %q: %w", filename, err)
	}
	return migration{version: version, name: name, filename: filename, sql: string(content)}, nil
}

// parseMigrationFilename extracts the numeric version and snake-case name from a
// migration filename of the form "0001_init.sql". It returns errBadMigrationName
// if the filename does not match the required pattern.
func parseMigrationFilename(filename string) (int64, string, error) {
	groups := migrationNamePattern.FindStringSubmatch(filename)
	if groups == nil {
		return 0, "", fmt.Errorf("%w: %q", errBadMigrationName, filename)
	}
	version, err := strconv.ParseInt(groups[1], 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("parsing version from %q: %w", filename, err)
	}
	return version, groups[2], nil
}

// validateMigrations checks that the (already version-sorted) migrations have no
// duplicate versions, returning errDuplicateMigration on the first collision.
func validateMigrations(migrations []migration) error {
	for i := 1; i < len(migrations); i++ {
		if migrations[i].version == migrations[i-1].version {
			return fmt.Errorf("%w: version %d used by both %q and %q",
				errDuplicateMigration, migrations[i].version,
				migrations[i-1].filename, migrations[i].filename)
		}
	}
	return nil
}

// ensureSchemaMigrationsTable creates the schema_migrations bookkeeping table if
// it does not already exist.
func ensureSchemaMigrationsTable(ctx context.Context, q querier) error {
	if _, err := q.Exec(ctx, schemaMigrationsDDL); err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}
	return nil
}

// appliedVersions returns the set of migration versions already recorded in
// schema_migrations.
func appliedVersions(ctx context.Context, q querier) (map[int64]bool, error) {
	rows, err := q.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("querying applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int64]bool)
	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scanning applied migration version: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating applied migrations: %w", err)
	}
	return applied, nil
}

// applyPending applies each migration whose version is not already in applied,
// in slice order, returning the filenames applied and logging each file's wall
// time as it commits. It stops and returns the migrations applied so far
// together with the error on the first failure.
func applyPending(
	ctx context.Context,
	q querier,
	migrations []migration,
	applied map[int64]bool,
) ([]string, error) {
	var done []string
	for _, mig := range migrations {
		if applied[mig.version] {
			continue
		}
		started := time.Now()
		if err := applyOne(ctx, q, mig); err != nil {
			return done, err
		}
		// The per-file wall time is what an upgrade's outage window is measured
		// from (docs/MIGRATION_SAFETY.md): each file holds its locks until it
		// commits, so the slowest file is the longest a request can wait.
		slog.InfoContext(ctx, "migration applied", "file", mig.filename,
			"duration_ms", time.Since(started).Milliseconds())
		done = append(done, mig.filename)
	}
	return done, nil
}

// applyOne executes a single migration and records it in schema_migrations
// inside one transaction, so a failure leaves neither the schema change nor the
// bookkeeping row behind. The deferred rollback is a no-op after a successful
// commit.
func applyOne(ctx context.Context, q querier, mig migration) (err error) {
	tx, err := q.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning tx for migration %q: %w", mig.filename, err)
	}
	defer func() {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("rolling back migration %q: %w", mig.filename, rbErr))
		}
	}()

	if _, execErr := tx.Exec(ctx, mig.sql); execErr != nil {
		return fmt.Errorf("applying migration %q: %w", mig.filename, execErr)
	}
	if _, execErr := tx.Exec(ctx,
		"INSERT INTO schema_migrations (version, name) VALUES ($1, $2)",
		mig.version, mig.name); execErr != nil {
		return fmt.Errorf("recording migration %q: %w", mig.filename, execErr)
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		return fmt.Errorf("committing migration %q: %w", mig.filename, commitErr)
	}
	return nil
}
