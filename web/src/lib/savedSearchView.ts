import { type SavedSearchParams } from '../services/savedSearches'

import { LIBRARY_DEFAULTS, type LibraryView } from './libraryView'
import { SEARCH_DEFAULTS, type SearchView } from './searchView'
import { writeUrlState } from './urlState'

/**
 * Pure helpers mapping a saved search's opaque `params` blob back onto a
 * navigable URL. Saved params are stored verbatim as the view-state object the
 * grid/search pages serialise into the URL, so restoring one is just choosing the
 * right route and re-encoding the params minimally (defaults omitted), which makes
 * Back/Forward and sharing behave exactly as if the user had built the view.
 */

/**
 * Reports whether the saved params describe a search view (the `/search` page)
 * rather than a plain library view. A search view always carries a `mode` field,
 * which a library view never does — so `mode` presence is the reliable
 * discriminator regardless of whether a query is set.
 */
export function isSearchParams(params: SavedSearchParams): boolean {
  return typeof params.mode === 'string' && params.mode !== ''
}

/**
 * Builds the `pathname?query` a saved search opens to. It routes to `/search`
 * when the params describe a search (a `mode` is present) and to `/library`
 * otherwise, then encodes the saved params against the target page's defaults so
 * the URL is minimal and reproduces the view exactly. Unknown/stale keys are
 * ignored (only the target's known keys are encoded) and missing keys fall back
 * to their defaults.
 */
export function savedSearchHref(params: SavedSearchParams): string {
  if (isSearchParams(params)) {
    const view: SearchView = { ...SEARCH_DEFAULTS, ...params }
    const query = writeUrlState(view, SEARCH_DEFAULTS).toString()
    return query === '' ? '/search' : `/search?${query}`
  }
  const view: LibraryView = { ...LIBRARY_DEFAULTS, ...params }
  const query = writeUrlState(view, LIBRARY_DEFAULTS).toString()
  return query === '' ? '/library' : `/library?${query}`
}
