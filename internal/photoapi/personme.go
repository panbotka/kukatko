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

// applyPersonMe resolves the query language's `person:me` against user's linked
// subject (see internal/personme) and returns the notices the response should
// carry.
//
// When the caller has no linked person the request cannot be satisfied, so
// params.MatchNone is set — the page comes back empty rather than silently
// widening to the whole library — and the returned notice says why. Every
// handler that parses the query language calls this, so the token means the same
// thing on the grid, the search, the timeline and the year facet.
func applyPersonMe(params *photos.ListParams, user auth.User) []string {
	used, resolved := personme.Resolve(params.QueryFilters, user.SubjectUID)
	if !used || resolved {
		return nil
	}
	params.MatchNone = true
	return []string{noticePersonMeUnlinked}
}
