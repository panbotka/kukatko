package photosorter

import (
	"context"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
)

// DefaultPageSize is the listing page size used when a caller passes a
// non-positive Limit.
const DefaultPageSize = 500

// Config holds the connection details for the read-only photo-sorter reader.
type Config struct {
	// DSN is the read-only PostgreSQL connection string for the photo-sorter
	// database. It is required.
	DSN string
	// Schema, when non-empty, is set as the connection search_path so every query
	// resolves against it. It is empty (public) in production and set by the
	// integration test to a fixture schema living alongside Kukátko's own tables.
	Schema string
	// MaxConns caps the reader's connection pool; a non-positive value leaves the
	// pgx default in place.
	MaxConns int
}

// Reader is a read-only client for a photo-sorter database. It owns its own pgx
// pool (separate from Kukátko's) and must be closed by the caller.
type Reader struct {
	pool   *pgxpool.Pool
	schema string
}

// New opens a connection pool against cfg.DSN, registering the pgvector types and
// (when cfg.Schema is set) the search_path on every connection, and verifies
// connectivity with a Ping. The caller owns the returned Reader and must Close
// it. It returns ErrInvalidDSN if the DSN cannot be parsed, or a wrapped error if
// the pool cannot be created or the initial Ping fails.
func New(ctx context.Context, cfg Config) (*Reader, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidDSN, err)
	}
	if cfg.MaxConns > 0 && cfg.MaxConns <= math.MaxInt32 {
		poolCfg.MaxConns = int32(cfg.MaxConns)
	}
	schema := cfg.Schema
	poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return afterConnect(ctx, conn, schema)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("photosorter: creating connection pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("photosorter: pinging database: %w", err)
	}
	return &Reader{pool: pool, schema: schema}, nil
}

// afterConnect registers the pgvector types and, when schema is non-empty, sets
// the connection's search_path so unqualified table names resolve against it.
func afterConnect(ctx context.Context, conn *pgx.Conn, schema string) error {
	if err := pgxvec.RegisterTypes(ctx, conn); err != nil {
		return fmt.Errorf("photosorter: registering pgvector types: %w", err)
	}
	if schema != "" {
		stmt := "SET search_path TO " + pgx.Identifier{schema}.Sanitize()
		if _, err := conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("photosorter: setting search_path: %w", err)
		}
	}
	return nil
}

// Close releases every connection held by the reader's pool. It is safe to call
// once.
func (r *Reader) Close() {
	r.pool.Close()
}

// limit returns p clamped to a positive page size, defaulting to DefaultPageSize.
func pageLimit(limit int) int {
	if limit <= 0 {
		return DefaultPageSize
	}
	return limit
}
