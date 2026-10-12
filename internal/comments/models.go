// Package comments stores the comment threads that make the library social: a
// short plain-text note by one user on one subject, so a family can work out who
// is on a picture and remember what was happening around it.
//
// # Three subjects, one thread
//
// A comment hangs off a photograph, a task (internal/phototask) or an album
// (internal/organize), and never more than one of them. The three are the same
// conversation with a different subject — a question asked of whoever knows the
// answer, and the answer written underneath it — so they share one table, one
// rate limit, one set of audit actions and one soft-delete rule rather than
// being built three times. Which subject a thread belongs to is the caller's to
// state, as a Subject; everything below it is identical.
//
// # Who may write
//
// Commenting is deliberately open to every authenticated role, viewers included.
// It is participation, not curation: a viewer cannot retitle a photo, move it
// between albums or name a face, but shutting them out of the conversation would
// leave most of the family with a read-only wall. On a task it is the point of
// the feature — the person who knows in which year the house was rebuilt is
// rarely the person who edits the library. The HTTP layer therefore guards the
// write routes with RequireAuth rather than RequireWrite — the one documented
// exception to the read-only rule (see docs/API.md).
//
// # Plain text, soft deletes
//
// A body is plain text and stays that way: nothing is rendered or sanitised
// server-side, so a client must escape what it displays. The one thing the
// store reads out of a body is who it mentions — `@[Name](user:<uid>)` tokens,
// see ParseMentions — which it resolves against the accounts, stores in
// comment_mentions and reports to a MentionObserver (the comment_mention
// notification) for the mentions that are new on the comment. Deletion is
// soft — the row keeps its place with deleted_at stamped, every read filters on
// deleted_at IS NULL — so an audited delete can still be explained afterwards.
//
// Every mutation writes its audit entry in the same transaction as the change
// (see internal/audit): a comment that exists always has a record of who wrote
// it, and a deleted one always has a record of who removed it.
package comments

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// MaxBodyLen is the longest comment body accepted, in characters (runes, not
// bytes) so the limit means the same for a Czech comment as for an English one.
// It mirrors the CHECK constraint on comments.body (migration 0052).
const MaxBodyLen = 2000

// SubjectKind names what a comment thread hangs off. The zero value is not a
// kind: a Subject always says which of the three it is.
type SubjectKind string

const (
	// SubjectPhoto is a thread on one photograph.
	SubjectPhoto SubjectKind = "photo"
	// SubjectTask is a thread on one task — the conversation that answers it.
	SubjectTask SubjectKind = "task"
	// SubjectAlbum is a thread on one album — the conversation about the whole
	// event rather than one picture of it.
	SubjectAlbum SubjectKind = "album"
)

// Subject identifies the one thing a comment is about. Exactly one subject per
// comment is a database constraint (comments_one_subject, migration 0080), and
// this type is how that reaches the store: a kind and the uid of the row.
type Subject struct {
	Kind SubjectKind
	UID  string
}

// PhotoSubject returns the Subject naming the photograph uid.
func PhotoSubject(uid string) Subject {
	return Subject{Kind: SubjectPhoto, UID: uid}
}

// TaskSubject returns the Subject naming the task uid.
func TaskSubject(uid string) Subject {
	return Subject{Kind: SubjectTask, UID: uid}
}

// AlbumSubject returns the Subject naming the album uid.
func AlbumSubject(uid string) Subject {
	return Subject{Kind: SubjectAlbum, UID: uid}
}

// Valid reports whether the subject names one of the known kinds and carries a
// uid. An invalid subject is a programming error rather than bad input — every
// caller builds one from a route it has already resolved — so the store refuses
// it with ErrInvalidSubject instead of guessing.
func (s Subject) Valid() bool {
	return s.UID != "" && (s.Kind == SubjectPhoto || s.Kind == SubjectTask || s.Kind == SubjectAlbum)
}

