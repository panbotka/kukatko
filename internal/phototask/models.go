// Package phototask is the work queue over the library: a question about a
// frozen group of photographs, the state of play, and — through
// internal/comments — the conversation that answers it.
//
// # What a task is for
//
// Curating an inherited library arrives in batches that share a doubt: scans
// whose printed caption disagrees with their stored date, photographs dated
// before their camera was on sale, several copies of one picture carrying three
// different years. Each batch ends at a question only a person can answer, and a
// task is that question made addressable: it has a URL to send to whoever knows,
// the photographs it is about, and a state saying whose move it is.
//
// # Frozen membership, remembered query
//
// The group is an explicit list of photo uids. The search that produced it is
// kept too (Task.Query) but never re-run: a task exists because the data is
// wrong and is answered by fixing it, so a live query would empty the group the
// moment the work was done — taking the record of what was touched with it. The
// frozen list is the receipt, which is why a closed task is worth keeping and why
// the batch labels it replaces never could be deleted.
//
// # Why the store reads the comments table
//
// Task listings carry their thread's size and the time of its newest comment,
// read by joining comments directly rather than asking internal/comments for
// them afterwards. The thread is not an ornament on a task the way it is on a
// photograph — it is where the answer arrives — so "has anybody replied since the
// state last moved" has to be answerable in the same query that filters and pages
// the listing. Enriching afterwards would mean filtering after paging, which
// silently returns short pages.
package phototask

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits on the free-text fields, in characters (runes, not bytes) so they mean
// the same for a Czech task as for an English one. Each mirrors the CHECK
// constraint on its column in migration 0079.
const (
	// MaxTitleLen bounds the question itself, which is a heading and a link text.
	MaxTitleLen = 200
	// MaxBodyLen bounds the context: what was noticed and what is already known.
	MaxBodyLen = 8000
	// MaxResolutionLen bounds how a closed task says it ended.
	MaxResolutionLen = 4000
	// MaxQueryLen bounds the remembered search that produced the group.
	MaxQueryLen = 2000
	// MaxPhotos bounds one task's membership. A batch is tens of photographs;
	// this is far above that and exists so a malformed request cannot ask the
	// database to insert an unbounded list in one statement.
	MaxPhotos = 1000
)

// State says whose move it is. The three open states name who is waited on; the
// two closed ones are both real outcomes, and a task in either always says how it
// ended (see ErrClosedNeedsResolution).
type State string

const (
	// StateQuestion waits for a person to answer. This is the state a task is
	// created in, and the one that puts it in front of whoever was sent the link.
	StateQuestion State = "question"
	// StateWorking means the answer is in and the edits are being made.
	StateWorking State = "working"
	// StateReview means the work is finished and waits to be approved.
	StateReview State = "review"
	// StateDone closes a task whose question was answered and acted on.
	StateDone State = "done"
	// StateRejected closes a task decided against — the doubt was unfounded, or
	// the answer cannot be established. It is a result, not a failure, which is
	// why it demands a resolution exactly as StateDone does.
	StateRejected State = "rejected"
)

// States lists every state in workflow order. It is the source the API and the
// clients read, so a new state cannot be added here and forgotten elsewhere.
var States = []State{StateQuestion, StateWorking, StateReview, StateDone, StateRejected}

// OpenStates lists the states a task is still live in — the ones the default
// listing shows.
var OpenStates = []State{StateQuestion, StateWorking, StateReview}

// Valid reports whether s is one of the known states.
func (s State) Valid() bool {
	return slices.Contains(States, s)
}

// Closed reports whether s is one of the two states that end a task. A closed
// task carries a closing timestamp and a resolution; an open one carries neither.
func (s State) Closed() bool {
	return s == StateDone || s == StateRejected
}

// NeedsHuman reports whether the state is waiting on a person rather than on
// whoever curates the library. It is what a listing turns into "your move".
func (s State) NeedsHuman() bool {
	return s == StateQuestion
}

