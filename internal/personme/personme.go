// Package personme resolves the words of the search query language that mean
// something different to every caller: `person:me` and `uploader:me`.
//
// # Why it is not in internal/query
//
// internal/query is by contract a pure parser. It turns a string into an AST and
// knows nothing about who is asking — no session, no database, no account — and
// that is exactly what makes the language testable and what keeps a filter's
// meaning independent of the request that carried it. So the token survives
// parsing as an ordinary text value, and every surface that knows its caller
// (the HTTP photo API, the MCP search tool) hands the parsed filters through
// here before compiling them to SQL.
//
// # The collision with a person actually called "me"
//
// The token wins, but only in its exact lower-case spelling. Every other
// spelling — `person:Me`, `person:ME` — stays an ordinary name match, and since
// name matching is case-insensitive it still finds a subject named "me". A
// subject is also always reachable by its UID (`person:sub…`), which no name can
// shadow. So: `person:me` is the caller, `person:Me` is the person, and the
// loser of the collision keeps two ways home. See docs/API.md.
//
// # What an unlinked caller gets
//
// Nothing — deliberately. Resolve reports that the token was used and could not
// be resolved, and the caller turns that into an empty answer with a reason. The
// two things it must never become are "everything" (dropping the filter) and a
// free-text search for the word "me": both answer a question nobody asked.
package personme

import "github.com/panbotka/kukatko/internal/query"

// Token is the value of the person: filter (and of its subject: alias) that
// names the caller rather than naming somebody. The uploader: filter reserves
// the same word for the same reason — see ResolveUploader.
const Token = "me"

// Resolve rewrites every `person:me` alternative in filters to the subject
// linked to the caller's account, in place.
//
// linked is the caller's linked subject UID, or nil when the account has not
// said which person it is. used reports whether the token appeared at all;
// resolved is false only when it did and linked was nil, which is the case the
// caller has to answer with an empty result and a reason.
//
// The rewritten alternative carries the subject UID, which is what the photos
// store compiles into an exact match — a UID cannot collide with a person's
// name. A negated alternative (`person:!me`) is rewritten the same way, so
// "everything I am not on" works for a linked caller; for an unlinked one it is
// just as unresolvable, because the app cannot say which photos are not of a
// person it cannot name.
func Resolve(filters []query.Filter, linked *string) (used, resolved bool) {
	for i := range filters {
		if filters[i].Key != query.KeyPerson {
			continue
		}
		values := filters[i].Values
		for j := range values {
			if values[j].Text != Token {
				continue
			}
			used = true
			if linked == nil {
				continue
			}
			values[j].Text = *linked
			values[j].Pattern = *linked
		}
	}
	return used, !used || linked != nil
}

// ResolveUploader rewrites every `uploader:me` alternative in filters to
// callerUID — the account making the request — in place, so the photos store
// compiles it into an exact match on the uploader's UID, which no username can
// shadow. A negated alternative (`uploader:!me`) is rewritten the same way, so
// "everything that is not mine" works too — including, as the compiled negation
// implies, the photos nobody uploaded at all.
//
// It has nothing to report and cannot fail, which is what makes it the simpler
// twin of Resolve: `me` here is the account that authenticated the request, and
// every surface that resolves the token has that account in hand — where
// person:me depends on a link the account may never have made. An empty
// callerUID (no such surface exists today) is therefore left alone rather than
// rewritten: an empty UID would compile to a name pattern matching every
// uploader, and answering "everyone" to "me" is the one answer that is never
// right.
func ResolveUploader(filters []query.Filter, callerUID string) {
	if callerUID == "" {
		return
	}
	for i := range filters {
		if filters[i].Key != query.KeyUploader {
			continue
		}
		values := filters[i].Values
		for j := range values {
			if values[j].Text != Token {
				continue
			}
			values[j].Text = callerUID
			values[j].Pattern = callerUID
		}
	}
}
