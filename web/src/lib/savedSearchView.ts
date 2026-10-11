import { type SearchMode } from '../services/photos'
import { type SavedSearchParams } from '../services/savedSearches'

import { LIBRARY_DEFAULTS, LIBRARY_PATH, type LibraryView } from './libraryView'
import { SEARCH_DEFAULTS, type SearchView, toMode } from './searchView'
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
 * when the params describe a search (a `mode` is present) and to the library
 * homepage otherwise, then encodes the saved params against the target page's
 * defaults so the URL is minimal and reproduces the view exactly. Unknown/stale
 * keys are ignored (only the target's known keys are encoded) and missing keys
 * fall back to their defaults.
 */
export function savedSearchHref(params: SavedSearchParams): string {
  if (isSearchParams(params)) {
    const view: SearchView = { ...SEARCH_DEFAULTS, ...params }
    const query = writeUrlState(view, SEARCH_DEFAULTS).toString()
    return query === '' ? '/search' : `/search?${query}`
  }
  const view: LibraryView = { ...LIBRARY_DEFAULTS, ...params }
  const query = writeUrlState(view, LIBRARY_DEFAULTS).toString()
  return query === '' ? LIBRARY_PATH : `${LIBRARY_PATH}?${query}`
}

/**
 * The one piece of a saved search's state beyond its query that the list shows
 * as a badge — whichever says most about what the search returns:
 *
 *   - `filters`: how many library facets narrow it (album, person, period, …);
 *   - `mode`: a search saved with a non-default way of searching;
 *   - `sort`: a view saved with a non-default order.
 */
export type SavedSearchBadge =
  | { kind: 'filters'; count: number }
  | { kind: 'mode'; mode: SearchMode }
  | { kind: 'sort'; sort: string }

/** What the saved-searches list prints under a saved search's name. */
export interface SavedSearchSummary {
  /** The saved query string (`q`), trimmed; `''` when the search has none. */
  query: string
  /** The most relevant extra state, or `null` when the search carries none. */
  badge: SavedSearchBadge | null
}

/**
 * The view keys that are neither the query nor its presentation: each one,
 * when set away from its default, narrows the result set. The period is one
 * filter however it is spelled, so its three keys (the legacy `year` and the
 * `taken_after`/`taken_before` pair) collapse into one entry.
 */
const FILTER_KEY_GROUPS: readonly (readonly (keyof LibraryView)[])[] = [
  ['archived'],
  ['has_gps'],
  ['dated'],
  ['camera'],
  ['year', 'taken_after', 'taken_before'],
  ['album'],
  ['label'],
  ['task'],
  ['person'],
  ['uploader'],
  ['favorite'],
  ['min_rating'],
  ['flag'],
]

/**
 * Reads one key of a saved params blob, `''` when it is absent. The blob is
 * stored verbatim, so a key the view had at its default is usually missing.
 */
function paramOf(params: SavedSearchParams, key: string): string {
  const value = params[key]
  return typeof value === 'string' ? value : ''
}

/**
 * Summarises a saved search for its row in the list: the query it was saved
 * with, plus the single most relevant piece of extra state. Filters outrank the
 * search mode, which outranks the sort — a filter changes *which* photos come
 * back, a mode changes how they are found, a sort only their order. Values equal
 * to the target page's defaults (and unknown keys) carry no state and are
 * ignored, exactly as {@link savedSearchHref} drops them from the URL.
 */
export function savedSearchSummary(params: SavedSearchParams): SavedSearchSummary {
  const query = paramOf(params, 'q').trim()
  const differs = (key: keyof LibraryView): boolean => {
    const value = paramOf(params, key)
    return value !== '' && value !== LIBRARY_DEFAULTS[key]
  }
  const filters = FILTER_KEY_GROUPS.filter((group) => group.some(differs)).length
  if (filters > 0) {
    return { query, badge: { kind: 'filters', count: filters } }
  }
  if (isSearchParams(params)) {
    const mode = toMode(paramOf(params, 'mode'))
    if (mode !== SEARCH_DEFAULTS.mode) {
      return { query, badge: { kind: 'mode', mode } }
    }
  }
  if (differs('sort')) {
    return { query, badge: { kind: 'sort', sort: paramOf(params, 'sort') } }
  }
  return { query, badge: null }
}
