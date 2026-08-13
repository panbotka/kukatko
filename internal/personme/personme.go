// Package personme resolves the one word of the search query language that
// means something different to every caller: `person:me`.
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
// names the caller rather than naming somebody.
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
