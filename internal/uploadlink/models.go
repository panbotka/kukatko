// Package uploadlink is the domain of upload links: a short link a curator
// creates and posts to a group chat, through which anybody — signed in or not —
// uploads photos straight into preset albums and labels.
//
// The short code in the URL (/u/<code>) is the link's whole credential, so it is
// stored only as its SHA-256, like an API token's secret: the plaintext exists in
// the create response and nowhere else. A link is live until it expires or is
// revoked; revocation is final, expiry can be moved.
//
// The package owns four tables (migration 0090): the links, their album and
// label targets, and the provenance of every photo that came in through one —
// which link, the name the uploader typed, and either their account or the hash
// of their anonymous browser session, which is what lets a person who registers
// afterwards claim the photos they just uploaded. Every link mutation and every
// recorded upload writes its audit entry in the mutation's transaction.
//
// It holds no HTTP code (internal/uploadlinkapi) and never runs the upload
// pipeline itself (internal/ingest): it records what an upload produced.
package uploadlink

import (
	"errors"
	"time"
)

// Errors the store and the validation return. The HTTP layer maps them onto
// status codes; nothing else about a link leaks through them.
var (
	// ErrNotFound means no link has this UID or code.
	ErrNotFound = errors.New("uploadlink: link not found")
	// ErrRevoked means the link was revoked; it can no longer be extended.
	ErrRevoked = errors.New("uploadlink: link is revoked")
	// ErrNoTargets means a link was created without any album or label.
	ErrNoTargets = errors.New("uploadlink: a link needs at least one album or label")
	// ErrTooManyTargets means a link names more albums or labels than MaxTargets.
	ErrTooManyTargets = errors.New("uploadlink: too many albums or labels")
	// ErrTargetNotFound means a named album or label does not exist.
	ErrTargetNotFound = errors.New("uploadlink: album or label not found")
	// ErrTitleTooLong means the title exceeds MaxTitleLen characters.
	ErrTitleTooLong = errors.New("uploadlink: title is too long")
	// ErrNoteTooLong means the note exceeds MaxNoteLen characters.
	ErrNoteTooLong = errors.New("uploadlink: note is too long")
)

// Limits on what a link may carry. They are characters (runes), not bytes.
const (
	// MaxTitleLen caps a link's title.
	MaxTitleLen = 200
	// MaxNoteLen caps the note shown to uploaders.
	MaxNoteLen = 2000
	// MaxUploaderNameLen caps the "from whom" name an uploader types; a longer
	// one is cut, never refused — the upload matters more than the signature.
	MaxUploaderNameLen = 100
	// MaxTargets caps the albums and the labels of one link, each.
	MaxTargets = 20
)

// State is where a link stands in its life.
type State string

const (
	// StateActive means uploads are accepted.
	StateActive State = "active"
	// StateExpired means the expiry passed; extending it makes it active again.
	StateExpired State = "expired"
	// StateRevoked means a curator revoked it for good.
	StateRevoked State = "revoked"
)

// Target is one album or label a link files its photos into: its UID and the
// name an uploader sees on a chip.
type Target struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
}

// Link is one upload link as the management list shows it.
type Link struct {
	// UID is the primary key, "ul" followed by random base32 characters. It is
	// not the code: the code is the credential, the UID merely names the row.
	UID string `json:"uid"`
	// Title and Note are shown to uploaders on the public page.
	Title string `json:"title"`
	Note  string `json:"note"`
	// CreatedBy is the creator's account UID, nil once the account is gone.
	CreatedBy *string `json:"created_by"`
	// CreatorName is the creator's display name (or username), empty when the
	// account is gone.
	CreatorName string    `json:"created_by_name"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	// RevokedAt is when the link was revoked, nil while it is not.
	RevokedAt *time.Time `json:"revoked_at"`
	// UploadCount is how many files came in through the link (new photos and
	// duplicates alike), and LastUsedAt when the latest did.
	UploadCount int        `json:"upload_count"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	// Albums and Labels are the targets, ordered by name.
	Albums []Target `json:"albums"`
	Labels []Target `json:"labels"`
}

// StateAt reports the link's state at now: revoked wins over expired, because a
// revoked link stays dead whatever its expiry says.
func (l Link) StateAt(now time.Time) State {
	if l.RevokedAt != nil {
		return StateRevoked
	}
	if !now.Before(l.ExpiresAt) {
		return StateExpired
	}
	return StateActive
}

// NewLink is what a curator asks for when creating a link.
type NewLink struct {
	// Title and Note are shown to uploaders; both may be empty.
	Title string
	Note  string
	// CreatedBy is the creating account's UID (required).
	CreatedBy string
	// ExpiresAt is when the link stops accepting uploads.
	ExpiresAt time.Time
	// AlbumUIDs and LabelUIDs are the targets; together they must name at least
	// one. Duplicates are ignored.
	AlbumUIDs []string
	LabelUIDs []string
}

// Upload is one file that came in through a link, as RecordUpload stores it.
type Upload struct {
	// LinkUID is the link the file came through.
	LinkUID string
	// PhotoUID is the photo the file resolved to.
	PhotoUID string
	// Created tells a freshly catalogued photo from a duplicate that was already
	// in the library and is merely filed into the link's targets.
	Created bool
	// UploaderName is the name the uploader typed, already normalised (see
	// NormalizeUploaderName); it may be empty.
	UploaderName string
	// UploadedBy is the uploader's account UID when they were signed in.
	UploadedBy string
	// SessionHash is the hash of an anonymous uploader's session token (see
	// HashSecret); empty for a signed-in uploader.
	SessionHash string
}

// Provenance is where a photo came from when it came through a link: the link
// and the name its uploader typed. It feeds the photo's metadata sidecar.
type Provenance struct {
	LinkUID      string
	LinkTitle    string
	UploaderName string
	UploadedAt   time.Time
}
