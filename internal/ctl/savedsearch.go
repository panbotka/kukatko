package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors for the saved-search inputs, checked client-side so an obvious
// mistake costs no round trip.
var (
	// ErrEmptySearchName indicates a blank saved-search name, which the API
	// rejects with 400.
	ErrEmptySearchName = errors.New("ctl: saved search name must not be empty")
	// ErrInvalidSearchParams indicates a --params value that is not a JSON object
	// of strings. The view state is shared with the web UI, which reads every
	// value as a string, so anything else would store a search the app cannot open.
	ErrInvalidSearchParams = errors.New(
		"ctl: saved search params must be a JSON object whose values are all strings")
	// ErrNoSearchEdits indicates a saved-search update that names neither a new
	// name nor new params.
	ErrNoSearchEdits = errors.New("ctl: saved search update needs --name or --param/--params")
)

// NotYoursError reports a 404 on a per-user resource. Saved searches are scoped
// to their owner and one belonging to somebody else is reported as 404 — never
// as 403, which would confirm it exists — so a bare "HTTP 404" cannot tell the
// two apart and the CLI says both.
type NotYoursError struct {
	// Resource names the kind of record, for the message.
	Resource string
	// UID is the record the caller asked for.
	UID string
}

// Error renders the two readings of the status the server deliberately conflates.
func (e *NotYoursError) Error() string {
	return e.Resource + " " + e.UID + " is not yours: it either does not exist or belongs to " +
		"another user, and the server never says which.\n" +
		"`ctl saved-searches list` shows the ones you own."
}

// SavedSearch is a named, owner-private snapshot of a library view: the filters,
// the sort, the query and the search mode the app puts in its URL. The owner is
// never surfaced — every response is already scoped to the caller.
type SavedSearch struct {
	UID       string            `json:"uid"`
	Name      string            `json:"name"`
	Params    map[string]string `json:"params"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// savedSearchBody is the body of both saved-search mutations. Name and Params are
// pointers because PATCH /saved-searches/{uid} genuinely merges — an omitted
// field is left alone server-side — which is why, unlike an album or a label,
// a saved search needs no read before its update.
type savedSearchBody struct {
	Name   *string            `json:"name,omitempty"`
	Params *map[string]string `json:"params,omitempty"`
}

// ParseSearchParams reads a --params value: the view-state object a saved search
// stores, exactly as the app serialises its URL. It must be a JSON object whose
// values are all strings.
func ParseSearchParams(raw string) (map[string]string, error) {
	params := map[string]string{}
	if strings.TrimSpace(raw) == "" {
		return params, nil
	}
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidSearchParams, err)
	}
	return params, nil
}

// ParseSearchParam reads one repeated --param value, `key=value`. The value may
// itself contain `=`, which a query like `q=title=x` needs.
func ParseSearchParam(raw string) (string, string, error) {
	key, value, found := strings.Cut(raw, "=")
	key = strings.TrimSpace(key)
	if !found || key == "" {
		return "", "", fmt.Errorf("%w: %q is not key=value", ErrInvalidSearchParams, raw)
	}
	return key, value, nil
}

// ListSavedSearches fetches GET /saved-searches and returns the raw JSON body:
// the caller's own saved searches, newest first.
//
// The envelope is a bare {"saved_searches": […]} — yet another shape next to the
// /photos, /albums and /labels ones. See ListAlbums on why each resource keeps
// its own decoder. Decode this one with DecodeSavedSearches.
func (c *Client) ListSavedSearches(ctx context.Context) (json.RawMessage, error) {
	return c.get(ctx, "/saved-searches", nil)
}

// GetSavedSearch fetches GET /saved-searches/{uid}. A search that is missing or
// belongs to somebody else yields a *NotYoursError.
func (c *Client) GetSavedSearch(ctx context.Context, uid string) (json.RawMessage, error) {
	if err := requireUID("saved search", uid); err != nil {
		return nil, err
	}
	raw, err := c.get(ctx, savedSearchPath(uid), nil)
	return raw, notYours("saved search", uid, err)
}

// CreateSavedSearch stores a new saved search owned by the calling user and
// returns it as raw JSON. Any signed-in role may keep their own; there is no
// editor check, because a saved search curates nobody else's view.
func (c *Client) CreateSavedSearch(
	ctx context.Context, name string, params map[string]string,
) (json.RawMessage, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrEmptySearchName
	}
	if params == nil {
		params = map[string]string{}
	}
	return c.send(ctx, http.MethodPost, "/saved-searches", savedSearchBody{Name: &name, Params: &params})
}

// UpdateSavedSearch renames a saved search, replaces its stored view, or both,
// and returns the refreshed record as raw JSON. A nil field is left untouched
// server-side, so this endpoint — alone among the ctl updates — needs no read
// first. A foreign or missing search yields a *NotYoursError.
func (c *Client) UpdateSavedSearch(
	ctx context.Context, uid string, name *string, params map[string]string,
) (json.RawMessage, error) {
	if err := requireUID("saved search", uid); err != nil {
		return nil, err
	}
	if name == nil && params == nil {
		return nil, ErrNoSearchEdits
	}
	var body savedSearchBody
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if trimmed == "" {
			return nil, ErrEmptySearchName
		}
		body.Name = &trimmed
	}
	if params != nil {
		body.Params = &params
	}
	raw, err := c.send(ctx, http.MethodPatch, savedSearchPath(uid), body)
	return raw, notYours("saved search", uid, err)
}

// DeleteSavedSearch removes one of the caller's saved searches. The photos it
// would have found are untouched — a saved search is a stored question, not a
// collection. The endpoint answers 204; a foreign or missing search yields a
// *NotYoursError.
func (c *Client) DeleteSavedSearch(ctx context.Context, uid string) error {
	if err := requireUID("saved search", uid); err != nil {
		return err
	}
	_, err := c.send(ctx, http.MethodDelete, savedSearchPath(uid), nil)
	return notYours("saved search", uid, err)
}

// savedSearchPath renders the path of one saved search.
func savedSearchPath(uid string) string {
	return "/saved-searches/" + url.PathEscape(uid)
}

// notYours rewrites the 404 of a per-user resource into the actionable message,
// passing every other error through unchanged.
func notYours(resource, uid string, err error) error {
	var status *StatusError
	if errors.As(err, &status) && status.Status == http.StatusNotFound {
		return &NotYoursError{Resource: resource, UID: uid}
	}
	return err
}

// DecodeSavedSearches decodes the bare {"saved_searches": […]} envelope.
func DecodeSavedSearches(raw json.RawMessage) ([]SavedSearch, error) {
	var payload struct {
		SavedSearches []SavedSearch `json:"saved_searches"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decoding the saved search list: %w", err)
	}
	return payload.SavedSearches, nil
}

