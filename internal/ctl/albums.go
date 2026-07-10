package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Sentinel errors for the album command inputs, checked client-side so an obvious
// typo costs no round trip.
var (
	// ErrEmptyTitle indicates a blank album title, which the API rejects with 400.
	ErrEmptyTitle = errors.New("ctl: album title must not be empty")
	// ErrInvalidAlbumType indicates an album type the API does not recognise.
	ErrInvalidAlbumType = errors.New(
		`ctl: album type must be "album", "folder", "moment", "state" or "month"`)
)

// Album types accepted by POST /albums. Only AlbumManual is worth creating by
// hand; the rest are the structural groupings the server generates itself, and
// are listed here so a hand-written --type is validated rather than 400'd.
const (
	// AlbumManual is a hand-curated album (the API's default for a blank type).
	AlbumManual = "album"
	// AlbumFolder is a folder/path-derived grouping.
	AlbumFolder = "folder"
	// AlbumMoment is an auto-generated event grouping.
	AlbumMoment = "moment"
	// AlbumState is an auto-generated place grouping.
	AlbumState = "state"
	// AlbumMonth is an auto-generated calendar-month grouping.
	AlbumMonth = "month"
)

// Album is the subset of an album payload the CLI renders. PhotoCount is only
// populated by GET /albums, which pairs every album with its size; GET
// /albums/{uid} returns a bare album and leaves it zero.
type Album struct {
	UID           string    `json:"uid"`
	Slug          string    `json:"slug"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	Type          string    `json:"type"`
	CoverPhotoUID *string   `json:"cover_photo_uid,omitempty"`
	Private       bool      `json:"private"`
	PhotoCount    int       `json:"photo_count"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// AlbumInput is the body of POST /albums. Every field but Title is optional and
// omitted from the wire when left at its zero value, so the server applies its
// own defaults (type "album").
type AlbumInput struct {
	Title         string  `json:"title"`
	Description   string  `json:"description,omitempty"`
	Type          string  `json:"type,omitempty"`
	CoverPhotoUID *string `json:"cover_photo_uid,omitempty"`
	Private       bool    `json:"private,omitempty"`
}

// validate range-checks the input the CLI can reject without a round trip.
func (in AlbumInput) validate() error {
	if strings.TrimSpace(in.Title) == "" {
		return ErrEmptyTitle
	}
	switch in.Type {
	case "", AlbumManual, AlbumFolder, AlbumMoment, AlbumState, AlbumMonth:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidAlbumType, in.Type)
	}
}

// photoUIDsBody is the body of the album membership endpoints and the shape they
// echo back: the album's photos in display order after the mutation.
type photoUIDsBody struct {
	PhotoUIDs []string `json:"photo_uids"`
}

// ListAlbums fetches GET /albums and returns the raw JSON body.
//
// The envelope is a bare {"albums": […]} with no paging fields — unlike /photos,
// which wraps its rows in {photos,total,limit,offset,next_offset}. The API has no
// uniform list envelope and the frontend depends on both shapes, so each resource
// gets its own decoder rather than a reshaped API. Decode this one with
// DecodeAlbums.
func (c *Client) ListAlbums(ctx context.Context) (json.RawMessage, error) {
	return c.get(ctx, "/albums", nil)
}

// GetAlbum fetches GET /albums/{uid} and returns the raw JSON body: a bare album
// object, without the photo count the list carries. A missing album yields a
// *StatusError with status 404.
func (c *Client) GetAlbum(ctx context.Context, uid string) (json.RawMessage, error) {
	if err := requireUID("album", uid); err != nil {
		return nil, err
	}
	return c.get(ctx, "/albums/"+url.PathEscape(uid), nil)
}

// CreateAlbum posts a new album to POST /albums and returns the created album's
// raw JSON, generated UID and unique slug included. It needs the editor or admin
// role: a viewer's token yields a *ForbiddenError.
func (c *Client) CreateAlbum(ctx context.Context, in AlbumInput) (json.RawMessage, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	return c.send(ctx, http.MethodPost, "/albums", in)
}

// AddAlbumPhotos appends photoUIDs to the album, after the photos already in it,
// and returns the refreshed membership order as raw JSON. It needs the editor or
// admin role.
func (c *Client) AddAlbumPhotos(ctx context.Context, uid string, photoUIDs []string) (json.RawMessage, error) {
	return c.albumMembership(ctx, http.MethodPost, uid, photoUIDs)
}

// RemoveAlbumPhotos removes photoUIDs from the album and returns the refreshed
// membership order as raw JSON. Removing a photo that is not a member is a no-op
// server-side. It needs the editor or admin role.
func (c *Client) RemoveAlbumPhotos(ctx context.Context, uid string, photoUIDs []string) (json.RawMessage, error) {
	return c.albumMembership(ctx, http.MethodDelete, uid, photoUIDs)
}

// albumMembership drives both membership endpoints, which share a path, a body
// and a response and differ only in the verb.
func (c *Client) albumMembership(
	ctx context.Context, method, uid string, photoUIDs []string,
) (json.RawMessage, error) {
	if err := requireUID("album", uid); err != nil {
		return nil, err
	}
	if len(photoUIDs) == 0 {
		return nil, ErrNoPhotoUIDs
	}
	path := "/albums/" + url.PathEscape(uid) + "/photos"
	return c.send(ctx, method, path, photoUIDsBody{PhotoUIDs: photoUIDs})
}

// DecodeAlbums decodes the bare {"albums": […]} envelope of GET /albums.
func DecodeAlbums(raw json.RawMessage) ([]Album, error) {
	var payload struct {
		Albums []Album `json:"albums"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decoding the album list: %w", err)
	}
	return payload.Albums, nil
}

// DecodeAlbum decodes one album, as returned by GET /albums/{uid} and POST
// /albums.
func DecodeAlbum(raw json.RawMessage) (Album, error) {
	var album Album
	if err := json.Unmarshal(raw, &album); err != nil {
		return Album{}, fmt.Errorf("decoding the album: %w", err)
	}
	return album, nil
}

// DecodePhotoUIDs decodes the {"photo_uids": […]} response the album membership
// endpoints echo back.
func DecodePhotoUIDs(raw json.RawMessage) ([]string, error) {
	var payload photoUIDsBody
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decoding the album membership: %w", err)
	}
	return payload.PhotoUIDs, nil
}
