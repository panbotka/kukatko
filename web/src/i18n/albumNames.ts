/**
 * Display names for machine-made album titles.
 *
 * The library carries hundreds of albums PhotoPrism generated in English — month
 * folders like `January 2026` and place albums named after a country — next to
 * the albums somebody actually created and named in Czech. Renaming them in the
 * database is out of the question (the titles are data, and the importers that
 * wrote them are gone), so the Czech UI translates them **at display time only**
 * through the map below and the country dictionary in `countryNames`.
 *
 * The rule is deliberately strict: a title is rewritten only when it matches a
 * known pattern *exactly* — a leading English month optionally followed by a
 * four-digit year, or a whole segment of the title being a known English country
 * name (optionally with a trailing year: `Czech Republic 2026`). Anything else is
 * shown verbatim, so a hand-written album name can never be mangled by a partial
 * match.
 */

import { countryDisplayName, isCzech, localizeCountryNames } from './countryNames'

/** English month names as PhotoPrism writes them, index 0 = January. */
const EN_MONTHS = [
  'january',
  'february',
  'march',
  'april',
  'may',
  'june',
  'july',
  'august',
  'september',
  'october',
  'november',
  'december',
] as const

/**
 * Czech month names in the nominative, lower-cased the way Czech writes a month
 * beside a year (`leden 2026`, not `Leden 2026`).
 */
const CS_MONTHS = [
  'leden',
  'únor',
  'březen',
  'duben',
  'květen',
  'červen',
  'červenec',
  'srpen',
  'září',
  'říjen',
  'listopad',
  'prosinec',
] as const

/** The year a month album is named after: exactly four digits, nothing else. */
const YEAR_RE = /^\d{4}$/

/**
 * The Czech reading of a month album's title — a month name on its own, or a
 * month name followed by a four-digit year, and nothing else, so `May Day` and
 * `January in Norway` stay as they are. Undefined when it is not one.
 */
function monthTitle(trimmed: string): string | undefined {
  const words = trimmed.split(/\s+/)
  if (words.length > 2) {
    return undefined
  }
  const index = (EN_MONTHS as readonly string[]).indexOf(words[0].toLowerCase())
  if (index < 0) {
    return undefined
  }
  const month = CS_MONTHS[index]
  if (words.length === 1) {
    return month
  }
  return YEAR_RE.test(words[1]) ? `${month} ${words[1]}` : undefined
}

/**
 * Translates a machine-made English album title for display, returning `title`
 * unchanged whenever it does not match a known pattern exactly — and always
 * when the UI language is not Czech (English is the language those titles are
 * already in).
 *
 * The last thing tried is the country pass over the title's segments, which is
 * what turns the place albums the importers composed out of a country and a year
 * (`Czech Republic 2026`) into Czech without touching a name someone wrote
 * themselves.
 *
 * @example
 *   albumDisplayTitle('January 2026', 'cs')      // 'leden 2026'
 *   albumDisplayTitle('Czechia', 'cs')           // 'Česko'
 *   albumDisplayTitle('Czech Republic 2026','cs')// 'Česko 2026'
 *   albumDisplayTitle('Dovolená 2019', 'cs')     // 'Dovolená 2019' (untouched)
 */
export function albumDisplayTitle(title: string, language: string): string {
  if (!isCzech(language)) {
    return title
  }
  const trimmed = title.trim()
  if (trimmed === '') {
    return title
  }

  const country = countryDisplayName(trimmed, language)
  if (country !== undefined) {
    return country
  }
  return monthTitle(trimmed) ?? localizeCountryNames(title, language)
}
