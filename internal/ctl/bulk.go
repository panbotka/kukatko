package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// Sentinel errors for the bulk command inputs, checked client-side so an obvious
// mistake costs no round trip and no transaction.
var (
	// ErrNoOperations indicates a bulk request that would change nothing.
	ErrNoOperations = errors.New("ctl: bulk needs at least one operation")
	// ErrConflictingOperations indicates a mutually exclusive set/clear pair.
	ErrConflictingOperations = errors.New("ctl: conflicting bulk operations")
	// ErrInvalidLocation indicates a --location that is not a "lat,lng" pair inside
	// the valid coordinate ranges.
	ErrInvalidLocation = errors.New(`ctl: location must be "lat,lng" within [-90,90] and [-180,180]`)
)

// Coordinate bounds for a set-location operation, mirroring the API's own.
const (
	minLat = -90.0
	maxLat = 90.0
	minLng = -180.0
	maxLng = 180.0
)

// BulkLocation is the lat/lng pair of a set-location operation.
type BulkLocation struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// BulkOperations is the wire form of a bulk operation set, matching the API's
// body field for field — POST /photos/bulk rejects unknown fields, so the tags
// are not free to drift.
//
// Set and clear are distinct keys rather than presence-versus-null, so the
// payload is unambiguous: a nil SetCaption leaves the caption alone, while
// ClearCaption empties it. Every field is omitted from the wire at its zero
// value, so the request carries only what the operator actually asked for.
type BulkOperations struct {
	AddAlbums        []string      `json:"add_to_albums,omitempty"`
	RemoveAlbums     []string      `json:"remove_from_albums,omitempty"`
	AddLabels        []string      `json:"add_labels,omitempty"`
	RemoveLabels     []string      `json:"remove_labels,omitempty"`
	SetCaption       *string       `json:"set_caption,omitempty"`
	ClearCaption     bool          `json:"clear_caption,omitempty"`
	SetDescription   *string       `json:"set_description,omitempty"`
	ClearDescription bool          `json:"clear_description,omitempty"`
	SetLocation      *BulkLocation `json:"set_location,omitempty"`
	ClearLocation    bool          `json:"clear_location,omitempty"`
	SetPrivate       *bool         `json:"set_private,omitempty"`
	Archive          bool          `json:"archive,omitempty"`
	Unarchive        bool          `json:"unarchive,omitempty"`
	SetFavorite      *bool         `json:"set_favorite,omitempty"`
	SetRating        *int          `json:"set_rating,omitempty"`
	SetFlag          *string       `json:"set_flag,omitempty"`
}

// IsEmpty reports whether the operation set would change nothing, so the caller
// can refuse the request before opening a server-side transaction over it.
func (o BulkOperations) IsEmpty() bool {
	for _, list := range [][]string{o.AddAlbums, o.RemoveAlbums, o.AddLabels, o.RemoveLabels} {
		if len(list) > 0 {
			return false
		}
	}
	requested := []bool{
		o.ClearCaption, o.ClearDescription, o.ClearLocation, o.Archive, o.Unarchive,
		o.SetCaption != nil, o.SetDescription != nil, o.SetLocation != nil,
		o.SetPrivate != nil, o.SetFavorite != nil, o.SetRating != nil, o.SetFlag != nil,
	}
	return !slices.Contains(requested, true)
}

// Validate mirrors the API's own checks: at least one operation, no mutually
// exclusive set/clear pair, and in-range rating, flag and coordinates. Catching
// these here spares a round trip and, more importantly, a rejected transaction.
func (o BulkOperations) Validate() error {
	if o.IsEmpty() {
		return ErrNoOperations
	}
	if err := o.validateExclusions(); err != nil {
		return err
	}
	if o.SetRating != nil && (*o.SetRating < RatingMin || *o.SetRating > RatingMax) {
		return fmt.Errorf("%w: %d", ErrInvalidRating, *o.SetRating)
	}
	if o.SetFlag != nil {
		if err := validateFlag(*o.SetFlag); err != nil {
			return err
		}
	}
	if o.SetLocation != nil {
		return validateCoords(o.SetLocation.Lat, o.SetLocation.Lng)
	}
	return nil
}

