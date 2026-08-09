// Package searchhistory is the database access layer for each user's recent
// search queries: the short, ordered list of what a user actually searched for,
// so the search box can hand a query straight back instead of making it be typed
// again.
//
// The history is stored server-side rather than in the browser precisely so it
// survives the device: a query composed on a laptop is offered on the phone. It
// is strictly per-user — every statement in this package is scoped to one
// user_uid and nothing reads across accounts.
//
// Queries are opaque plain strings here. This package trims and caps them
// ([Normalize]) and nothing else: `internal/query` remains the only thing that
// reads meaning into a query, and the history's job is to return the exact text
// that was run.
package searchhistory

import (
	"errors"
	"strings"
	"time"
	"unicode"
)

const (
	// MaxEntries is how many recent searches one user's history keeps. Every
	// record prunes back to this many, so the table is bounded by the number of
	// accounts rather than by how much anyone searches.
	MaxEntries = 20

	// MaxQueryLength is the longest query, in characters, the history stores.
	// Longer queries are truncated rather than rejected — a query that outgrew
	// the limit is still worth offering back — and it matches the CHECK on the
	// search_history.query column (migration 0056_search_history.sql).
	MaxQueryLength = 500
)

// ErrEmptyQuery is returned when a query holds nothing but whitespace, so there
// is no search to remember.
var ErrEmptyQuery = errors.New("searchhistory: query is empty")

// Entry is one remembered search: the query exactly as it was run, and when it
// was last run. Re-running a query moves SearchedAt forward instead of adding a
// second entry.
type Entry struct {
	// Query is the search text, verbatim (trimmed and length-capped).
	Query string `json:"query"`
	// SearchedAt is when the query was most recently run.
	SearchedAt time.Time `json:"searched_at"`
}

// Normalize prepares a query for storage: it trims surrounding whitespace and
// caps the result at [MaxQueryLength] characters, trimming any whitespace the
// cut left at the end. It returns an empty string for a query that holds nothing
// but whitespace, which callers treat as "nothing to remember".
//
// It deliberately does not fold case, strip diacritics or collapse whitespace
// inside the query: the stored string is handed back to the search box verbatim,
// and collapsing runs of spaces would change the meaning of a quoted value such
// as `title:"a  b"`.
func Normalize(query string) string {
	trimmed := strings.TrimSpace(query)
	if len(trimmed) <= MaxQueryLength {
		// Fast path: a string shorter than the limit in bytes is also shorter in
		// characters, so there is nothing to count.
		return trimmed
	}
	runes := []rune(trimmed)
	if len(runes) <= MaxQueryLength {
		return trimmed
	}
	return strings.TrimRightFunc(string(runes[:MaxQueryLength]), unicode.IsSpace)
}
