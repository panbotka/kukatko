// Package organize is Kukátko's database access layer for the organisation
// features built on top of the photo catalogue: albums (named groupings of
// photos, always presented chronologically),
// labels (tags attached to photos with a provenance and uncertainty)
// and per-user favorites (replacing photo-sorter's single global favorite flag).
//
// Albums and labels carry an application-generated UID and a unique slug derived
// from their title/name; the store appends a numeric suffix on slug collision.
// Membership and favorites are join tables whose foreign keys cascade on photo,
// label, album and user deletion, so the store never leaves orphan rows.
package organize

import (
	"errors"
	"time"
)

// Sentinel errors returned by the store so callers (handlers, importers, tests)
// can branch with errors.Is.
var (
	// ErrAlbumNotFound indicates no album matched the given key.
	ErrAlbumNotFound = errors.New("organize: album not found")
	// ErrLabelNotFound indicates no label matched the given key.
	ErrLabelNotFound = errors.New("organize: label not found")
	// ErrPhotoNotFound indicates a referenced photo does not exist, surfaced when a
	// membership/attachment write violates the photo foreign key.
	ErrPhotoNotFound = errors.New("organize: photo not found")
	// ErrUserNotFound indicates a referenced user does not exist, surfaced when a
	// favorite write violates the user foreign key.
	ErrUserNotFound = errors.New("organize: user not found")
	// ErrSlugExhausted indicates a unique slug could not be generated for a name
	// after exhausting the numeric-suffix attempts (effectively never in practice).
	ErrSlugExhausted = errors.New("organize: could not generate a unique slug")
	// ErrInvalidType indicates an album type outside the allowed set.
	ErrInvalidType = errors.New("organize: invalid album type")
	// ErrInvalidSource indicates a photo-label source outside the allowed set.
	ErrInvalidSource = errors.New("organize: invalid label source")
	// ErrInvalidRating indicates a star rating outside the allowed 0–5 range.
	ErrInvalidRating = errors.New("organize: invalid rating")
	// ErrInvalidFlag indicates a pick/reject flag outside the allowed set.
	ErrInvalidFlag = errors.New("organize: invalid rating flag")
)

// AlbumType classifies an album, mirrored by the SQL CHECK constraint on
// albums.type.
type AlbumType string

// The recognised album types.
const (
	// AlbumManual is a hand-curated album (the default).
	AlbumManual AlbumType = "album"
	// AlbumFolder is a folder/path-derived grouping (e.g. from import).
	AlbumFolder AlbumType = "folder"
	// AlbumMoment is an auto-generated event grouping.
	AlbumMoment AlbumType = "moment"
	// AlbumState is an auto-generated place (state/region) grouping.
	AlbumState AlbumType = "state"
	// AlbumMonth is an auto-generated calendar-month grouping.
	AlbumMonth AlbumType = "month"
)

// valid reports whether t is one of the recognised album types.
func (t AlbumType) valid() bool {
	switch t {
	case AlbumManual, AlbumFolder, AlbumMoment, AlbumState, AlbumMonth:
		return true
	default:
		return false
	}
}

// LabelSource records where a photo-label attachment came from, mirrored by the
// SQL CHECK constraint on photo_labels.source.
type LabelSource string

// The recognised label sources.
const (
	// SourceManual is a label a user attached by hand (the default).
	SourceManual LabelSource = "manual"
	// SourceAI is a label produced by automatic classification.
	SourceAI LabelSource = "ai"
	// SourceImport is a label carried over from a PhotoPrism / photo-sorter import.
	SourceImport LabelSource = "import"
)

// valid reports whether s is one of the recognised label sources.
func (s LabelSource) valid() bool {
	switch s {
	case SourceManual, SourceAI, SourceImport:
		return true
	default:
		return false
	}
}

