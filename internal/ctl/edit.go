package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Sentinel errors for the photo-edit inputs, checked client-side so an obvious
// mistake costs neither a round trip nor an audit entry.
var (
	// ErrNoEdits indicates an edit that would send an empty body — the server
	// would happily accept it and record an audit entry for a change nobody made.
	ErrNoEdits = errors.New("ctl: edit needs at least one field")
	// ErrConflictingEdits indicates a set-and-clear pair for the same field.
	ErrConflictingEdits = errors.New("ctl: conflicting edit flags")
	// ErrInvalidTimestamp indicates a --taken-at that is not one of the accepted
	// date or timestamp spellings.
	ErrInvalidTimestamp = errors.New("ctl: taken-at must be a date or an RFC 3339 timestamp")
	// ErrIncompleteLocation indicates only one half of a coordinate pair.
	ErrIncompleteLocation = errors.New("ctl: --lat and --lng must be given together")
)

// LocationSourceManual is the only location_source PATCH /photos/{uid} accepts.
// Sending it means "I vouch for this coordinate": it promotes an estimated
// location to one the user owns, and the estimator stops overwriting it.
const LocationSourceManual = "manual"

// takenAtLayouts are the spellings --taken-at accepts, tried in order. A bare
// date and a date with a time are read as UTC, which is what the API does with
// the same input; anything more specific has to say its offset.
var takenAtLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}

// PhotoEdit is the body of PATCH /photos/{uid}, built one field at a time. It is
// always handled as a pointer — it is a builder, and every method below either
// mutates it or reads what was built.
//
// It is a key/value map rather than a struct of pointers because the API reads
// *presence*, not nullness: a field the caller did not touch must not appear in
// the request at all, while a field they cleared must appear with an explicit
// null. Resending an unchanged taken_at, for instance, would flip its source from
// exif to manual — so "the same value" and "no value" are not the same request,
// and only a map can say so honestly.
//
// The zero value is an empty edit; add fields with Set, SetTime and Clear.
type PhotoEdit struct {
	fields map[string]any
}

// Set records a field with a concrete value. A value that is an empty string is
// a legitimate edit — it empties a NOT NULL text column — and is not the same as
// leaving the field out.
func (e *PhotoEdit) Set(name string, value any) {
	if e.fields == nil {
		e.fields = make(map[string]any)
	}
	e.fields[name] = value
}

// SetTime records a timestamp field in the RFC 3339 spelling the API decodes.
func (e *PhotoEdit) SetTime(name string, value time.Time) {
	e.Set(name, value.Format(time.RFC3339))
}

// Clear records a field as explicitly empty, which the API distinguishes from an
// omitted one. Only the nullable columns (taken_at, lat, lng) accept it; a text
// column is emptied by setting it to "".
func (e *PhotoEdit) Clear(name string) {
	e.Set(name, nil)
}

// IsEmpty reports whether the edit would change nothing. A nil edit is empty.
func (e *PhotoEdit) IsEmpty() bool {
	return e == nil || len(e.fields) == 0
}

// Names returns the fields the edit carries, sorted, for a confirmation line.
func (e *PhotoEdit) Names() []string {
	return slices.Sorted(maps.Keys(e.fields))
}

// MarshalJSON renders the edit as the request body. An empty edit marshals to
// {}, which Validate refuses to send.
func (e *PhotoEdit) MarshalJSON() ([]byte, error) {
	if e.IsEmpty() {
		return []byte("{}"), nil
	}
	encoded, err := json.Marshal(e.fields)
	if err != nil {
		return nil, fmt.Errorf("encoding the photo edit: %w", err)
	}
	return encoded, nil
}

// Body renders the request body as indented JSON, for --dry-run to print.
func (e *PhotoEdit) Body() (string, error) {
	encoded, err := json.MarshalIndent(e.fields, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding the photo edit: %w", err)
	}
	return string(encoded), nil
}

// Validate refuses an edit that carries nothing.
//
// It deliberately stops there. The server owns the rules — the length caps on the
// credits and the dating note, the clearing of taken_at_note when the estimate
// flag goes away, which location_source values a client may claim — and a second
// copy of them here would be one that drifts. ctl reports the 400 it gets back.
func (e *PhotoEdit) Validate() error {
	if e.IsEmpty() {
		return ErrNoEdits
	}
	return nil
}

// EditPhoto applies a partial metadata edit with PATCH /photos/{uid} and returns
// the refreshed detail body the server answers with, asking for the optional
// blocks opts names so a caller can read back what it wrote in one round trip.
//
// It needs the editor or admin role: a viewer's token yields a *ForbiddenError.
// A rule the server enforces — an over-long credit, a location_source it will not
// accept — yields a *StatusError with status 400 and the server's own sentence.
func (c *Client) EditPhoto(
	ctx context.Context, uid string, edit *PhotoEdit, opts PhotoDetailOptions,
) (json.RawMessage, error) {
	if err := requireUID("photo", uid); err != nil {
		return nil, err
	}
	if err := edit.Validate(); err != nil {
		return nil, err
	}
	path := "/photos/" + url.PathEscape(uid)
	if query := opts.query(); len(query) > 0 {
		path += "?" + query.Encode()
	}
	return c.send(ctx, http.MethodPatch, path, edit)
}

// ParseTakenAt reads a --taken-at value in any of takenAtLayouts, returning the
// instant to send. A bare date means midnight UTC of that day.
func ParseTakenAt(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	for _, layout := range takenAtLayouts {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("%w: %q", ErrInvalidTimestamp, raw)
}
