// Package comments stores the per-photo comment threads that make the library
// social: a short plain-text note by one user on one photo, so a family can work
// out who is on a picture and remember what was happening around it.
//
// # Who may write
//
// Commenting is deliberately open to every authenticated role, viewers included.
// It is participation, not curation: a viewer cannot retitle a photo, move it
// between albums or name a face, but shutting them out of the conversation would
// leave most of the family with a read-only wall. The HTTP layer therefore guards
// the write routes with RequireAuth rather than RequireWrite — the one documented
// exception to the read-only rule (see docs/API.md).
//
// # Plain text, soft deletes
//
// A body is plain text and stays that way: nothing is parsed, rendered or
// sanitised server-side, so a client must escape what it displays. Deletion is
// soft — the row keeps its place with deleted_at stamped, every read filters on
// deleted_at IS NULL — so an audited delete can still be explained afterwards.
//
// Every mutation writes its audit entry in the same transaction as the change
// (see internal/audit): a comment that exists always has a record of who wrote
// it, and a deleted one always has a record of who removed it.
package comments

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxBodyLen is the longest comment body accepted, in characters (runes, not
// bytes) so the limit means the same for a Czech comment as for an English one.
// It mirrors the CHECK constraint on photo_comments.body (migration 0052).
const MaxBodyLen = 2000

var (
	// ErrNotFound indicates the comment does not exist, or has been soft-deleted
	// (a deleted comment is invisible to every read path, including this one).
	ErrNotFound = errors.New("comments: comment not found")
	// ErrPhotoNotFound indicates the photo being commented on does not exist.
	ErrPhotoNotFound = errors.New("comments: photo not found")
	// ErrEmptyBody indicates the body was empty or only whitespace.
	ErrEmptyBody = errors.New("comments: comment body is empty")
	// ErrBodyTooLong indicates the body exceeded MaxBodyLen characters.
	ErrBodyTooLong = errors.New("comments: comment body is too long")
)

// Comment is one stored comment as read back for a client: the body plus who
// wrote it and when. AuthorUID and AuthorName are empty for a comment whose
// author's account has since been deleted (the row survives authorless, see
// migration 0052); the soft-delete timestamp is never exposed, because a deleted
// comment is never listed.
type Comment struct {
	UID       string `json:"uid"`
	PhotoUID  string `json:"photo_uid"`
	AuthorUID string `json:"author_uid"`
	// AuthorName is the author's display name, falling back to the username, as
	// resolved by the store — so a client renders a name without a second lookup.
	AuthorName string `json:"author_name"`
	// AuthorPhotoUID is the cover photo of the person the author's account says
	// it is, so the thread can show a face where it would otherwise draw the
	// first letter of a name. It is nil in every case but the fully-set one: no
	// account, no linked person, or a linked person nobody has chosen a cover
	// photo for — which is the common case, and why the client must always keep
	// its initial-letter fallback.
	//
	// Linking an account to a person is what publishes that face here, which is
	// stated where the link is set. It is a cover photo, chosen by hand on the
	// person's page, and is shown to every reader of the thread — the same
	// readers who can already open that person's page.
	AuthorPhotoUID *string   `json:"author_photo_uid,omitempty"`
	Body           string    `json:"body"`
	CreatedAt      time.Time `json:"created_at"`
	// EditedAt is nil until the author first rewrites the body, which is what
	// lets a client mark a comment as edited without comparing timestamps.
	EditedAt *time.Time `json:"edited_at,omitempty"`
}

// normalizeBody trims surrounding whitespace from a comment body and validates
// what is left: ErrEmptyBody for a blank body, ErrBodyTooLong for one over
// MaxBodyLen characters. It is the single validation point shared by creating
// and editing a comment, so both accept exactly the same bodies.
func normalizeBody(body string) (string, error) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return "", ErrEmptyBody
	}
	if utf8.RuneCountInString(trimmed) > MaxBodyLen {
		return "", ErrBodyTooLong
	}
	return trimmed, nil
}
