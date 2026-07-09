package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// Sentinel errors for the photo command inputs, checked client-side so an
// obvious typo costs no round trip.
var (
	// ErrInvalidPaging indicates a negative limit or offset.
	ErrInvalidPaging = errors.New("ctl: limit and offset must not be negative")
	// ErrInvalidYear indicates a year outside the range a photo could carry.
	ErrInvalidYear = errors.New("ctl: year is out of range")
	// ErrInvalidArchived indicates an unknown --archived value.
	ErrInvalidArchived = errors.New(`ctl: archived must be "true", "false" or "only"`)
	// ErrInvalidSearchMode indicates an unknown --mode value.
	ErrInvalidSearchMode = errors.New(`ctl: mode must be "fulltext", "semantic" or "hybrid"`)
	// ErrEmptyQuery indicates a blank search query, which the API rejects with 400.
	ErrEmptyQuery = errors.New("ctl: search query must not be empty")
	// ErrEmptyUID indicates a blank photo uid.
	ErrEmptyUID = errors.New("ctl: photo uid must not be empty")
)

// Bounds on --year. Photography starts well after minYear, and a year beyond
// maxYear is a typo rather than a filter.
const (
	minYear = 1800
	maxYear = 9999
)

// Search modes accepted by GET /search; the API defaults to hybrid.
const (
	// SearchFulltext ranks by Czech-aware full-text relevance only.
	SearchFulltext = "fulltext"
	// SearchSemantic ranks by CLIP vector similarity to the embedded query.
	SearchSemantic = "semantic"
	// SearchHybrid fuses the two rankings with Reciprocal Rank Fusion.
	SearchHybrid = "hybrid"
)

// Photo is the subset of a photo payload the CLI renders. It is intentionally
// partial: `-o json` prints the server's bytes unchanged, so this struct only
// has to carry what a table column needs.
type Photo struct {
	UID        string     `json:"uid"`
	FileName   string     `json:"file_name"`
	FileSize   int64      `json:"file_size"`
	FileMime   string     `json:"file_mime"`
	FileWidth  int        `json:"file_width"`
	FileHeight int        `json:"file_height"`
	MediaType  string     `json:"media_type"`
	TakenAt    *time.Time `json:"taken_at,omitempty"`
	Title      string     `json:"title"`
	IsFavorite bool       `json:"is_favorite"`
	Rating     int        `json:"rating"`
	Flag       string     `json:"flag"`
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
}

// PhotoPage decodes the envelope of GET /photos and GET /search.
//
// The API has no uniform list envelope — /photos wraps its rows in
// {photos,total,limit,offset,next_offset} while other resources return a bare
// list — so this decoder is deliberately specific to photos. Do not generalise
// it, and do not reshape the API to make a generic one possible: the frontend
// depends on both shapes as they are.
//
// Mode and Degraded are only ever set by the search endpoint.
type PhotoPage struct {
	Photos     []Photo `json:"photos"`
	Total      int     `json:"total"`
	Limit      int     `json:"limit"`
	Offset     int     `json:"offset"`
	NextOffset *int    `json:"next_offset"`
	Mode       string  `json:"mode,omitempty"`
	Degraded   bool    `json:"degraded,omitempty"`
}

// PhotoFile is one stored file of a photo, as listed by the detail endpoint.
type PhotoFile struct {
	FilePath  string `json:"file_path"`
	FileSize  int64  `json:"file_size"`
	FileMime  string `json:"file_mime"`
	IsPrimary bool   `json:"is_primary"`
	Role      string `json:"role"`
}

// NamedRef is a compact album or label reference. Albums carry a title and
// labels a name, so both tags are declared and exactly one is populated.
type NamedRef struct {
	UID   string `json:"uid"`
	Title string `json:"title,omitempty"`
	Name  string `json:"name,omitempty"`
}

// Label returns whichever of the two human-readable fields the server filled in.
func (r NamedRef) Label() string {
	if r.Title != "" {
		return r.Title
	}
	return r.Name
}

// PhotoDetail decodes GET /photos/{uid}: a photo view plus its files and its
// album and label memberships.
type PhotoDetail struct {
	Photo
	Description string      `json:"description"`
	Notes       string      `json:"notes"`
	CameraMake  string      `json:"camera_make"`
	CameraModel string      `json:"camera_model"`
	LensModel   string      `json:"lens_model"`
	Lat         *float64    `json:"lat,omitempty"`
	Lng         *float64    `json:"lng,omitempty"`
	Files       []PhotoFile `json:"files"`
	Albums      []NamedRef  `json:"albums"`
	Labels      []NamedRef  `json:"labels"`
}

// ListOptions carries the GET /photos filters the CLI exposes. A zero value asks
// for the server's defaults: the first page, newest first, archived excluded.
type ListOptions struct {
	Limit  int
	Offset int
	Sort   string
	Order  string
	// Year scopes the list to one calendar year. The API has no year filter, so
	// it is sent as a taken_after/taken_before range; 0 means no year filter.
	Year int
	// Album and Label scope the list to one album's or one label's photos.
	Album string
	Label string
	// Favorite scopes the list to the calling user's own favorites.
	Favorite bool
	// Archived is "", "false", "true" or "only"; empty leaves the server default
	// (live photos only).
	Archived string
}

