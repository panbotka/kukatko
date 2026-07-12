import { type ArchivedFilter, type PhotoListParams, type PhotoSort } from '../services/photos'

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
  private: string
  camera: string
  q: string
  /**
   * Capture-year facet: '' (any) or a four-digit year, one of those
   * `GET /photos/years` offers. Photos with no capture time never match.
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
  taken_after: string
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
  private: '',
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

/** A four-digit calendar year — the only year value the backend accepts. */
const YEAR_PATTERN = /^\d{4}$/

/**
 * Narrows a raw string to a four-digit year, dropping anything else (a hand-typed
 * or stale URL) to "no filter" rather than letting the backend answer 400 and the
 * grid render an error.
 */
function toYear(raw: string): string {
  return YEAR_PATTERN.test(raw) ? raw : ''
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
 * a tampered URL cannot send an out-of-range sort/archived/year value to the
 * backend. Free-text, tri-state and UID filters pass through verbatim: the album,
 * label and person values stay in their comma-joined form and are split into
 * repeated query params by {@link import('../services/photos').buildPhotoQuery}.
 * The backend treats an empty value as no filter, and an unknown album/label/person
 * UID simply matches nothing.
 */
export function viewToParams(view: LibraryView): PhotoListParams {
  return {
    sort: toSort(view.sort),
    archived: toArchived(view.archived),
    has_gps: view.has_gps,
    private: view.private,
    camera: view.camera,
    q: view.q,
    year: toYear(view.year),
    album: view.album,
    label: view.label,
    person: view.person,
    favorite: view.favorite,
    taken_after: view.taken_after,
    taken_before: view.taken_before,
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
    view.private !== '' ||
    view.camera !== '' ||
    (!options.ignoreQuery && view.q !== '') ||
    view.year !== '' ||
    view.album !== '' ||
    view.label !== '' ||
    view.person !== '' ||
    view.favorite !== '' ||
    view.taken_after !== '' ||
    view.taken_before !== '' ||
    view.min_rating !== '' ||
    view.flag !== ''
  )
}
