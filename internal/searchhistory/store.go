package searchhistory

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store reads and writes per-(user, project) search history over the shared pgx
// pool. It
// owns no connection; it borrows the pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// recordQuerySQL remembers one query in one project. The (user_uid, project_id,
// query) primary key makes it an upsert: running the same search again moves its
// position to the front of the history instead of appending a duplicate.
// project_id is always written explicitly: the column's transitional default
// (migration 0102) is never relied on.
const recordQuerySQL = `
INSERT INTO search_history (user_uid, project_id, query)
VALUES ($1, $2, $3)
ON CONFLICT (user_uid, project_id, query) DO UPDATE SET searched_at = now()`

// pruneHistorySQL drops everything past the newest $3 entries of one user's
// history in one project, which is what keeps each (user, project) history a
// fixed-size ring rather than a log. Both the outer DELETE and the subquery name
// the project, so trimming one project's ring never touches another's.
//
// The rows to keep are identified by query rather than by timestamp, because
// (user_uid, project_id, query) is the primary key and therefore unique per
// (user, project), while two searches recorded in the same transaction share a
// searched_at. `ORDER BY searched_at DESC, query` is the same total order the
// listing uses, so the entry that falls off the end is exactly the one the client
// would have shown last.
const pruneHistorySQL = `
DELETE FROM search_history
WHERE ` + userProjectClause + `
  AND query NOT IN (
    SELECT query
    FROM search_history
    WHERE ` + userProjectClause + `
    ORDER BY searched_at DESC, query
    LIMIT $3
  )`

// Record remembers that userUID ran query in projectUID, moving it to the front
// of their history there and pruning that history back to [MaxEntries] entries.
// The user's history in any other project is left alone.
//
// An empty projectUID is [ErrNoProject]. The query is [Normalize]d first; a query
// that holds nothing but whitespace returns [ErrEmptyQuery] and writes nothing.
// The upsert and the prune run in one transaction, so a concurrent read never
// observes a history that has grown past the cap.
func (s *Store) Record(ctx context.Context, projectUID, userUID, query string) error {
	if err := requireProject(projectUID); err != nil {
		return err
	}
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

	if _, err := tx.Exec(ctx, recordQuerySQL, userUID, projectUID, normalized); err != nil {
		return fmt.Errorf("searchhistory: recording query for %s: %w", userUID, err)
	}
	if _, err := tx.Exec(ctx, pruneHistorySQL, userUID, projectUID, MaxEntries); err != nil {
		return fmt.Errorf("searchhistory: pruning history of %s: %w", userUID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("searchhistory: committing history of %s: %w", userUID, err)
	}
	return nil
}

// listHistorySQL returns one user's recent queries in one project, newest
// first, tie-broken by query so the order is total and the same as the prune's.
const listHistorySQL = `
SELECT query, searched_at
FROM search_history
WHERE ` + userProjectClause + `
ORDER BY searched_at DESC, query
LIMIT $3`

// List returns userUID's recent searches in projectUID, most recent first, at
// most [MaxEntries] of them. A user who has searched nothing there yields an
// empty (non-nil) slice and a nil error; what they searched in another project
// is never listed. An empty projectUID is [ErrNoProject].
func (s *Store) List(ctx context.Context, projectUID, userUID string) ([]Entry, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, listHistorySQL, userUID, projectUID, MaxEntries)
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

// clearHistorySQL forgets one user's whole history in one project.
const clearHistorySQL = `DELETE FROM search_history WHERE ` + userProjectClause

// Clear forgets userUID's whole search history in projectUID; the history in
// every other project is kept. It is idempotent: clearing an already-empty
// history succeeds. An empty projectUID is [ErrNoProject].
func (s *Store) Clear(ctx context.Context, projectUID, userUID string) error {
	if err := requireProject(projectUID); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, clearHistorySQL, userUID, projectUID); err != nil {
		return fmt.Errorf("searchhistory: clearing history of %s: %w", userUID, err)
	}
	return nil
}
