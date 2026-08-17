package photoapi

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/personme"
	"github.com/panbotka/kukatko/internal/photos"
)

// noticePersonMeUnlinked is the machine-readable reason a listing came back
// empty: the caller asked for `person:me` without their account having said
// which person that is. The client turns the code into a sentence (and into an
// offer to go and set it); the server states the fact, which is the same bargain
// every other code in this API makes.
const noticePersonMeUnlinked = "person_me_unlinked"

// applyMeTokens resolves the query language's two caller-dependent values
// against user — `person:me` against their linked subject and `uploader:me`
// against their own account (see internal/personme) — and returns the notices
// the response should carry.
//
// The two live in one function because they must never be resolved apart: every
// handler that parses the query language calls this, so both tokens mean the
// same thing on the grid, the search, the timeline and the facets, and adding a
// handler cannot half-resolve them.
//
// When the caller has no linked person the request cannot be satisfied, so
// params.MatchNone is set — the page comes back empty rather than silently
// widening to the whole library — and the returned notice says why. `uploader:me`
// has no such failure: it names the account that authenticated the request.
func applyMeTokens(params *photos.ListParams, user auth.User) []string {
	personme.ResolveUploader(params.QueryFilters, user.UID)
	used, resolved := personme.Resolve(params.QueryFilters, user.SubjectUID)
	if !used || resolved {
		return nil
	}
	params.MatchNone = true
	return []string{noticePersonMeUnlinked}
}
