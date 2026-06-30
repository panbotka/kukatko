// Package savedsearch is the database access layer for per-user saved searches
// ("smart albums"): named, owner-private filter/search definitions a user can
// re-open later. It mirrors the per-user ownership model of user favorites — a
// saved search belongs to exactly one owner, and only that owner may see or
// modify it. Ownership scoping is enforced by the HTTP layer above this store.
package savedsearch

import (
	"encoding/json"
	"errors"
	"time"
)

// ErrNotFound is returned when a saved search does not exist.
var ErrNotFound = errors.New("saved search not found")

// SavedSearch is a named, owner-private saved view/search definition. Params
// holds opaque saved state (filters, sort, search query, mode) as raw JSON so the
// store stays agnostic to the frontend's view shape; it is persisted verbatim.
type SavedSearch struct {
	// UID is the primary key, of the form "ss" followed by random base32 chars.
	UID string `json:"uid"`
	// OwnerUID is the UID of the user who owns the saved search.
	OwnerUID string `json:"owner_uid"`
	// Name is the user-facing label for the saved search.
	Name string `json:"name"`
	// Params is the opaque saved view/search state as raw JSON.
	Params json.RawMessage `json:"params"`
	// CreatedAt is when the saved search was first created.
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is when the saved search was last modified.
	UpdatedAt time.Time `json:"updated_at"`
}
