package organize

import (
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	// albumFallbackSlug is used when an album title slugifies to the empty string
	// (e.g. a title made entirely of punctuation or non-Latin script).
	albumFallbackSlug = "album"
	// labelFallbackSlug is the equivalent fallback for label names.
	labelFallbackSlug = "label"
	// maxSlugAttempts bounds how many numeric suffixes the store tries when making a
	// slug unique. The cap is far above any realistic number of name collisions; it
	// exists only so a pathological loop terminates.
	maxSlugAttempts = 1000
)

// slugify converts a display name into a URL-safe base slug: diacritics are
// stripped, the result is lower-cased, and every run of characters that is not an
// ASCII letter or digit collapses to a single hyphen, with leading and trailing
// hyphens trimmed. A name that fully strips away yields the given fallback, so
// every row still has a non-empty base slug to disambiguate.
//
// Diacritics are removed by NFD-decomposing each character (č → c + combining
// caron) and then keeping only ASCII alphanumerics, which drops the combining
// marks. norm.NFD.String holds no shared state, so slugify is safe for concurrent
// use.
func slugify(name, fallback string) string {
	slug := collapseToSlug(strings.ToLower(norm.NFD.String(name)))
	if slug == "" {
		return fallback
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

// insertWithUniqueSlug calls write with successive candidate slugs (base, base-2,
// base-3, …) until a write avoids a slug unique-constraint violation, returning
// that write's result. Any non-slug error aborts immediately; ErrSlugExhausted is
// returned if every attempt collides.
func insertWithUniqueSlug[T any](base string, write func(slug string) (T, error)) (T, error) {
	var zero T
	for attempt := range maxSlugAttempts {
		out, err := write(candidateSlug(base, attempt))
		if name, ok := isUniqueViolation(err); ok && strings.Contains(name, "slug") {
			continue
		}
		if err != nil {
			return zero, err
		}
		return out, nil
	}
	return zero, ErrSlugExhausted
}
