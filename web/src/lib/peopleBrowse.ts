import { type SubjectCount, type SubjectType } from '../services/people'

import { foldedIncludes } from './text'

/**
 * Browsing the people index: the pure filter/sort the page lays over the one
 * list the API returns.
 *
 * The API hands over every subject at once, alphabetically, and the page used to
 * draw all of them and nothing else — on a phone that is some fifty rows of
 * strangers ordered by first name, and "find grandma" means remembering her name
 * and walking the alphabet. The rules live here as pure functions so the
 * ordering, the search and the counts are testable without a DOM, exactly as
 * `lib/albumBrowse` does for the album index.
 */

/** The kinds of subject the type filter offers, in the order it lists them. */
export const PEOPLE_TABS = ['all', 'person', 'pet', 'other'] as const

/** One choice of the type filter: every subject, or one {@link SubjectType}. */
export type PeopleTab = (typeof PEOPLE_TABS)[number]

/**
 * The type shown when the URL asks for none: everybody.
 *
 * Unlike the album index, which opens on hand-made albums because the
 * machine-made ones outnumber them, no kind of subject here is noise — a page
 * called "People" that silently hid the dog would be hiding content, not clutter.
 */
export const PEOPLE_TAB_DEFAULT: PeopleTab = 'all'

/** The orderings the sort selector offers, in the order it lists them. */
export const PEOPLE_SORTS = ['count', 'name', 'recent'] as const

/** One ordering of the people index. */
export type PeopleSort = (typeof PEOPLE_SORTS)[number]

/**
 * The ordering used when the URL asks for none: the people with the most photos
 * first.
 *
 * The API answers alphabetically and the page used to keep that order, which for
 * a real library opens on a wall of a hundred strangers sorted by first name. The
 * people somebody actually browses to are the ones the archive is full of, so
 * they lead; the alphabet is one selection away for whoever came looking for a
 * particular name — and the search above it is the faster answer to that anyway.
 */
export const PEOPLE_SORT_DEFAULT: PeopleSort = 'count'

/** Narrows a raw URL value to a type filter, defaulting when it is not one. */
export function toPeopleTab(value: string): PeopleTab {
  return (PEOPLE_TABS as readonly string[]).includes(value)
    ? (value as PeopleTab)
    : PEOPLE_TAB_DEFAULT
}

/** Narrows a raw URL value to an ordering, defaulting when it is not one. */
export function toPeopleSort(value: string): PeopleSort {
  return (PEOPLE_SORTS as readonly string[]).includes(value)
    ? (value as PeopleSort)
    : PEOPLE_SORT_DEFAULT
}

/**
 * URL-encoded view state of the people index: the name search, the kind of
 * subject and the ordering. All values are strings (the urlState convention), so
 * the whole view round-trips through the query string.
 */
// A type alias (not an interface) so it satisfies the urlState `Record<string,
// string>` constraint — interfaces lack the implicit index signature TS requires.
// eslint-disable-next-line @typescript-eslint/consistent-type-definitions -- see above
export type PeopleView = {
  q: string
  type: string
  sort: string
}

/** The people index as it opens: everyone, unfiltered, in alphabetical order. */
export const PEOPLE_DEFAULTS: PeopleView = {
  q: '',
  type: PEOPLE_TAB_DEFAULT,
  sort: PEOPLE_SORT_DEFAULT,
}

/** What the people index is currently showing, decoded from the URL. */
export interface PeopleBrowseOptions {
  /** Free-text filter over names (folded: case- and accent-insensitive). */
  query: string
  /** The selected kind of subject. */
  tab: PeopleTab
  /** The selected ordering. */
  sort: PeopleSort
  /** Active UI language, deciding how names collate. */
  language: string
}

/** Decodes the URL view into the options {@link browsePeople} takes. */
export function peopleBrowseOptions(view: PeopleView, language: string): PeopleBrowseOptions {
  return {
    query: view.q,
    tab: toPeopleTab(view.type),
    sort: toPeopleSort(view.sort),
    language,
  }
}

/** The people index after filtering: what to render, and what each type holds. */
export interface PeopleBrowseResult {
  /** The subjects of the selected type, in the selected order. */
  visible: SubjectCount[]
  /** How many subjects each type holds under the current search. */
  counts: Record<PeopleTab, number>
  /** How many subjects the search dropped, across all types. */
  filteredOut: number
}

/**
 * Compares two subjects by name in the reader's own language: numeric collation
 * so `Anna 2` precedes `Anna 10`, base sensitivity so case and diacritics do not
 * split the alphabet.
 *
 * The API already orders by name, but in the database's collation — the page
 * re-sorts so a Czech reader gets a Czech alphabet, and so the tie-break under
 * "most photos" is the same order the alphabetical view uses.
 */
function compareByName(a: SubjectCount, b: SubjectCount, language: string): number {
  return a.name.localeCompare(b.name, language, { numeric: true, sensitivity: 'base' })
}

/**
 * Applies the search, the type filter and the ordering to the whole subject
 * list, and counts what each type holds under the same search — so the filter's
 * options answer "where are my matches?" rather than restating the library's
 * totals.
 *
 * `count` orders by `photo_count`, the figure the tile's caption shows and the
 * one the subject's gallery honours; `recent` by `created_at`, which is when the
 * person was added to the library rather than when their photos were taken —
 * "who did I name last week" is a question about the naming. Both fall back to
 * the name on a tie, so the grid never reshuffles itself between renders.
 */
export function browsePeople(
  subjects: SubjectCount[],
  { query, tab, sort, language }: PeopleBrowseOptions,
): PeopleBrowseResult {
  const pool = subjects.filter((subject) => foldedIncludes(subject.name, query))

  const counts: Record<PeopleTab, number> = { all: pool.length, person: 0, pet: 0, other: 0 }
  for (const subject of pool) {
    // A type this frontend does not know yet still counts as somebody: it lands
    // in `other` rather than falling out of every option.
    counts[knownType(subject.type)] += 1
  }

  const visible = pool.filter((subject) => tab === 'all' || knownType(subject.type) === tab)
  visible.sort(comparator(sort, language))

  return { visible, counts, filteredOut: subjects.length - pool.length }
}

/**
 * The comparison one ordering sorts by, with the name as every tie-break: two
 * people with the same number of photos (or added in the same second, as a bulk
 * naming session produces) must not swap places between renders.
 */
function comparator(
  sort: PeopleSort,
  language: string,
): (a: SubjectCount, b: SubjectCount) => number {
  switch (sort) {
    case 'count':
      return (a, b) => b.photo_count - a.photo_count || compareByName(a, b, language)
    case 'recent':
      return (a, b) => addedAt(b) - addedAt(a) || compareByName(a, b, language)
    default:
      return (a, b) => compareByName(a, b, language)
  }
}

/**
 * When a subject was added, as a number to sort by. A missing or unparseable
 * timestamp sorts oldest rather than throwing the whole list into `NaN`
 * comparisons, which in JavaScript would leave the order arbitrary.
 */
function addedAt(subject: SubjectCount): number {
  const at = Date.parse(subject.created_at)
  return Number.isNaN(at) ? 0 : at
}

/** Maps a subject's type onto a filter option, folding unknown types into `other`. */
function knownType(type: SubjectType): Exclude<PeopleTab, 'all'> {
  switch (type) {
    case 'person':
      return 'person'
    case 'pet':
      return 'pet'
    default:
      return 'other'
  }
}
