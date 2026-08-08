import { type ArchivedFilter, type PhotoListParams, type PhotoSort } from '../services/photos'

import {
  ANY_PERIOD,
  isAnyPeriod,
  type Period,
  periodForYears,
  takenBeforeParam,
  toDateBound,
} from './period'

/**
 * The library's canonical route. The library *is* the homepage — the grid is the
 * app's centrepiece — so every link the app builds points here. The historical
 * `/library` route survives only as a replacing redirect for bookmarks and links
 * minted before the swap; nothing in the app should target it.
 */
export const LIBRARY_PATH = '/'

/**
 * URL-encoded view state for the library grid: every filter, the sort and the
 * archived toggle. All values are strings (the urlState convention), so the
 * whole view round-trips through the query string and Back/Forward restores it
 * exactly. An empty string means "no filter" / the default.
 */
// A type alias (not an interface) so it satisfies the urlState `Record<string,
// string>` constraint — interfaces lack the implicit index signature TS requires.
// eslint-disable-next-line @typescript-eslint/consistent-type-definitions -- see above
export type LibraryView = {
  sort: string
  archived: string
  has_gps: string
  camera: string
  q: string
  /**
   * **Legacy** capture-year facet: '' or a four-digit year. Nothing writes it any
   * more — the period control replaced the year dropdown and stores its state in
   * {@link LibraryView.taken_after}/{@link LibraryView.taken_before} — but URLs
   * and saved searches minted before that swap still carry it, so it is still
   * read: {@link periodOf} folds it into the period, and the first touch of the
   * control clears it ({@link periodPatch}).
   */
  year: string
  /**
   * Album facet: '' (any) or a comma-joined list of album UIDs, all of which a
   * photo must belong to (AND). The list rides in this single URL key — the
   * urlState layer stores every value as one string — with a comma delimiter that
   * cannot occur in a UID; use {@link parseFilterList} / {@link joinFilterList} to
   * decode and encode it. A single UID (no comma) is the plain one-album scope and
   * doubles as the detail page's album scope (see
   * {@link import('./detailView').DetailView}) — the same `album` query param
   * means the same thing everywhere.
   */
  album: string
  /**
   * Label facet: '' (any) or a comma-joined list of label UIDs, all of which a
   * photo must carry (AND). Encoded like {@link LibraryView.album}. A single UID
   * doubles as the detail page's label scope.
   */
  label: string
  /**
   * Person facet: '' (any) or a comma-joined list of subject UIDs, every one of
   * which a photo must contain (AND). Encoded like {@link LibraryView.album}; a
   * subject is on a photo when a named face/region marker links them.
   */
  person: string
  /**
   * Favorites filter: '' (any) or 'true' to keep only the current user's
   * favorites. A two-state toggle — the backend only scopes on 'true', so there is
   * no "not favorited" value — wired into the URL like every other filter.
   */
  favorite: string
  /**
   * Capture-time period, lower bound: '' (open) or an inclusive `YYYY-MM-DD` day.
   * Together with {@link LibraryView.taken_before} it is the **single** filter on
   * the time axis — a decade, a year, "before 1950" or "summer 2019" are all this
   * one pair — written by the period control and read back through
   * {@link periodOf}. Photos with no capture time never match.
   */
  taken_after: string
  /** Capture-time period, inclusive upper bound; see {@link LibraryView.taken_after}. */
  taken_before: string
  /** Minimum star rating filter: '' (any) or '1'–'5'. */
  min_rating: string
  /** Personal-marking filter: '' (any), 'pick' (👍), 'reject' (👎) or 'eye' (👁). */
  flag: string
}

/**
 * Default view: newest first, archived hidden, no filters. Declared at module
 * scope so the urlState setter keeps a stable identity, and so values equal to a
 * default are omitted from the URL (keeping it minimal and shareable).
 */
export const LIBRARY_DEFAULTS: LibraryView = {
  sort: 'newest',
  archived: 'false',
  has_gps: '',
  camera: '',
  q: '',
  year: '',
  album: '',
  label: '',
  person: '',
  favorite: '',
  taken_after: '',
  taken_before: '',
  min_rating: '',
  flag: '',
}

/**
 * Default view of a grid scoped to **one album**: the library defaults, but
 * oldest first. An album is a story, and a story is read from its beginning —
 * the backend pins an album scope to capture-time order for exactly that reason
 * and only lets the direction be chosen. Declared beside
 * {@link LIBRARY_DEFAULTS} and at module scope for the same reasons: a stable
 * identity for the urlState setter, and "oldest" left out of the URL so only a
 * deliberate switch to newest-first shows up in it — and survives Back, a reload
 * and being shared.
 */
export const ALBUM_DEFAULTS: LibraryView = {
  ...LIBRARY_DEFAULTS,
  sort: 'oldest',
}

/**
 * The two orders an album offers. Its sort key is not the reader's to pick (the
 * backend pins it to capture time), so the switch is a direction, not the
 * library's six-way selector — see {@link ALBUM_DEFAULTS}.
 */
export const ALBUM_SORTS: readonly string[] = ['oldest', 'newest']

/** Accepted sort aliases; an unknown value falls back to the default. */
const SORTS: readonly PhotoSort[] = ['newest', 'oldest', 'added', 'title', 'size', 'rating']

/** Accepted archive selectors; an unknown value falls back to hiding archived. */
const ARCHIVED: readonly ArchivedFilter[] = ['false', 'true', 'only']

