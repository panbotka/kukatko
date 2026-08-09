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
	"time"

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

// TakenAt is a capture date set by a bulk operation, together with the grain it
// was stated at. At is always the **first instant of the stated period in UTC**
// (1 January for a year, the 1st for a month), because taken_at is the single
// anchor the timeline, the period filter, the year facets and the query
// language's year: filter all read — a box of scans dated "1974" has to sort and
// filter into 1974 like any other photo. Precision is what keeps the anchor from
// being read back as a day nobody claimed; see photos.TakenAtPrecision*.
//
// It changes catalogue metadata only. The originals and their EXIF are never
// touched — the app does not write into the files it was given — and the
// caller's usual sidecar rewrite carries the new date out to storage.
type TakenAt struct {
	At        time.Time `json:"at"`
	Precision string    `json:"precision"`
}

// Operations is the resolved set of changes to apply to every target photo. Each
// field is independently optional: nil slices/pointers and a false ClearLocation
// mean "leave unchanged". A non-nil Title/Description pointer sets that column
// (the empty string clears it); ClearLocation wipes lat/lng; Archive true
// archives and false unarchives; Hide true keeps the photos out of the library
// firehose and false brings them back; Favorite toggles the acting user's
// favorite; Rating sets the acting user's star rating (0–5) and Flag the
// pick/reject flag.
type Operations struct {
	AddAlbums    []string
	RemoveAlbums []string
	AddLabels    []string
	RemoveLabels []string
	Title        *string
	Description  *string
	// TakenAt sets the capture date of every target photo at a stated grain — the
	// one repair for a shelf of scans the scanner dated to the day it was switched
	// on. See TakenAt for why a coarse grain still stores a concrete instant.
	TakenAt       *TakenAt
	Location      *Location
	ClearLocation bool
	Archive       *bool
	// Hide sets photos.hidden_from_library. Hiding is the operation this whole
	// batch path exists for on the feature's own terms: the real use is fifty
	// document scans at once, not one.
	Hide     *bool
	Favorite *bool
	Rating   *int
	Flag     *string
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
	o.addTakenAtSummary(summary)
	return summary
}

// addTakenAtSummary records a set-taken-date operation in the audit details, as
// the instant that was stored and the grain it was stated at. Both are needed to
// read the entry back years later: the instant alone would leave "1 January
// 1974" looking like a date somebody typed.
func (o Operations) addTakenAtSummary(summary map[string]any) {
	if o.TakenAt == nil {
		return
	}
	summary["taken_at"] = map[string]any{
		"at":        o.TakenAt.At.UTC().Format(time.RFC3339),
		"precision": o.TakenAt.Precision,
	}
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
	if o.Hide != nil {
		summary["hide"] = *o.Hide
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
