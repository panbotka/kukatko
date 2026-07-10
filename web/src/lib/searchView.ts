import { type SearchMode } from '../services/photos'

import { type LibraryView, LIBRARY_DEFAULTS } from './libraryView'
import { writeUrlState } from './urlState'

/**
 * URL-encoded view state for the search page: every library filter/sort field
 * plus the search `mode`. As with {@link LibraryView}, all values are strings so
 * the whole view round-trips through the query string and Back/Forward restores
 * it exactly. `q` here is the search query (the prominent search input), not a
 * substring filter.
 *
 * A type alias (an intersection of {@link LibraryView} and `mode`) rather than an
 * interface, so it keeps the implicit index signature the urlState
 * `Record<string, string>` constraint requires.
 */
export type SearchView = LibraryView & {
  mode: string
}

/**
 * Default search view: hybrid mode, no query, library defaults for the rest.
 * Declared at module scope so the urlState setter keeps a stable identity and a
 * value equal to a default is omitted from the URL (keeping it shareable).
 */
export const SEARCH_DEFAULTS: SearchView = {
  ...LIBRARY_DEFAULTS,
  mode: 'hybrid',
}

/** Accepted search modes; an unknown value falls back to the default `hybrid`. */
const MODES: readonly SearchMode[] = ['fulltext', 'semantic', 'hybrid']

/** Narrows a raw string to a known search mode, defaulting to "hybrid". */
export function toMode(raw: string): SearchMode {
  return (MODES as readonly string[]).includes(raw) ? (raw as SearchMode) : 'hybrid'
}

/**
 * Builds the link from a library view to the search page, carrying the filters
 * (and the quick-filter text as the search query) so the reader lands on the same
 * photos and can widen them with full-text or semantic search. Values equal to a
 * default — the search `mode` among them — are omitted, keeping the URL minimal.
 *
 * This is the one bridge between the two pages: the library filters by substring,
 * `/search` searches; neither duplicates the other.
 */
export function searchHref(view: LibraryView): string {
  const query = writeUrlState({ ...SEARCH_DEFAULTS, ...view }, SEARCH_DEFAULTS).toString()
  return query === '' ? '/search' : `/search?${query}`
}
