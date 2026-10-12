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

// savedSearchColumns is the column list every query returns, in the order
// scanSavedSearch reads it.
const savedSearchColumns = "uid, project_id, owner_uid, name, params, created_at, updated_at"

// scanSavedSearch reads one saved_searches row into a SavedSearch. It maps
// pgx.ErrNoRows to ErrNotFound so callers can branch on the sentinel.
func scanSavedSearch(r row) (SavedSearch, error) {
	var s SavedSearch
	err := r.Scan(&s.UID, &s.ProjectUID, &s.OwnerUID, &s.Name, &s.Params, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SavedSearch{}, ErrNotFound
	}
	if err != nil {
		return SavedSearch{}, fmt.Errorf("savedsearch: scanning row: %w", err)
	}
	return s, nil
}

// insertSavedSearchSQL inserts a saved search into a project and returns the
// persisted row, including the database-assigned timestamps. project_id is always
// written explicitly: the column's transitional default is never relied on.
const insertSavedSearchSQL = `
INSERT INTO saved_searches (uid, project_id, owner_uid, name, params)
VALUES ($1, $2, $3, $4, $5)
RETURNING ` + savedSearchColumns

// Create inserts a saved search owned by ownerUID in projectUID with the given
// name and opaque params, returning the persisted record with its generated UID
// and timestamps. params must be valid JSON; a nil params is stored as the empty
// JSON object. An empty projectUID is ErrNoProject.
func (s *Store) Create(
	ctx context.Context, projectUID, ownerUID, name string, params json.RawMessage,
) (SavedSearch, error) {
	if err := requireProject(projectUID); err != nil {
		return SavedSearch{}, err
	}
	uid, err := newSavedSearchUID()
	if err != nil {
		return SavedSearch{}, err
	}
	saved, err := scanSavedSearch(s.pool.QueryRow(ctx, insertSavedSearchSQL,
		uid, projectUID, ownerUID, name, defaultParams(params)))
	if err != nil {
		return SavedSearch{}, err
	}
	return saved, nil
}

// listSavedSearchesSQL returns a user's saved searches in one project, most
// recently created first then by uid for a stable tie-break.
const listSavedSearchesSQL = `
SELECT ` + savedSearchColumns + `
FROM saved_searches
WHERE ` + ownerProjectClause + `
ORDER BY created_at DESC, uid`

// List returns every saved search ownerUID owns in projectUID, newest first. A
// user with no saved searches there yields an empty (non-nil) slice and a nil
// error; the same user's searches in another project are never listed. An empty
// projectUID is ErrNoProject.
func (s *Store) List(ctx context.Context, projectUID, ownerUID string) ([]SavedSearch, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, listSavedSearchesSQL, projectUID, ownerUID)
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

// getSavedSearchSQL fetches one saved search by its UID, only when it belongs
// to the owner inside the project.
const getSavedSearchSQL = `
SELECT ` + savedSearchColumns + `
FROM saved_searches
WHERE ` + ownerProjectClause + ` AND uid = $3`

// Get returns the saved search identified by uid when ownerUID owns it in
// projectUID. A missing uid, another owner's search and the owner's own search in
// another project are all the same ErrNotFound. An empty projectUID is
// ErrNoProject.
func (s *Store) Get(ctx context.Context, projectUID, ownerUID, uid string) (SavedSearch, error) {
	if err := requireProject(projectUID); err != nil {
		return SavedSearch{}, err
	}
	return scanSavedSearch(s.pool.QueryRow(ctx, getSavedSearchSQL, projectUID, ownerUID, uid))
}

// updateSavedSearchSQL rewrites a saved search's name and params and stamps
// updated_at, returning the updated row — only when it belongs to the owner
// inside the project.
const updateSavedSearchSQL = `
UPDATE saved_searches
SET name = $4, params = $5, updated_at = now()
WHERE ` + ownerProjectClause + ` AND uid = $3
RETURNING ` + savedSearchColumns

// Update rewrites the name and params of the saved search identified by uid and
// returns the updated record, or ErrNotFound unless ownerUID owns it in
// projectUID (nothing is written then). A nil params is stored as the empty JSON
// object. An empty projectUID is ErrNoProject.
func (s *Store) Update(
	ctx context.Context, projectUID, ownerUID, uid, name string, params json.RawMessage,
) (SavedSearch, error) {
	if err := requireProject(projectUID); err != nil {
		return SavedSearch{}, err
	}
	return scanSavedSearch(s.pool.QueryRow(ctx, updateSavedSearchSQL,
		projectUID, ownerUID, uid, name, defaultParams(params)))
}

// deleteSavedSearchSQL removes one saved search, only when it belongs to the
// owner inside the project.
const deleteSavedSearchSQL = `DELETE FROM saved_searches WHERE ` + ownerProjectClause + ` AND uid = $3`

// Delete removes the saved search identified by uid, returning ErrNotFound
// unless ownerUID owns it in projectUID (nothing is deleted then). An empty
// projectUID is ErrNoProject.
func (s *Store) Delete(ctx context.Context, projectUID, ownerUID, uid string) error {
	if err := requireProject(projectUID); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, deleteSavedSearchSQL, projectUID, ownerUID, uid)
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
