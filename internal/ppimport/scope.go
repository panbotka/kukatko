package ppimport

import (
	"fmt"
	"strconv"
	"strings"
)

// Year bounds a --year filter is allowed to name. The lower bound is the year of
// the oldest surviving photograph; anything outside the range is a typo, not a
// selection, and would silently import nothing.
const (
	minScopeYear = 1826
	maxScopeYear = 9999
)

// Scope narrows a partial import run to a subset of the source catalogue. The
// zero Scope selects everything — that is the full incremental run (Import).
//
// The filters combine: several set at once narrow the run together (PhotoPrism
// ANDs the space-separated terms of a q= expression, and the album filter is a
// separate s= parameter), so --album X --year 1985 imports the photos of album X
// taken in 1985.
//
// Every non-zero Scope makes the run partial, which is why ImportScoped never
// records a watermark: a scoped run sees a slice of the library only.
type Scope struct {
	// AlbumUID, when non-empty, selects the photos of one PhotoPrism album (it is
	// passed as the s= album filter, not as a q= term).
	AlbumUID string
	// Label, when non-empty, selects the photos carrying that PhotoPrism label,
	// named by its slug (for example "sdh").
	Label string
	// Person, when non-empty, selects the photos a named subject appears on (the
	// subject's full name, for example "Aleš Kozák").
	Person string
	// Year, when non-zero, selects the photos taken in that calendar year.
	Year int
}

// normalized returns the scope with its string filters trimmed, so a flag passed
// as " sdh " scopes the same run as "sdh".
func (sc Scope) normalized() Scope {
	sc.AlbumUID = strings.TrimSpace(sc.AlbumUID)
	sc.Label = strings.TrimSpace(sc.Label)
	sc.Person = strings.TrimSpace(sc.Person)
	return sc
}

// IsEmpty reports whether the scope names no filter at all, i.e. selects the
// whole source catalogue.
func (sc Scope) IsEmpty() bool {
	return sc.AlbumUID == "" && sc.Label == "" && sc.Person == "" && sc.Year == 0
}

// validate rejects a scope that cannot select anything meaningful: ErrEmptyScope
// when no filter is set (the caller wants Import instead), and ErrInvalidYear
// when the year is outside the plausible range — which would import nothing
// while reporting a clean, empty run.
func (sc Scope) validate() error {
	if sc.IsEmpty() {
		return ErrEmptyScope
	}
	if sc.Year != 0 && (sc.Year < minScopeYear || sc.Year > maxScopeYear) {
		return fmt.Errorf("%w: %d (want %d–%d)", ErrInvalidYear, sc.Year, minScopeYear, maxScopeYear)
	}
	return nil
}

// Query renders the non-album filters as a PhotoPrism photo-search expression
// for the q= parameter: label:"<slug>", person:"<name>" and year:<YYYY>, joined
// by spaces (PhotoPrism ANDs them). Values are quoted, since a person's name
// contains spaces. It returns "" when the scope names no q= filter (an empty or
// album-only scope), which leaves the listing's incremental watermark filter in
// charge.
func (sc Scope) Query() string {
	terms := make([]string, 0, 3)
	if sc.Label != "" {
		terms = append(terms, fmt.Sprintf("label:%q", sc.Label))
	}
	if sc.Person != "" {
		terms = append(terms, fmt.Sprintf("person:%q", sc.Person))
	}
	if sc.Year != 0 {
		terms = append(terms, "year:"+strconv.Itoa(sc.Year))
	}
	return strings.Join(terms, " ")
}

// String renders the scope for logs and CLI output, listing the filters that are
// set ("album=ppal1 year=1985") or "full" for the zero scope.
func (sc Scope) String() string {
	parts := make([]string, 0, 4)
	if sc.AlbumUID != "" {
		parts = append(parts, "album="+sc.AlbumUID)
	}
	if sc.Label != "" {
		parts = append(parts, "label="+sc.Label)
	}
	if sc.Person != "" {
		parts = append(parts, "person="+strconv.Quote(sc.Person))
	}
	if sc.Year != 0 {
		parts = append(parts, "year="+strconv.Itoa(sc.Year))
	}
	if len(parts) == 0 {
		return "full"
	}
	return strings.Join(parts, " ")
}
