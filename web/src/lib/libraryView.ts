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
   * Album facet: '' (none) or a {@link UID_DELIMITER}-joined list of album UIDs,
   * combined with AND — a photo matches only if it is in every listed album. A
   * single UID is the common case and round-trips as `?album=al_1`. Doubles as the
   * detail page's album scope (see {@link import('./detailView').DetailView}) — the
   * same `album` query param means the same thing everywhere. Use {@link albumList}
   * to read it and {@link addUID}/{@link removeUID} to edit it.
   */
  album: string
  /**
   * Label facet: '' (none) or a {@link UID_DELIMITER}-joined list of label UIDs,
   * combined with AND — a photo matches only if it carries every listed label.
   * Doubles as the detail page's label scope. Read via {@link labelList}.
   */
  label: string
  taken_after: string
  taken_before: string
  /** Minimum star rating filter: '' (any) or '1'–'5'. */
  min_rating: string
  /** Pick/reject flag filter: '' (any), 'pick' or 'reject'. */
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

/**
 * The delimiter joining multiple album/label UIDs inside the single-string
 * `album`/`label` view fields (the urlState layer stores every key as one
 * string). A comma cannot appear in a UID, so it never collides with a value and
 * the joined list round-trips cleanly through the URL query string.
 */
export const UID_DELIMITER = ','

/** Splits a delimiter-joined UID field into its UIDs, dropping empties (`''` → `[]`). */
export function splitUIDs(joined: string): string[] {
  return joined.split(UID_DELIMITER).filter((uid) => uid !== '')
}

/** Joins UIDs back into the delimiter-encoded field (`[]` → `''`). */
export function joinUIDs(uids: string[]): string {
  return uids.join(UID_DELIMITER)
}

/** The album UIDs currently selected, in order. */
export function albumList(view: LibraryView): string[] {
  return splitUIDs(view.album)
}

/** The label UIDs currently selected, in order. */
export function labelList(view: LibraryView): string[] {
  return splitUIDs(view.label)
}

/**
 * Returns the joined field with `uid` appended, or the field unchanged when it is
 * already present (selections are a set, so a facet is never added twice).
 */
export function addUID(joined: string, uid: string): string {
  const uids = splitUIDs(joined)
  return uids.includes(uid) ? joined : joinUIDs([...uids, uid])
}

/** Returns the joined field with `uid` removed (a no-op when it is absent). */
export function removeUID(joined: string, uid: string): string {
  return joinUIDs(splitUIDs(joined).filter((existing) => existing !== uid))
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
 * Maps the URL view state to API list params, sanitising the enum-like fields so
 * a tampered URL cannot send an out-of-range sort/archived/year value to the
 * backend. Free-text, tri-state and UID filters pass through verbatim (the
 * backend treats an empty value as no filter, and an unknown album/label UID
 * simply matches nothing). The delimiter-joined `album`/`label` fields are split
 * into UID lists the query builder emits as repeated params, one per selected UID.
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
    album: splitUIDs(view.album),
    label: splitUIDs(view.label),
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
    view.taken_after !== '' ||
    view.taken_before !== '' ||
    view.min_rating !== '' ||
    view.flag !== ''
  )
}
