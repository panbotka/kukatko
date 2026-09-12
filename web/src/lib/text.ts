/**
 * Case- and accent-insensitive text helpers for client-side filtering
 * (e.g. the album/label autocomplete). Mirrors the backend's
 * `immutable_unaccent` so a query like `namesti` matches `Náměstí`.
 */

/** Combining diacritical marks (Unicode block U+0300–U+036F). */
const COMBINING_MARKS = /[̀-ͯ]/g

/**
 * Folds a string to a case- and accent-insensitive form: lower-cased,
 * whitespace-trimmed and with combining diacritical marks stripped via NFD
 * decomposition. Returns an empty string for a blank input.
 */
export function foldText(value: string): string {
  return value.normalize('NFD').replace(COMBINING_MARKS, '').toLowerCase().trim()
}

/**
 * Reports whether `haystack` contains `needle` after both are folded
 * ({@link foldText}). An empty (or whitespace-only) needle matches everything.
 */
export function foldedIncludes(haystack: string, needle: string): boolean {
  const q = foldText(needle)
  if (q === '') {
    return true
  }
  return foldText(haystack).includes(q)
}

/**
 * Reports whether two strings are the same name after folding
 * ({@link foldText}), so `Dovolená` and `dovolena` are one and the same label.
 */
export function foldedEquals(a: string, b: string): boolean {
  return foldText(a) === foldText(b)
}

/**
 * Shortens a string to at most `max` characters, ending it in an ellipsis where
 * anything was cut. Counted in user-perceived characters, not code units, so a
 * name written with combining diacritics is not cut in half mid-letter.
 *
 * It is for drawing into a box of a fixed width — an SVG `<text>`, chiefly,
 * which unlike HTML has no `text-overflow: ellipsis` to fall back on. The full
 * string belongs in a `title` beside it: this cuts the label, never the data.
 */
export function truncateText(value: string, max: number): string {
  if (max <= 0) {
    return ''
  }
  const letters = [...new Intl.Segmenter().segment(value)].map((part) => part.segment)
  if (letters.length <= max) {
    return value
  }
  return `${letters
    .slice(0, max - 1)
    .join('')
    .trimEnd()}…`
}