var (
	// ErrNotFound indicates the task does not exist.
	ErrNotFound = errors.New("phototask: task not found")
	// ErrPhotoNotFound indicates one of the photographs being added does not exist.
	ErrPhotoNotFound = errors.New("phototask: photo not found")
	// ErrEmptyTitle indicates the question was empty or only whitespace.
	ErrEmptyTitle = errors.New("phototask: title is empty")
	// ErrTooLong indicates one of the text fields exceeded its limit; the wrapped
	// message names which.
	ErrTooLong = errors.New("phototask: text is too long")
	// ErrInvalidState indicates a state outside States.
	ErrInvalidState = errors.New("phototask: unknown state")
	// ErrClosedNeedsResolution indicates an attempt to close a task without
	// saying how it ended.
	ErrClosedNeedsResolution = errors.New("phototask: a closed task needs a resolution")
	// ErrTooManyPhotos indicates a membership change beyond MaxPhotos.
	ErrTooManyPhotos = errors.New("phototask: too many photos")
)

// Task is one stored task as read back for a client: the question, the state of
// play, who opened it, and the two counts that say whether anything has happened
// — how many photographs it is about and how big its thread is.
type Task struct {
	UID string `json:"uid"`
	// Title is the question in one line, as a person would ask it.
	Title string `json:"title"`
	// Body is the context, in Markdown. A client renders it through a sanitising
	// renderer; nothing is parsed server-side.
	Body  string `json:"body"`
	State State  `json:"state"`
	// Resolution is how the task ended, empty while it is open.
	Resolution string `json:"resolution"`
	// Query is the search that produced the group, kept verbatim as evidence of
	// the rule behind it. The server never executes it.
	Query string `json:"query"`
	// CreatedByUID and CreatedByName are empty for a task whose author's account
	// has since been deleted (the row survives authorless).
	CreatedByUID  string    `json:"created_by"`
	CreatedByName string    `json:"created_by_name"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	// StateAt is when the state last moved. Compared against LastCommentAt it
	// answers the question the state alone cannot: has anybody replied since?
	StateAt      time.Time  `json:"state_at"`
	ClosedAt     *time.Time `json:"closed_at,omitempty"`
	ClosedByUID  string     `json:"closed_by,omitempty"`
	ClosedByName string     `json:"closed_by_name,omitempty"`
	PhotoCount   int        `json:"photo_count"`
	// CoverPhotoUID is the first photograph added, used as the listing's
	// thumbnail. Empty for a task with no photographs.
	CoverPhotoUID string `json:"cover_photo_uid,omitempty"`
	CommentCount  int    `json:"comment_count"`
	// LastCommentAt is when the newest live comment was written, nil for a thread
	// nobody has written in.
	LastCommentAt *time.Time `json:"last_comment_at,omitempty"`
	// HasNewAnswer reports that somebody has commented since the state last
	// moved. It is derived, and it is stamped by the store rather than left to
	// each client, because it is the whole signal an agent polls for: work that
	// has been answered and is waiting to be written into the library.
	HasNewAnswer bool `json:"has_new_answer"`
}

// Update is a partial change to a task: a nil field is left alone, a non-nil one
// is written. State carries its own consequences — closing stamps the closing
// time and the person who closed it, reopening clears both — which is why it is
// applied by applyUpdate rather than written straight into SQL.
type Update struct {
	Title      *string
	Body       *string
	State      *State
	Resolution *string
	Query      *string
}

// normalizeTitle trims a question and validates what is left: ErrEmptyTitle for
// a blank one, ErrTooLong past MaxTitleLen.
func normalizeTitle(title string) (string, error) {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return "", ErrEmptyTitle
	}
	if utf8.RuneCountInString(trimmed) > MaxTitleLen {
		return "", fmt.Errorf("%w: title over %d characters", ErrTooLong, MaxTitleLen)
	}
	return trimmed, nil
}

// normalizeText trims one of the optional text fields and checks it against
// limit, naming the field in the error. Unlike a title, empty is allowed: a task
// may have no context, no resolution and no remembered query.
func normalizeText(field, value string, limit int) (string, error) {
	trimmed := strings.TrimSpace(value)
	if utf8.RuneCountInString(trimmed) > limit {
		return "", fmt.Errorf("%w: %s over %d characters", ErrTooLong, field, limit)
	}
	return trimmed, nil
}