var (
	// ErrNotFound indicates the comment does not exist, or has been soft-deleted
	// (a deleted comment is invisible to every read path, including this one).
	ErrNotFound = errors.New("comments: comment not found")
	// ErrSubjectNotFound indicates the photo, task or album being commented on
	// does not exist.
	ErrSubjectNotFound = errors.New("comments: subject not found")
	// ErrInvalidSubject indicates the Subject named no known kind, or no uid.
	ErrInvalidSubject = errors.New("comments: invalid subject")
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
//
// Exactly one of PhotoUID, TaskUID and AlbumUID is set, matching the row's one
// subject.
type Comment struct {
	UID string `json:"uid"`
	// PhotoUID is the photograph this comment is on, empty on any other thread.
	PhotoUID string `json:"photo_uid,omitempty"`
	// TaskUID is the task this comment is on, empty on any other thread.
	TaskUID string `json:"task_uid,omitempty"`
	// AlbumUID is the album this comment is on, empty on any other thread.
	AlbumUID  string `json:"album_uid,omitempty"`
	AuthorUID string `json:"author_uid"`
	// AuthorName is the author's display name, falling back to the username, as
	// resolved by the store — so a client renders a name without a second lookup.
	AuthorName string `json:"author_name"`
	// AuthorPhotoUID is the cover photo of the person the author's account says
	// it is. It is nil in every case but the fully-set one: no account, no linked
	// person, or a linked person nobody has chosen a cover photo for — which is
	// the common case.
	//
	// It predates profile pictures and is no longer how a thread draws a face:
	// the web client asks GET /users/{uid}/avatar with AuthorUID instead, which
	// resolves the whole chain (an uploaded picture, a picked photo, the linked
	// person's face) rather than this one link of it. The field stays for API
	// consumers that read a thread without following that endpoint, and because
	// it is a true fact about the author either way.
	AuthorPhotoUID *string   `json:"author_photo_uid,omitempty"`
	Body           string    `json:"body"`
	CreatedAt      time.Time `json:"created_at"`
	// EditedAt is nil until the author first rewrites the body, which is what
	// lets a client mark a comment as edited without comparing timestamps.
	EditedAt *time.Time `json:"edited_at,omitempty"`
	// Mentions are the accounts the body's mention tokens name that still
	// exist, in order of first mention, each with its current name — so a
	// renamed account reads under its new name — and its linked person. A
	// token whose account is gone has no entry; a client shows the token's
	// own name then. Never nil: a comment without mentions carries an empty
	// array.
	Mentions []Mention `json:"mentions"`
}

// Mention is one account a comment mentions, as read back: resolved against
// the users table when the comment is read, not when it was written.
type Mention struct {
	// UserUID is the mentioned account.
	UserUID string `json:"user_uid"`
	// Name is the account's current display name, falling back to the username.
	Name string `json:"name"`
	// SubjectUID is the person of the library the account says it is, or nil —
	// the common case — when nobody has said. It is where a mention links.
	SubjectUID *string `json:"subject_uid"`
}

// MentionObserver hears that a comment freshly mentions accounts, inside the
// transaction that wrote or edited it — so whatever it does commits with the
// comment or not at all. It is how the comment_mention notification
// (internal/mentionnotify) learns about every mention without the HTTP layer
// having to remember it.
//
// It hears only mentions new on the comment: the ones a create carries, and
// on an edit the ones the previous body did not have — fixing a typo does not
// announce everybody again. A self-mention is never reported. It is not
// called at all when nothing is new.
//
// An error it returns fails the comment. An observer that must never cost a
// comment (a notification is worth less than the remark) contains its own
// failures, for instance under a savepoint.
type MentionObserver interface {
	// Mentioned hears that c now mentions mentioned, none of them before.
	Mentioned(ctx context.Context, tx pgx.Tx, c Comment, mentioned []Mention) error
}

// Subject returns the subject this comment hangs off, reconstructed from
// whichever of the three uid fields the row carries.
func (c Comment) Subject() Subject {
	switch {
	case c.TaskUID != "":
		return TaskSubject(c.TaskUID)
	case c.AlbumUID != "":
		return AlbumSubject(c.AlbumUID)
	default:
		return PhotoSubject(c.PhotoUID)
	}
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