/** Narrows a raw string to a known sort alias, defaulting to "newest". */
function toSort(raw: string): PhotoSort {
  return (SORTS as readonly string[]).includes(raw) ? (raw as PhotoSort) : 'newest'
}

/** Narrows a raw string to a known archive selector, defaulting to "false". */
function toArchived(raw: string): ArchivedFilter {
  return (ARCHIVED as readonly string[]).includes(raw) ? (raw as ArchivedFilter) : 'false'
}

/** A four-digit calendar year, as the legacy `year` URL key still carries one. */
const YEAR_PATTERN = /^\d{4}$/

/**
 * The capture-time period the view is filtered by — the one accessor every
 * reader of the time axis goes through, so no two of them can disagree.
 *
 * Normally that is the `taken_after`/`taken_before` pair, sanitised so a
 * hand-typed or stale URL degrades to "open" instead of a 400 from the backend.
 * When neither is set, a legacy `year=1965` (an old bookmark, a saved search
 * stored before the year dropdown became a period control) is folded in as that
 * year's period, so those views keep showing what they always showed.
 */
export function periodOf(view: LibraryView): Period {
  const period = { from: toDateBound(view.taken_after), to: toDateBound(view.taken_before) }
  if (!isAnyPeriod(period)) {
    return period
  }
  if (!YEAR_PATTERN.test(view.year)) {
    return ANY_PERIOD
  }
  const year = Number(view.year)
  return periodForYears(year, year)
}

/**
 * The view patch that puts `period` in force — including the clearing of the
 * legacy `year` key, so the two can never both be set and contradict each other.
 * Passing {@link ANY_PERIOD} clears the filter.
 */
export function periodPatch(period: Period): Partial<LibraryView> {
  return { taken_after: period.from, taken_before: period.to, year: '' }
}

/**
 * The delimiter joining several album/label UIDs inside a single URL key. A comma
 * cannot appear in a UID, so it round-trips the multi-selection through the
 * `Record<string, string>` urlState layer without a dedicated key per value.
 */
export const FILTER_LIST_DELIMITER = ','

/**
 * Decodes a comma-joined filter list (e.g. an `album`/`label` view value) into its
 * UIDs, dropping empty segments so `''` yields `[]` and a trailing comma is
 * ignored. The order is preserved, matching the order the chips are shown in.
 */
export function parseFilterList(raw: string): string[] {
  return raw.split(FILTER_LIST_DELIMITER).filter((uid) => uid !== '')
}

/** Encodes a list of UIDs back into the comma-joined form stored in the URL. */
export function joinFilterList(uids: string[]): string {
  return uids.join(FILTER_LIST_DELIMITER)
}

/**
 * Returns the filter list with `uid` appended, unless it is empty or already
 * present (selecting the same album/label twice is a no-op). Used by the facet
 * controls, which add to the current selection rather than replacing it.
 */
export function addToFilterList(raw: string, uid: string): string {
  if (uid === '') {
    return raw
  }
  const uids = parseFilterList(raw)
  if (uids.includes(uid)) {
    return raw
  }
  return joinFilterList([...uids, uid])
}

/**
 * Returns the filter list with `uid` removed, leaving the rest in order. Removing
 * the last UID yields `''`, which clears the facet.
 */
export function removeFromFilterList(raw: string, uid: string): string {
  return joinFilterList(parseFilterList(raw).filter((current) => current !== uid))
}

/**
 * Maps the URL view state to API list params, sanitising the enum-like fields so
 * a tampered URL cannot send an out-of-range sort/archived value to the backend.
 * Free-text, tri-state and UID filters pass through verbatim: the album, label
 * and person values stay in their comma-joined form and are split into repeated
 * query params by {@link import('../services/photos').buildPhotoQuery}. The
 * backend treats an empty value as no filter, and an unknown album/label/person
 * UID simply matches nothing.
 *
 * The time axis goes through {@link periodOf}, so the legacy `year` key and the
 * date bounds arrive as one period, with its upper bound stretched to the end of
 * its last day ({@link takenBeforeParam}) — a period is inclusive of the day the
 * reader picked.
 */
export function viewToParams(view: LibraryView): PhotoListParams {
  const period = periodOf(view)
  return {
    sort: toSort(view.sort),
    archived: toArchived(view.archived),
    has_gps: view.has_gps,
    camera: view.camera,
    q: view.q,
    album: view.album,
    label: view.label,
    person: view.person,
    favorite: view.favorite,
    taken_after: period.from,
    taken_before: takenBeforeParam(period.to),
    min_rating: view.min_rating,
    flag: view.flag,
  }
}

/**
 * Reports whether any filter (excluding sort) differs from its default. Pass
 * `ignoreQuery` on the search page, where `q` is the page's own search query
 * rather than a filter this bar should offer to clear.
 */
export function hasActiveFilters(
  view: LibraryView,
  options: { ignoreQuery?: boolean } = {},
): boolean {
  return (
    view.archived !== LIBRARY_DEFAULTS.archived ||
    view.has_gps !== '' ||
    view.camera !== '' ||
    (!options.ignoreQuery && view.q !== '') ||
    !isAnyPeriod(periodOf(view)) ||
    view.album !== '' ||
    view.label !== '' ||
    view.person !== '' ||
    view.favorite !== '' ||
    view.min_rating !== '' ||
    view.flag !== ''
  )
}
