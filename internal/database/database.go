// Package database provides Kukátko's PostgreSQL access layer: a pgx connection
// pool with pgvector type registration plus an embedded SQL migration runner
// that auto-applies schema changes on startup.
//
// Embeddings are stored directly in PostgreSQL as pgvector halfvec columns, so
// every pooled connection registers the vector/halfvec/sparsevec types on
// connect. That makes the `vector` extension a precondition of the pool rather
// than of the schema: New installs it over a plain connection before the pool is
// built (see ensureVectorExtension), so a brand-new database comes up. The
// `unaccent` extension has no such constraint and is installed by migration 0001
// along with a second, idempotent CREATE EXTENSION for `vector`.
package database

import (
	"context"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxvec "github.com/pgvector/pgvector-go/pgx"

	"github.com/panbotka/kukatko/internal/config"
)

// DB wraps a pgxpool.Pool and is the entry point for all database access.
type DB struct {
	pool *pgxpool.Pool
}

// vectorExtension is the PostgreSQL extension providing the vector/halfvec
// types. Every pooled connection registers those types on connect, so it is the
// one extension that has to exist before the pool does — see ensureVectorExtension.
const vectorExtension = "vector"

// sessionTimeZone is the time zone every pooled connection runs in. Calendar
// arithmetic in SQL (date_part('year', taken_at), make_timestamptz(…)) resolves
// in the session zone, while the Go side builds its date boundaries in UTC — so
// the two only agree, and a photo near midnight on New Year's Eve only lands in
// one year, when the session is pinned to UTC as well. It is deliberately not
// configurable: one reference frame everywhere is the point.
const sessionTimeZone = "UTC"

// New opens a pgx connection pool from cfg.URL, applies the configured pool-size
// limits, pins every session to UTC, registers the pgvector types (vector,
// halfvec, sparsevec) on every connection, and verifies connectivity with a
// Ping. The caller owns the returned DB and must Close it. It returns a wrapped
// error if the DSN is invalid, the pool cannot be created, or the initial Ping
// fails.
func New(ctx context.Context, cfg config.DatabaseConfig) (*DB, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parsing database url: %w", err)
	}
	applyPoolLimits(poolCfg, cfg)
	pinSessionTimeZone(poolCfg)

	// This has to happen before the pool exists, not in a migration. The pool's
	// AfterConnect registers the pgvector types, and on a database that has never
	// been migrated there are none to register — so every connection fails with
	// "vector type not found in the database" and the migration that would have
	// created the extension can never run over the pool. A fresh install would
	// deadlock on itself; one plain connection breaks the cycle.
	if err := ensureVectorExtension(ctx, poolCfg.ConnConfig); err != nil {
		return nil, err
	}
	poolCfg.AfterConnect = registerVectorTypes

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("creating connection pool: %w", err)
	}

	db := &DB{pool: pool}
	if err := db.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return db, nil
}

// ensureVectorExtension installs the pgvector extension when the database does
// not have it yet, over a single short-lived connection opened from connCfg —
// which must be a config WITHOUT the pgvector AfterConnect hook, since that hook
// is exactly what cannot succeed yet. Migration 0001 creates the extension too;
// this is the copy that runs early enough for the pool to come up at all, and
// the two are idempotent with respect to each other.
//
// A database that already has the extension issues no CREATE EXTENSION at all,
// only a catalog read, so an instance whose role may not create extensions (a
// managed Postgres, a pre-provisioned shared server) is unaffected. It returns a
// wrapped error if the connection cannot be opened, the catalog cannot be read,
// or the extension is missing and cannot be created.
func ensureVectorExtension(ctx context.Context, connCfg *pgx.ConnConfig) error {
	conn, err := pgx.ConnectConfig(ctx, connCfg.Copy())
	if err != nil {
		return fmt.Errorf("connecting to check the %s extension: %w", vectorExtension, err)
	}
	// The close must outlive a cancelled ctx, otherwise a shutdown mid-startup
	// leaks the connection until the server times it out.
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()

	return ensureExtension(ctx, conn, vectorExtension)
}

// ensureExtension installs the named PostgreSQL extension into the connected
// database unless pg_extension already lists it, and reports nothing else — an
// already-installed extension is a no-op. CREATE EXTENSION takes an identifier
// rather than a bind parameter, so name is quoted as one; callers pass a
// constant. It returns a wrapped error naming the extension if the catalog read
// or the creation fails, the latter typically meaning the server does not ship
// the extension or the role may not create it.
func ensureExtension(ctx context.Context, conn *pgx.Conn, name string) error {
	const query = "SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = $1)"

	var installed bool
	if err := conn.QueryRow(ctx, query, name).Scan(&installed); err != nil {
		return fmt.Errorf("checking whether the %s extension is installed: %w", name, err)
	}
	if installed {
		return nil
	}

	if _, err := conn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS "+pgx.Identifier{name}.Sanitize()); err != nil {
		return fmt.Errorf("creating the %s extension (the server must ship it and the role "+
			"must be allowed to CREATE EXTENSION): %w", name, err)
	}
	return nil
}

// registerVectorTypes registers the pgvector types on a freshly established
// connection so vector/halfvec values can be scanned and bound directly. It is
// used as the pool's AfterConnect hook.
func registerVectorTypes(ctx context.Context, conn *pgx.Conn) error {
	if err := pgxvec.RegisterTypes(ctx, conn); err != nil {
		return fmt.Errorf("registering pgvector types: %w", err)
	}
	return nil
}

// pinSessionTimeZone makes every connection of the pool start in
// sessionTimeZone by sending it as a startup runtime parameter, so no query has
// to guess which zone the server or the DSN happens to default to. It overrides
// a timezone the DSN asks for: the catalogue's date arithmetic is only
// self-consistent in one zone, so that choice is not the deployment's to make.
func pinSessionTimeZone(poolCfg *pgxpool.Config) {
	if poolCfg.ConnConfig.RuntimeParams == nil {
		poolCfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	poolCfg.ConnConfig.RuntimeParams["timezone"] = sessionTimeZone
}

// applyPoolLimits maps Kukátko's connection-pool configuration onto the pgx pool
// config: MaxOpenConns becomes the pool's MaxConns and MaxIdleConns its
// MinConns. Non-positive or out-of-range values leave the pgx defaults intact.
func applyPoolLimits(poolCfg *pgxpool.Config, cfg config.DatabaseConfig) {
	if n := cfg.MaxOpenConns; n > 0 && n <= math.MaxInt32 {
		poolCfg.MaxConns = int32(n)
	}
	if n := cfg.MaxIdleConns; n > 0 && n <= math.MaxInt32 {
		poolCfg.MinConns = int32(n)
	}
}

// Ping verifies that a connection to the database can be acquired and is
// responsive, returning a wrapped error if the database is unreachable.
func (db *DB) Ping(ctx context.Context) error {
	if err := db.pool.Ping(ctx); err != nil {
		return fmt.Errorf("pinging database: %w", err)
	}
	return nil
}

// Close releases every connection held by the pool. It blocks until all
// acquired connections have been returned and is safe to call once.
func (db *DB) Close() {
	db.pool.Close()
}

// Pool returns the underlying pgx connection pool for callers that need direct
// query access. The pool stays owned by the DB; callers must not Close it.
func (db *DB) Pool() *pgxpool.Pool {
	return db.pool
}

// Migrate applies all pending embedded migrations against this database and
// returns the filenames applied during the call. See the package-level Migrate
// function for the full contract.
func (db *DB) Migrate(ctx context.Context) ([]string, error) {
	return Migrate(ctx, db.pool)
}
