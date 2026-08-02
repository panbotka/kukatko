package people

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// fallbackSlug is the last-resort base slug for a name that identifies nobody
// (empty, whitespace or punctuation only), so slug generation stays total and
// every stored subject has a non-empty base slug. Nothing routine reaches it any
// more: the subject API rejects such a name and every find-or-create-by-name path
// keys on NameSlug, which reports "" for exactly these names.
const fallbackSlug = "subject"

// digestSlugLen is how many hex characters of a name's digest a digest slug
// carries. 8 hex characters (32 bits) make an accidental collision between two
// names in a library of a few hundred subjects negligible, and the store's
// numeric suffix resolves one anyway.
const digestSlugLen = 8

// NameSlug returns the key a subject name is matched on, or "" when the name
// identifies nobody at all — it is empty, whitespace, or punctuation only.
//
// **Every find-or-create-by-name path must key on this, never on Slugify.**
// Slugify substitutes a constant for a name it cannot slugify, and a constant key
// is a catch-all: the first nameless face creates one empty-named subject and
// every nameless face after it is found by that same key and assigned to it. That
// is not hypothetical — it collapsed 16 532 markers onto a single empty-named
// subject in production. See docs/OPERATIONS.md § "Nameless catch-all subject".
//
// A name written entirely outside ASCII (CJK, Cyrillic, Greek, …) is still a
// name, so it does not yield "": it slugifies to a digest of the name, which is
// stable for that name and distinct from every other name's. Only a name with no
// letter and no digit anywhere is treated as no name.
func NameSlug(name string) string {
	if slug := collapseToSlug(strings.ToLower(norm.NFD.String(name))); slug != "" {
		return slug
	}
	if !hasLetterOrDigit(name) {
		return ""
	}
	return fallbackSlug + "-" + nameDigest(name)
}

// Slugify converts a subject name into a URL-safe base slug: diacritics are
// stripped, the result is lower-cased, and every run of characters that is not an
// ASCII letter or digit collapses to a single hyphen, with leading and trailing
// hyphens trimmed. The result is a *base* slug; the store appends a numeric
// suffix to make it unique.
//
// It is total — it never returns "" — because a stored subject needs a non-empty
// slug. That makes it right for slug *generation* and wrong for *matching*: use
// NameSlug to decide whether a name identifies a subject at all.
//
// Diacritics are removed by NFD-decomposing each character (č → c + combining
// caron) and then keeping only ASCII alphanumerics, which drops the combining
// marks. norm.NFD.String holds no shared state, so Slugify is safe for concurrent
// use.
func Slugify(name string) string {
	if slug := NameSlug(name); slug != "" {
		return slug
	}
	return fallbackSlug
}

// hasLetterOrDigit reports whether s carries at least one letter or digit in any
// script. It is what separates a real name no ASCII slug can represent from a
// string that names nobody.
func hasLetterOrDigit(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// nameDigest returns the short hex digest standing in for a name that carries
// letters or digits but no ASCII ones. It is taken over the lower-cased,
// NFC-normalised name so the same name always yields the same slug (making
// find-or-create idempotent across runs) while two different names practically
// never share one.
func nameDigest(name string) string {
	sum := sha256.Sum256([]byte(norm.NFC.String(strings.ToLower(name))))
	return hex.EncodeToString(sum[:])[:digestSlugLen]
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
