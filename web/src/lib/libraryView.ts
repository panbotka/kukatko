import { type ArchivedFilter, type PhotoListParams, type PhotoSort } from '../services/photos'

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
  taken_after: string
  taken_before: string
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
  taken_after: '',
  taken_before: '',
}

/** Accepted sort aliases; an unknown value falls back to the default. */
const SORTS: readonly PhotoSort[] = ['newest', 'oldest', 'added', 'title', 'size']

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
 * Maps the URL view state to API list params, sanitising the enum-like fields so
 * a tampered URL cannot send an out-of-range sort/archived value to the backend.
 * Free-text and tri-state filters pass through verbatim (the backend treats an
 * empty value as no filter).
 */
export function viewToParams(view: LibraryView): PhotoListParams {
  return {
    sort: toSort(view.sort),
    archived: toArchived(view.archived),
    has_gps: view.has_gps,
    private: view.private,
    camera: view.camera,
    q: view.q,
    taken_after: view.taken_after,
    taken_before: view.taken_before,
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
    view.taken_after !== '' ||
    view.taken_before !== ''
  )
}
