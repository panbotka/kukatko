// Package bulk applies metadata changes to many photos in a single transaction.
// One request lists the target photo UIDs and an operation set (album/label
// membership, description/caption, location, archive state and the caller's
// per-user favorite). The whole batch runs in one transaction together
// with a durable audit_log entry, so it commits or rolls back atomically. Each
// photo is reported individually (updated/skipped/error): a missing photo is
// recorded as an error without aborting the valid ones, while a genuine database
// failure rolls the whole batch back. See ARCHITECTURE.md §1 (bulk editing).
package bulk

import (
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultMaxBatchSize caps how many photos one request may target when the
// caller supplies a non-positive limit.
const DefaultMaxBatchSize = 1000

// Per-photo result statuses returned in PhotoResult.Status.
const (
	// StatusUpdated marks a photo whose operations were applied.
	StatusUpdated = "updated"
	// StatusSkipped marks a photo skipped without change, for example a UID
	// repeated within the same request.
	StatusSkipped = "skipped"
	// StatusError marks a photo that could not be processed, for example one that
	// does not exist.
	StatusError = "error"
)

// Sentinel errors describing why a bulk request was rejected before any change.
var (
	// ErrNoPhotos indicates the request listed no photo UIDs.
	ErrNoPhotos = errors.New("bulk: no photo UIDs provided")
	// ErrNoOperations indicates the operation set was empty.
	ErrNoOperations = errors.New("bulk: no operations provided")
	// ErrBatchTooLarge indicates the photo count exceeded the configured limit.
	ErrBatchTooLarge = errors.New("bulk: batch size exceeds limit")
	// ErrAlbumNotFound indicates an add-to-album operation referenced a missing
	// album.
	ErrAlbumNotFound = errors.New("bulk: album not found")
	// ErrLabelNotFound indicates an add-label operation referenced a missing
	// label.
	ErrLabelNotFound = errors.New("bulk: label not found")
)

// Location is a geographic coordinate set by a bulk operation.
type Location struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// Operations is the resolved set of changes to apply to every target photo. Each
// field is independently optional: nil slices/pointers and a false ClearLocation
// mean "leave unchanged". A non-nil Title/Description pointer sets that column
// (the empty string clears it); ClearLocation wipes lat/lng; Archive true
// archives and false unarchives; Favorite toggles the acting user's favorite;
// Rating sets the acting user's star rating (0–5) and Flag the pick/reject flag.
type Operations struct {
	AddAlbums     []string
	RemoveAlbums  []string
	AddLabels     []string
	RemoveLabels  []string
	Title         *string
	Description   *string
	Location      *Location
	ClearLocation bool
	Archive       *bool
	Favorite      *bool
	Rating        *int
	Flag          *string
}

// PhotoResult is the outcome of one photo in a bulk request.
type PhotoResult struct {
	PhotoUID string `json:"photo_uid"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}

// Counts summarises a bulk request's per-photo outcomes.
type Counts struct {
	Total   int `json:"total"`
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
	Errored int `json:"errored"`
}

// Result is the full response of a bulk request: a per-photo breakdown plus the
// aggregate counts.
type Result struct {
	Results []PhotoResult `json:"results"`
	Counts  Counts        `json:"counts"`
}

// add appends a per-photo outcome and updates the matching aggregate count.
func (r *Result) add(uid, status, msg string) {
	r.Results = append(r.Results, PhotoResult{PhotoUID: uid, Status: status, Error: msg})
	switch status {
	case StatusUpdated:
		r.Counts.Updated++
	case StatusSkipped:
		r.Counts.Skipped++
	case StatusError:
		r.Counts.Errored++
	}
}

// Service applies bulk operations against a PostgreSQL pool, enforcing the
// per-request batch-size limit.
type Service struct {
	pool     *pgxpool.Pool
	maxBatch int
}

// NewService returns a Service backed by pool. A non-positive maxBatch falls back
// to DefaultMaxBatchSize.
func NewService(pool *pgxpool.Pool, maxBatch int) *Service {
	if maxBatch <= 0 {
		maxBatch = DefaultMaxBatchSize
	}
	return &Service{pool: pool, maxBatch: maxBatch}
}

// MaxBatch returns the configured per-request photo limit.
func (s *Service) MaxBatch() int {
	return s.maxBatch
}

// IsEmpty reports whether the operation set requests no changes at all.
func (o Operations) IsEmpty() bool {
	return len(o.Summary()) == 0
}

// Summary returns a JSON-able description of the requested operations, used for
// the audit-log details. Only operations that change something appear.
func (o Operations) Summary() map[string]any {
	summary := o.collectionSummary()
	o.addScalarSummary(summary)
	return summary
}

// collectionSummary adds the album/label slice operations to a fresh summary map.
func (o Operations) collectionSummary() map[string]any {
	summary := map[string]any{}
	if len(o.AddAlbums) > 0 {
		summary["add_albums"] = o.AddAlbums
	}
	if len(o.RemoveAlbums) > 0 {
		summary["remove_albums"] = o.RemoveAlbums
	}
	if len(o.AddLabels) > 0 {
		summary["add_labels"] = o.AddLabels
	}
	if len(o.RemoveLabels) > 0 {
		summary["remove_labels"] = o.RemoveLabels
	}
	return summary
}

// addScalarSummary adds the scalar (description, location, flags) operations to
// the given summary map.
func (o Operations) addScalarSummary(summary map[string]any) {
	if o.Title != nil {
		summary["title"] = *o.Title
	}
	if o.Description != nil {
		summary["description"] = *o.Description
	}
	if o.Location != nil {
		summary["location"] = map[string]float64{"lat": o.Location.Lat, "lng": o.Location.Lng}
	}
	if o.ClearLocation {
		summary["clear_location"] = true
	}
	if o.Archive != nil {
		summary["archive"] = *o.Archive
	}
	if o.Favorite != nil {
		summary["favorite"] = *o.Favorite
	}
	if o.Rating != nil {
		summary["rating"] = *o.Rating
	}
	if o.Flag != nil {
		summary["flag"] = *o.Flag
	}
}