// DecodeSavedSearch decodes one saved search.
func DecodeSavedSearch(raw json.RawMessage) (SavedSearch, error) {
	var search SavedSearch
	if err := json.Unmarshal(raw, &search); err != nil {
		return SavedSearch{}, fmt.Errorf("decoding the saved search: %w", err)
	}
	return search, nil
}

// paramsWidth keeps a saved search's stored view on one terminal row. The whole
// object is in `-o json` and `-o llm`, and in `saved-searches get`.
const paramsWidth = 60

// WriteSavedSearches renders the caller's saved searches as a compact table.
func WriteSavedSearches(w io.Writer, searches []SavedSearch) error {
	if len(searches) == 0 {
		return writeLine(w, "no saved searches found")
	}
	rows := make([][]string, 0, len(searches))
	for _, search := range searches {
		rows = append(rows, []string{
			search.UID,
			elide(dash(search.Name), nameWidth),
			elide(dash(formatSearchParams(search.Params)), paramsWidth),
			formatStamp(search.UpdatedAt),
		})
	}
	return writeTable(w, []string{"UID", "NAME", "VIEW", "UPDATED"}, rows)
}

// WriteSavedSearch renders one saved search as an aligned key/value table, with
// the stored view spelled out one key per row — that is the part somebody reads
// a saved search to find out.
func WriteSavedSearch(w io.Writer, search SavedSearch) error {
	rows := make([][2]string, 0, len(search.Params)+5)
	rows = append(rows,
		[2]string{"UID", search.UID},
		[2]string{"NAME", dash(search.Name)},
		[2]string{"KEYS", strconv.Itoa(len(search.Params))},
		[2]string{"CREATED", formatStamp(search.CreatedAt)},
		[2]string{"UPDATED", formatStamp(search.UpdatedAt)},
	)
	for _, key := range sortedKeys(search.Params) {
		rows = append(rows, [2]string{"  " + key, search.Params[key]})
	}
	return writeKeyValues(w, rows)
}

// formatSearchParams renders a stored view on one line, keys in a stable order so
// two listings of the same search read the same.
func formatSearchParams(params map[string]string) string {
	parts := make([]string, 0, len(params))
	for _, key := range sortedKeys(params) {
		parts = append(parts, key+"="+params[key])
	}
	return strings.Join(parts, " ")
}

// sortedKeys returns a map's keys in a stable order.
func sortedKeys(params map[string]string) []string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
