/** Constants of the search page's start state (`SearchStartState`). */

/**
 * How many recent searches the start state lists. The ring keeps twenty, but a
 * page that is only waiting for a query wants the handful the reader is likely
 * to repeat, not the whole archive of what they once typed.
 */
export const START_RECENT_LIMIT = 10

/**
 * The example queries offered as a first step: one per filter family a family
 * archive is searched by most — a year, a person, an album, a label, the text.
 * They are query-language literals, not UI copy, so they are not translated: the
 * library's names are Czech whatever language the interface speaks.
 */
export const SEARCH_EXAMPLES: readonly string[] = [
  'year:1965',
  'person:Jarmila',
  'album:"Léto 2024"',
  'label:kostel',
  'text:veselice',
]
