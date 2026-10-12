// Package savedsearch is the database access layer for per-user saved searches
// ("smart albums"): named, owner-private filter/search definitions a user can
// re-open later. It mirrors the per-user ownership model of user favorites — a
// saved search belongs to exactly one owner inside exactly one project, and only
// that owner, inside that project, may see or modify it. The store enforces both:
// every method takes the project and the owner (see store_project.go).
package savedsearch

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	// ErrNotFound is returned when a saved search does not exist — or belongs to
	// another owner, or to another project: all three are the same answer.
	ErrNotFound = errors.New("saved search not found")
	// ErrNoProject is returned by a Store method called without a project — a
	// programming error, never "anywhere in the instance".
	ErrNoProject = errors.New("savedsearch: no project given")
)

// SavedSearch is a named, owner-private saved view/search definition. Params
// holds opaque saved state (filters, sort, search query, mode) as raw JSON so the
// store stays agnostic to the frontend's view shape; it is persisted verbatim.
type SavedSearch struct {
	// UID is the primary key, of the form "ss" followed by random base32 chars.
	UID string `json:"uid"`
	// ProjectUID is the project the saved search belongs to.
	ProjectUID string `json:"project_uid"`
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
