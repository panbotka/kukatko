package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// Sentinel errors for the curation command inputs, checked client-side so an
// obvious typo costs no round trip.
var (
	// ErrInvalidRating indicates a star rating outside the 0–5 range the API accepts.
	ErrInvalidRating = errors.New("ctl: rating must be between 0 and 5")
	// ErrInvalidFlag indicates a personal-mark flag the API does not recognise.
	ErrInvalidFlag = errors.New(`ctl: flag must be "none", "pick", "reject" or "eye"`)
	// ErrEmptyRating indicates a rating command that would change neither the stars
	// nor the flag, which the API rejects with 400.
	ErrEmptyRating = errors.New("ctl: a rating command must set the rating, the flag, or both")
)

// The inclusive bounds of a star rating, mirroring the API's validation and the
// SQL CHECK on user_ratings.rating.
const (
	// RatingMin is the lowest star rating, equivalent to no rating at all.
	RatingMin = 0
	// RatingMax is the highest star rating.
	RatingMax = 5
)

// Personal-mark flags accepted by PUT /photos/{uid}/rating. The stored strings
// are historical; the web UI surfaces them as icons (thumbs-up/thumbs-down/eye).
const (
	// FlagNone is the absence of a personal mark.
	FlagNone = "none"
	// FlagPick marks a photo with a thumbs-up (stored "pick").
	FlagPick = "pick"
	// FlagReject marks a photo with a thumbs-down (stored "reject").
	FlagReject = "reject"
	// FlagEye marks a photo with the neutral eye mark.
	FlagEye = "eye"
)

// ratingBody is the body of PUT /photos/{uid}/rating: an optional star rating and
// an optional cull flag. An omitted field is left unchanged server-side, so both
// are pointers and both are dropped from the wire when nil.
type ratingBody struct {
	Rating *int    `json:"rating,omitempty"`
	Flag   *string `json:"flag,omitempty"`
}

// validate mirrors the API's own checks so a bad value never costs a round trip.
func (b ratingBody) validate() error {
	if b.Rating == nil && b.Flag == nil {
		return ErrEmptyRating
	}
	if b.Rating != nil && (*b.Rating < RatingMin || *b.Rating > RatingMax) {
		return fmt.Errorf("%w: %d", ErrInvalidRating, *b.Rating)
	}
	if b.Flag != nil {
		return validateFlag(*b.Flag)
	}
	return nil
}

// validateFlag rejects a personal-mark flag the API does not recognise.
func validateFlag(flag string) error {
	switch flag {
	case FlagNone, FlagPick, FlagReject, FlagEye:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidFlag, flag)
	}
}

// ListFavorites fetches one page of GET /favorites — the calling user's own
// favorited photos — and returns the raw JSON body. Favorites are per-user, so
// the page is scoped by the token, not by any parameter.
//
// The envelope is the /photos one, so decode it with DecodePhotoPage. The
// favorite filter is dropped rather than forwarded: the endpoint scopes itself to
// the caller and would ignore it.
func (c *Client) ListFavorites(ctx context.Context, opts ListOptions) (json.RawMessage, error) {
	q, err := opts.query()
	if err != nil {
		return nil, err
	}
	q.Del("favorite")
	return c.get(ctx, "/favorites", q)
}

// AddFavorite favorites one photo for the calling user. It is idempotent, needs
// only a logged-in role (favorites are per-user, so a viewer may set their own),
// and answers 204, so there is nothing to return but an error. A missing photo
// yields a *StatusError with status 404.
func (c *Client) AddFavorite(ctx context.Context, photoUID string) error {
	return c.toggleFavorite(ctx, http.MethodPut, photoUID)
}

// RemoveFavorite unfavorites one photo for the calling user. It is idempotent.
func (c *Client) RemoveFavorite(ctx context.Context, photoUID string) error {
	return c.toggleFavorite(ctx, http.MethodDelete, photoUID)
}

// toggleFavorite drives both favorite endpoints, which share a path and differ
// only in the verb.
func (c *Client) toggleFavorite(ctx context.Context, method, photoUID string) error {
	if err := requireUID("photo", photoUID); err != nil {
		return err
	}
	_, err := c.send(ctx, method, "/photos/"+url.PathEscape(photoUID)+"/favorite", nil)
	return err
}

// SetRating sets the calling user's star rating and/or cull flag on one photo.
// A nil rating or flag leaves that side unchanged server-side; at least one must
// be supplied. Ratings are per-user, so a viewer may rate their own view. The
// endpoint answers 204, so there is nothing to return but an error.
func (c *Client) SetRating(ctx context.Context, photoUID string, rating *int, flag *string) error {
	if err := requireUID("photo", photoUID); err != nil {
		return err
	}
	body := ratingBody{Rating: rating, Flag: flag}
	if err := body.validate(); err != nil {
		return err
	}
	_, err := c.send(ctx, http.MethodPut, ratingPath(photoUID), body)
	return err
}

// ClearRating removes the calling user's rating and flag from one photo. It is
// idempotent: clearing an unrated photo still succeeds.
func (c *Client) ClearRating(ctx context.Context, photoUID string) error {
	if err := requireUID("photo", photoUID); err != nil {
		return err
	}
	_, err := c.send(ctx, http.MethodDelete, ratingPath(photoUID), nil)
	return err
}

// ratingPath renders the rating path for one photo.
func ratingPath(photoUID string) string {
	return "/photos/" + url.PathEscape(photoUID) + "/rating"
}