// Album is a named grouping of photos. Slug is generated from Title and made
// unique by the store. CoverPhotoUID is nil until a cover is chosen and is
// cleared if that photo is deleted; CreatedBy is nil for system-generated albums
// and is cleared if the creating user is deleted.
type Album struct {
	UID           string    `json:"uid"`
	Slug          string    `json:"slug"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	Type          AlbumType `json:"type"`
	CoverPhotoUID *string   `json:"cover_photo_uid,omitempty"`
	Private       bool      `json:"private"`
	CreatedBy     *string   `json:"created_by,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// AlbumCount is an album paired with how many photos it contains, as returned by
// SearchAlbums.
type AlbumCount struct {
	Album
	// PhotoCount is the number of photos in the album.
	PhotoCount int `json:"photo_count"`
}

// AlbumSummary is an album as the album index renders it: the album with its
// photo count, the cover to show for it, and the span of capture times across
// the photos it holds. It is what ListAlbums returns.
//
// The three added fields are derived, not stored: they are aggregated per album
// in one pass over its membership, so a caller never fetches an album's photos
// to learn what to draw.
type AlbumSummary struct {
	AlbumCount
	// CoverUID is the photo whose thumbnail stands for the album: the hand-picked
	// Album.CoverPhotoUID when one is set, and otherwise the album's newest
	// visible photo — the same photo on every request, never an arbitrary one.
	// It is nil only for an album that holds no visible photo and has no cover
	// chosen by hand, which is the album index's cue to draw the empty state.
	CoverUID *string `json:"cover_uid,omitempty"`
	// TakenFrom and TakenTo bracket the capture times of the album's visible
	// photos: the earliest and the latest. Both are nil together when no visible
	// photo in the album has a known capture time, since a range needs at least
	// one; a single photo (or many taken at one instant) makes them equal.
	TakenFrom *time.Time `json:"taken_from,omitempty"`
	TakenTo   *time.Time `json:"taken_to,omitempty"`
}

// AlbumUpdate carries the user-editable fields applied by Store.UpdateAlbum.
// Title is re-slugged on change; CoverPhotoUID clears (sets NULL) when nil.
type AlbumUpdate struct {
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	Type          AlbumType `json:"type"`
	CoverPhotoUID *string   `json:"cover_photo_uid"`
	Private       bool      `json:"private"`
}

// Label is a tag that can be attached to photos. Slug is generated from Name and
// made unique by the store. Priority floats higher labels up in the UI.
type Label struct {
	UID       string    `json:"uid"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Priority  int       `json:"priority"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// LabelCount is a label paired with how many photos carry it, as returned by
// ListLabels.
type LabelCount struct {
	Label
	// PhotoCount is the number of photos the label is attached to.
	PhotoCount int `json:"photo_count"`
}

// LabelUpdate carries the user-editable fields applied by Store.UpdateLabel.
// Name is re-slugged on change.
type LabelUpdate struct {
	Name     string `json:"name"`
	Priority int    `json:"priority"`
}

// RatingFlag is a per-user personal mark on a photo, a single mutually-exclusive
// value mirrored by the SQL CHECK constraint on user_ratings.flag. The stored
// strings are internal and kept stable across the reframing from pick/reject
// culling to a neutral eye/thumbs-up/thumbs-down mark; the UI maps them to icons.
type RatingFlag string

// The recognised rating flags. The stored strings are historical: 'pick' surfaces
// as thumbs-up and 'reject' as thumbs-down in the UI.
const (
	// FlagNone is the absence of a personal mark (the default).
	FlagNone RatingFlag = "none"
	// FlagPick marks a photo with a thumbs-up (stored as the historical "pick").
	FlagPick RatingFlag = "pick"
	// FlagReject marks a photo with a thumbs-down (stored as the historical "reject").
	FlagReject RatingFlag = "reject"
	// FlagEye marks a photo with the neutral eye ("seen"/keep-an-eye-on) mark.
	FlagEye RatingFlag = "eye"
)

// valid reports whether f is one of the recognised rating flags.
func (f RatingFlag) valid() bool {
	switch f {
	case FlagNone, FlagPick, FlagReject, FlagEye:
		return true
	default:
		return false
	}
}

// The inclusive bounds of a star rating, mirroring the SQL CHECK constraint on
// user_ratings.rating.
const (
	// ratingMin is the lowest star rating (unrated).
	ratingMin = 0
	// ratingMax is the highest star rating.
	ratingMax = 5
)

// PhotoRating is a user's star rating (0–5) and personal mark for one photo.
// A photo a user has never rated reads back as the zero value — rating 0, flag
// "none" — because the store keeps no row for all-default ratings.
type PhotoRating struct {
	// Rating is the user's star rating from 0 (unrated) to 5.
	Rating int `json:"rating"`
	// Flag is the user's personal mark; one of "none", "pick", "reject", "eye".
	Flag string `json:"flag"`
}
