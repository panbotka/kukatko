package people

import (
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// fallbackSlug is used when a name slugifies to the empty string (e.g. a name
// made entirely of punctuation or non-Latin script), so every subject still has a
// non-empty base slug to disambiguate.
const fallbackSlug = "subject"

// Slugify converts a subject name into a URL-safe base slug: diacritics are
// stripped, the result is lower-cased, and every run of characters that is not an
// ASCII letter or digit collapses to a single hyphen, with leading and trailing
// hyphens trimmed. An empty or fully stripped name yields fallbackSlug. The
// result is a *base* slug; the store appends a numeric suffix to make it unique.
//
// Diacritics are removed by NFD-decomposing each character (č → c + combining
// caron) and then keeping only ASCII alphanumerics, which drops the combining
// marks. norm.NFD.String holds no shared state, so Slugify is safe for concurrent
// use.
func Slugify(name string) string {
	slug := collapseToSlug(strings.ToLower(norm.NFD.String(name)))
	if slug == "" {
		return fallbackSlug
	}
	return slug
}

// collapseToSlug keeps the ASCII alphanumerics of s, replaces every run of other
// characters with a single hyphen, and drops NFD combining marks silently so an
// accent does not split a word. Leading and trailing hyphens are never emitted.
func collapseToSlug(s string) string {
	var b strings.Builder
	pendingHyphen := false
	for _, r := range s {
		switch {
		case isSlugChar(r):
			if pendingHyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingHyphen = false
			b.WriteRune(r)
		case unicode.Is(unicode.Mn, r):
			// Combining mark left by NFD decomposition (e.g. the caron of "č" whose
			// base "c" was already written): drop it without starting a new word.
		default:
			pendingHyphen = true
		}
	}
	return b.String()
}

// isSlugChar reports whether r is an ASCII lowercase letter or digit, the only
// characters kept verbatim in a slug.
func isSlugChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

// candidateSlug returns the slug to try on a given zero-based attempt: the base
// slug on the first attempt, then base-2, base-3, … as the numeric suffix grows,
// matching the convention users expect from de-duplicated slugs.
func candidateSlug(base string, attempt int) string {
	if attempt == 0 {
		return base
	}
	return base + "-" + strconv.Itoa(attempt+1)
}
