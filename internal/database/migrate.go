package database

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
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

// Migrate applies every embedded migration not yet recorded in
// schema_migrations, in ascending version order, each within its own
// transaction. It is idempotent: already-applied migrations are skipped. The
// filenames applied during this call are returned (empty when the database is
// already up to date). Errors from loading, recording, or executing a migration
// are returned wrapped.
func Migrate(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	migrations, err := loadMigrations(migrationFS, migrationsDir)
	if err != nil {
		return nil, err
	}
	if err := ensureSchemaMigrationsTable(ctx, pool); err != nil {
		return nil, err
	}
	applied, err := appliedVersions(ctx, pool)
	if err != nil {
		return nil, err
	}
	return applyPending(ctx, pool, migrations, applied)
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
func ensureSchemaMigrationsTable(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, schemaMigrationsDDL); err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}
	return nil
}

// appliedVersions returns the set of migration versions already recorded in
// schema_migrations.
func appliedVersions(ctx context.Context, pool *pgxpool.Pool) (map[int64]bool, error) {
	rows, err := pool.Query(ctx, "SELECT version FROM schema_migrations")
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
// in slice order, returning the filenames applied. It stops and returns the
// migrations applied so far together with the error on the first failure.
func applyPending(
	ctx context.Context,
	pool *pgxpool.Pool,
	migrations []migration,
	applied map[int64]bool,
) ([]string, error) {
	var done []string
	for _, mig := range migrations {
		if applied[mig.version] {
			continue
		}
		if err := applyOne(ctx, pool, mig); err != nil {
			return done, err
		}
		done = append(done, mig.filename)
	}
	return done, nil
}

// applyOne executes a single migration and records it in schema_migrations
// inside one transaction, so a failure leaves neither the schema change nor the
// bookkeeping row behind. The deferred rollback is a no-op after a successful
// commit.
func applyOne(ctx context.Context, pool *pgxpool.Pool, mig migration) (err error) {
	tx, err := pool.Begin(ctx)
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
