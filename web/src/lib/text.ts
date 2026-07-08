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