// validate range-checks the options that the CLI can catch without a round trip.
func (o ListOptions) validate() error {
	if o.Limit < 0 || o.Offset < 0 {
		return ErrInvalidPaging
	}
	if o.Year != 0 && (o.Year < minYear || o.Year > maxYear) {
		return fmt.Errorf("%w: %d", ErrInvalidYear, o.Year)
	}
	switch o.Archived {
	case "", "false", "true", "only":
	default:
		return fmt.Errorf("%w: %q", ErrInvalidArchived, o.Archived)
	}
	return nil
}

// query renders the options as the API's query parameters, omitting every filter
// left at its zero value so the server applies its own defaults.
func (o ListOptions) query() (url.Values, error) {
	if err := o.validate(); err != nil {
		return nil, err
	}
	q := url.Values{}
	setNonEmpty(q, "sort", o.Sort)
	setNonEmpty(q, "order", o.Order)
	setNonEmpty(q, "album", o.Album)
	setNonEmpty(q, "label", o.Label)
	setNonEmpty(q, "archived", o.Archived)
	if o.Limit > 0 {
		q.Set("limit", strconv.Itoa(o.Limit))
	}
	if o.Offset > 0 {
		q.Set("offset", strconv.Itoa(o.Offset))
	}
	if o.Year != 0 {
		after, before := yearBounds(o.Year)
		q.Set("taken_after", after)
		q.Set("taken_before", before)
	}
	if o.Favorite {
		q.Set("favorite", "true")
	}
	return q, nil
}

// setNonEmpty sets key to value unless value is empty.
func setNonEmpty(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}

// yearBounds returns the RFC 3339 instants bracketing a calendar year in UTC.
// Both ends are inclusive because the API compares taken_at with >= and <=, so
// the upper bound is the last representable instant of 31 December.
func yearBounds(year int) (after, before string) {
	start := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 0).Add(-time.Nanosecond)
	return start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano)
}

// SearchOptions carries the GET /search parameters: the query text, the ranking
// mode, and the paging and filters a plain list accepts — except List.Favorite,
// which GET /search does not implement. See query.
type SearchOptions struct {
	Query string
	Mode  string
	List  ListOptions
}

// query renders the search parameters, validating the mode and rejecting a blank
// query the way the API would.
//
// The favorite parameter is dropped rather than forwarded: handleSearch never
// reads it, so sending it would promise a filter the server does not apply. Only
// ErrEmptyQuery and ErrInvalidSearchMode can fail here beyond the list's own
// validation.
func (o SearchOptions) query() (url.Values, error) {
	if o.Query == "" {
		return nil, ErrEmptyQuery
	}
	switch o.Mode {
	case "", SearchFulltext, SearchSemantic, SearchHybrid:
	default:
		return nil, fmt.Errorf("%w: %q", ErrInvalidSearchMode, o.Mode)
	}
	q, err := o.List.query()
	if err != nil {
		return nil, err
	}
	q.Del("favorite")
	q.Set("q", o.Query)
	setNonEmpty(q, "mode", o.Mode)
	return q, nil
}

// ListPhotos fetches one page of GET /photos and returns the raw JSON body, so
// `-o json` can print the API's own bytes unchanged. Decode it with
// DecodePhotoPage to render a table.
func (c *Client) ListPhotos(ctx context.Context, opts ListOptions) (json.RawMessage, error) {
	q, err := opts.query()
	if err != nil {
		return nil, err
	}
	return c.get(ctx, "/photos", q)
}

// GetPhoto fetches GET /photos/{uid} and returns the raw JSON body. It returns
// ErrEmptyUID for a blank uid and a *StatusError with status 404 for a photo
// that does not exist.
func (c *Client) GetPhoto(ctx context.Context, uid string) (json.RawMessage, error) {
	if uid == "" {
		return nil, ErrEmptyUID
	}
	return c.get(ctx, "/photos/"+url.PathEscape(uid), nil)
}

// SearchPhotos fetches one page of GET /search and returns the raw JSON body.
// When the embeddings sidecar is offline the server silently falls back to
// full-text ranking and sets degraded on the response, which DecodePhotoPage
// surfaces so the renderer can say so.
func (c *Client) SearchPhotos(ctx context.Context, opts SearchOptions) (json.RawMessage, error) {
	q, err := opts.query()
	if err != nil {
		return nil, err
	}
	return c.get(ctx, "/search", q)
}

// DecodePhotoPage decodes a /photos or /search envelope.
func DecodePhotoPage(raw json.RawMessage) (PhotoPage, error) {
	var page PhotoPage
	if err := json.Unmarshal(raw, &page); err != nil {
		return PhotoPage{}, fmt.Errorf("decoding photo list: %w", err)
	}
	return page, nil
}

// DecodePhotoDetail decodes a /photos/{uid} response.
func DecodePhotoDetail(raw json.RawMessage) (PhotoDetail, error) {
	var detail PhotoDetail
	if err := json.Unmarshal(raw, &detail); err != nil {
		return PhotoDetail{}, fmt.Errorf("decoding photo detail: %w", err)
	}
	return detail, nil
}