// validateExclusions rejects the pairs of operations that contradict each other.
func (o BulkOperations) validateExclusions() error {
	pairs := []struct {
		conflict bool
		names    string
	}{
		{o.SetCaption != nil && o.ClearCaption, "--set-caption and --clear-caption"},
		{o.SetDescription != nil && o.ClearDescription, "--set-description and --clear-description"},
		{o.SetLocation != nil && o.ClearLocation, "--location and --clear-location"},
		{o.Archive && o.Unarchive, "--archive and --unarchive"},
	}
	for _, pair := range pairs {
		if pair.conflict {
			return fmt.Errorf("%w: %s are mutually exclusive", ErrConflictingOperations, pair.names)
		}
	}
	return nil
}

// bulkRequest is the body of POST /photos/bulk: the target photos and the
// operation set applied to each. The server runs the whole batch in one
// transaction, which is why ctl sends one request rather than looping per photo.
type bulkRequest struct {
	PhotoUIDs  []string       `json:"photo_uids"`
	Operations BulkOperations `json:"operations"`
}

// BulkPhotoResult is the outcome of one photo in a bulk request.
type BulkPhotoResult struct {
	PhotoUID string `json:"photo_uid"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}

// BulkCounts summarises a bulk request's per-photo outcomes.
type BulkCounts struct {
	Total   int `json:"total"`
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
	Errored int `json:"errored"`
}

// BulkResult decodes the response of POST /photos/bulk: a per-photo breakdown
// plus the aggregate counts. It is a fourth envelope shape, shared with nothing.
type BulkResult struct {
	Results []BulkPhotoResult `json:"results"`
	Counts  BulkCounts        `json:"counts"`
}

// Bulk applies one operation set to every photo in photoUIDs with a single POST
// /photos/bulk, and returns the raw per-photo result.
//
// It is deliberately one request for the whole batch: the server applies the
// operations in one transaction, so a per-photo loop would trade that atomicity
// for N transactions and N audit entries. It needs the editor or admin role: a
// viewer's token yields a *ForbiddenError. A batch the server considers too large
// yields a *StatusError with status 413.
func (c *Client) Bulk(ctx context.Context, photoUIDs []string, ops BulkOperations) (json.RawMessage, error) {
	uids, err := NormalizeUIDs(photoUIDs)
	if err != nil {
		return nil, err
	}
	if err := ops.Validate(); err != nil {
		return nil, err
	}
	return c.send(ctx, http.MethodPost, "/photos/bulk", bulkRequest{PhotoUIDs: uids, Operations: ops})
}

// DecodeBulkResult decodes a POST /photos/bulk response.
func DecodeBulkResult(raw json.RawMessage) (BulkResult, error) {
	var result BulkResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return BulkResult{}, fmt.Errorf("decoding the bulk result: %w", err)
	}
	return result, nil
}

// ParseLocation parses a "lat,lng" pair into a set-location operation, rejecting
// anything that is not two in-range decimal numbers.
func ParseLocation(raw string) (*BulkLocation, error) {
	lat, lng, ok := strings.Cut(raw, ",")
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrInvalidLocation, raw)
	}
	parsedLat, err := strconv.ParseFloat(strings.TrimSpace(lat), 64)
	if err != nil {
		return nil, fmt.Errorf("%w: %q", ErrInvalidLocation, raw)
	}
	parsedLng, err := strconv.ParseFloat(strings.TrimSpace(lng), 64)
	if err != nil {
		return nil, fmt.Errorf("%w: %q", ErrInvalidLocation, raw)
	}
	if err := validateCoords(parsedLat, parsedLng); err != nil {
		return nil, err
	}
	return &BulkLocation{Lat: parsedLat, Lng: parsedLng}, nil
}

// validateCoords rejects coordinates outside their valid ranges.
func validateCoords(lat, lng float64) error {
	if lat < minLat || lat > maxLat || lng < minLng || lng > maxLng {
		return fmt.Errorf("%w: got %g,%g", ErrInvalidLocation, lat, lng)
	}
	return nil
}
