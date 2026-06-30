package savedsearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the database access layer for per-user saved searches. It owns no
// connection; it borrows the shared pgx pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// row is the column scanner shared by the single-row queries (insert/get/update).
type row interface {
	Scan(dest ...any) error
}

// scanSavedSearch reads one saved_searches row into a SavedSearch. It maps
// pgx.ErrNoRows to ErrNotFound so callers can branch on the sentinel.
func scanSavedSearch(r row) (SavedSearch, error) {
	var s SavedSearch
	err := r.Scan(&s.UID, &s.OwnerUID, &s.Name, &s.Params, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SavedSearch{}, ErrNotFound
	}
	if err != nil {
		return SavedSearch{}, fmt.Errorf("savedsearch: scanning row: %w", err)
	}
	return s, nil
}

// insertSavedSearchSQL inserts a saved search and returns the persisted row,
// including the database-assigned timestamps.
const insertSavedSearchSQL = `
INSERT INTO saved_searches (uid, owner_uid, name, params)
VALUES ($1, $2, $3, $4)
RETURNING uid, owner_uid, name, params, created_at, updated_at`

// Create inserts a saved search owned by ownerUID with the given name and opaque
// params, returning the persisted record with its generated UID and timestamps.
// params must be valid JSON; a nil params is stored as the empty JSON object.
func (s *Store) Create(
	ctx context.Context, ownerUID, name string, params json.RawMessage,
) (SavedSearch, error) {
	uid, err := newSavedSearchUID()
	if err != nil {
		return SavedSearch{}, err
	}
	saved, err := scanSavedSearch(s.pool.QueryRow(ctx, insertSavedSearchSQL,
		uid, ownerUID, name, defaultParams(params)))
	if err != nil {
		return SavedSearch{}, err
	}
	return saved, nil
}

// listSavedSearchesSQL returns a user's saved searches, most recently created
// first then by uid for a stable tie-break.
const listSavedSearchesSQL = `
SELECT uid, owner_uid, name, params, created_at, updated_at
FROM saved_searches
WHERE owner_uid = $1
ORDER BY created_at DESC, uid`

// List returns every saved search owned by ownerUID, newest first. A user with no
// saved searches yields an empty (non-nil) slice and a nil error.
func (s *Store) List(ctx context.Context, ownerUID string) ([]SavedSearch, error) {
	rows, err := s.pool.Query(ctx, listSavedSearchesSQL, ownerUID)
	if err != nil {
		return nil, fmt.Errorf("savedsearch: listing for owner %s: %w", ownerUID, err)
	}
	defer rows.Close()

	out := make([]SavedSearch, 0)
	for rows.Next() {
		saved, scanErr := scanSavedSearch(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, saved)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("savedsearch: iterating for owner %s: %w", ownerUID, err)
	}
	return out, nil
}

// getSavedSearchSQL fetches one saved search by its UID.
const getSavedSearchSQL = `
SELECT uid, owner_uid, name, params, created_at, updated_at
FROM saved_searches
WHERE uid = $1`

// Get returns the saved search identified by uid, or ErrNotFound if no such row
// exists. Ownership is not checked here; the caller scopes access to the owner.
func (s *Store) Get(ctx context.Context, uid string) (SavedSearch, error) {
	return scanSavedSearch(s.pool.QueryRow(ctx, getSavedSearchSQL, uid))
}

// updateSavedSearchSQL rewrites a saved search's name and params and stamps
// updated_at, returning the updated row.
const updateSavedSearchSQL = `
UPDATE saved_searches
SET name = $2, params = $3, updated_at = now()
WHERE uid = $1
RETURNING uid, owner_uid, name, params, created_at, updated_at`

// Update rewrites the name and params of the saved search identified by uid and
// returns the updated record, or ErrNotFound if no such row exists. A nil params
// is stored as the empty JSON object.
func (s *Store) Update(
	ctx context.Context, uid, name string, params json.RawMessage,
) (SavedSearch, error) {
	return scanSavedSearch(s.pool.QueryRow(ctx, updateSavedSearchSQL,
		uid, name, defaultParams(params)))
}

// Delete removes the saved search identified by uid, returning ErrNotFound if no
// such row exists.
func (s *Store) Delete(ctx context.Context, uid string) error {
	tag, err := s.pool.Exec(ctx, "DELETE FROM saved_searches WHERE uid = $1", uid)
	if err != nil {
		return fmt.Errorf("savedsearch: deleting %s: %w", uid, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// defaultParams returns params unchanged, or the empty JSON object when params is
// nil/empty, so the NOT NULL params column always receives well-formed JSON.
func defaultParams(params json.RawMessage) json.RawMessage {
	if len(params) == 0 {
		return json.RawMessage("{}")
	}
	return params
}
