// Package database provides Kukátko's PostgreSQL access layer: a pgx connection
// pool with pgvector type registration plus an embedded SQL migration runner
// that auto-applies schema changes on startup.
//
// Embeddings are stored directly in PostgreSQL as pgvector halfvec columns, so
// every pooled connection registers the vector/halfvec/sparsevec types on
// connect. The `vector` and `unaccent` extensions are expected to be present
// (they are installed by migration 0001 and pre-provisioned on the shared
// Postgres instance).
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

// New opens a pgx connection pool from cfg.URL, applies the configured pool-size
// limits, registers the pgvector types (vector, halfvec, sparsevec) on every
// connection, and verifies connectivity with a Ping. The caller owns the
// returned DB and must Close it. It returns a wrapped error if the DSN is
// invalid, the pool cannot be created, or the initial Ping fails.
func New(ctx context.Context, cfg config.DatabaseConfig) (*DB, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parsing database url: %w", err)
	}
	applyPoolLimits(poolCfg, cfg)
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

// registerVectorTypes registers the pgvector types on a freshly established
// connection so vector/halfvec values can be scanned and bound directly. It is
// used as the pool's AfterConnect hook.
func registerVectorTypes(ctx context.Context, conn *pgx.Conn) error {
	if err := pgxvec.RegisterTypes(ctx, conn); err != nil {
		return fmt.Errorf("registering pgvector types: %w", err)
	}
	return nil
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
