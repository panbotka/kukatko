package searchhistory

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store reads and writes per-user search history over the shared pgx pool. It
// owns no connection; it borrows the pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// recordQuerySQL remembers one query. The (user_uid, query) primary key makes it
// an upsert: running the same search again moves its position to the front of the
// history instead of appending a duplicate.
const recordQuerySQL = `
INSERT INTO search_history (user_uid, query)
VALUES ($1, $2)
ON CONFLICT (user_uid, query) DO UPDATE SET searched_at = now()`

// pruneHistorySQL drops everything past the newest $2 entries of one user's
// history, which is what keeps the history a fixed-size ring rather than a log.
//
// The rows to keep are identified by query rather than by timestamp, because
// (user_uid, query) is the primary key and therefore unique per user, while two
// searches recorded in the same transaction share a searched_at. `ORDER BY
// searched_at DESC, query` is the same total order the listing uses, so the entry
// that falls off the end is exactly the one the client would have shown last.
const pruneHistorySQL = `
DELETE FROM search_history
WHERE user_uid = $1
  AND query NOT IN (
    SELECT query
    FROM search_history
    WHERE user_uid = $1
    ORDER BY searched_at DESC, query
    LIMIT $2
  )`

// Record remembers that userUID ran query, moving it to the front of their
// history and pruning the history back to [MaxEntries] entries.
//
// The query is [Normalize]d first; a query that holds nothing but whitespace
// returns [ErrEmptyQuery] and writes nothing. The upsert and the prune run in one
// transaction, so a concurrent read never observes a history that has grown past
// the cap.
func (s *Store) Record(ctx context.Context, userUID, query string) error {
	normalized := Normalize(query)
	if normalized == "" {
		return ErrEmptyQuery
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("searchhistory: beginning transaction: %w", err)
	}
	defer func() {
		// Rolling back an already-committed transaction is a no-op, so this is safe
		// on the success path too.
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, recordQuerySQL, userUID, normalized); err != nil {
		return fmt.Errorf("searchhistory: recording query for %s: %w", userUID, err)
	}
	if _, err := tx.Exec(ctx, pruneHistorySQL, userUID, MaxEntries); err != nil {
		return fmt.Errorf("searchhistory: pruning history of %s: %w", userUID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("searchhistory: committing history of %s: %w", userUID, err)
	}
	return nil
}

// listHistorySQL returns one user's recent queries, newest first, tie-broken by
// query so the order is total and the same as the prune's.
const listHistorySQL = `
SELECT query, searched_at
FROM search_history
WHERE user_uid = $1
ORDER BY searched_at DESC, query
LIMIT $2`

// List returns userUID's recent searches, most recent first, at most
// [MaxEntries] of them. A user who has searched nothing yields an empty
// (non-nil) slice and a nil error.
func (s *Store) List(ctx context.Context, userUID string) ([]Entry, error) {
	rows, err := s.pool.Query(ctx, listHistorySQL, userUID, MaxEntries)
	if err != nil {
		return nil, fmt.Errorf("searchhistory: listing history of %s: %w", userUID, err)
	}
	defer rows.Close()

	out := make([]Entry, 0, MaxEntries)
	for rows.Next() {
		var entry Entry
		if err := rows.Scan(&entry.Query, &entry.SearchedAt); err != nil {
			return nil, fmt.Errorf("searchhistory: scanning history row: %w", err)
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("searchhistory: iterating history of %s: %w", userUID, err)
	}
	return out, nil
}

// Clear forgets userUID's whole search history. It is idempotent: clearing an
// already-empty history succeeds.
func (s *Store) Clear(ctx context.Context, userUID string) error {
	if _, err := s.pool.Exec(ctx, "DELETE FROM search_history WHERE user_uid = $1", userUID); err != nil {
		return fmt.Errorf("searchhistory: clearing history of %s: %w", userUID, err)
	}
	return nil
}
