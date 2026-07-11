import { type PhotoListParams } from '../services/photos'

import {
  albumList,
  labelList,
  LIBRARY_DEFAULTS,
  LIBRARY_PATH,
  type LibraryView,
  viewToParams,
} from './libraryView'
import { searchHref } from './searchView'
import { writeUrlState } from './urlState'

/**
 * The detail page's view state: the originating library view plus the favorites
 * scope and the search `mode` it may have been opened from. Carrying this in the
 * URL lets the detail page page through the same list for prev/next and build a
 * Back link to the exact originating view — the project's "Back always works".
 *
 * The album and label scopes need no field of their own: they are library filters
 * ({@link LibraryView}'s `album`/`label` facets), and the `album`/`label` query
 * param means the same thing whether it came from a facet or from an album/label
 * page. Favorites is not expressible as a library filter, so it stays here.
 *
 * `mode` is the search-scope marker: it is only non-empty when the photo was
 * opened from the search page. Its presence — mirroring
 * {@link import('./slideshowView').SlideshowScope} — tells the detail page to
 * page prev/next through `GET /search` (ranking the query) rather than the
 * library, and Back to reconstruct the `/search?…` URL. Because the search page's
 * default mode is `hybrid` (not empty), the originating grid always writes it, so
 * a default-mode search is still recognised as a search.
 */
// A type alias (not interface) so it satisfies the urlState Record<string,string>
// constraint, like LibraryView.
export type DetailView = LibraryView & {
  favorite: string
  mode: string
}

/** Defaults: the library defaults plus an empty (no) favorites/search scope. */
export const DETAIL_DEFAULTS: DetailView = {
  ...LIBRARY_DEFAULTS,
  favorite: '',
  mode: '',
}

/**
 * Maps the detail view to the list params used to fetch the neighbouring photos:
 * the library filters/sort (album and label among them) plus the favorites scope.
 */
export function detailToParams(view: DetailView): PhotoListParams {
  return {
    ...viewToParams(view),
    favorite: view.favorite,
  }
}

/**
 * Encodes the detail view as a query string for a `/photos/{uid}?…` link, so
 * navigating from a list (or via prev/next) preserves the originating order and
 * scope. Values equal to a default are omitted.
 */
export function detailQueryString(view: DetailView): string {
  return writeUrlState(view, DETAIL_DEFAULTS).toString()
}

/**
 * Builds the Back link to the originating list view: the scoped album/label page,
 * the search results, the favorites page, or the library (the homepage), each
 * carrying the library filters/sort so the prior view is restored exactly.
 *
 * When a *single* album or label scope names the destination route, it is dropped
 * from the query — the page already scopes itself, so repeating the filter would
 * only make the URL redundant. A multi-album/label filter (or albums mixed with
 * labels) has no single scope page, so it falls through to the library — or the
 * search when `mode` is set — carrying the whole filter so the exact prior view is
 * restored. A search scope (`mode` set) is rebuilt through {@link searchHref},
 * which also carries the search query and mode.
 */
export function backHref(view: DetailView): string {
  const albums = albumList(view)
  const labels = labelList(view)
  if (albums.length === 1 && labels.length === 0) {
    return `/albums/${albums[0]}${libraryQuery({ ...view, album: '', label: '' })}`
  }
  if (labels.length === 1 && albums.length === 0) {
    return `/labels/${labels[0]}${libraryQuery({ ...view, album: '', label: '' })}`
  }
  if (view.mode !== '') {
    return searchHref(view)
  }
  if (view.favorite === 'true') {
    return `/favorites${libraryQuery(view)}`
  }
  return `${LIBRARY_PATH}${libraryQuery(view)}`
}

/**
 * Encodes the library filters/sort of a detail view as a `?…` suffix (empty when
 * everything is at its default), so a Back link restores the view it left.
 */
function libraryQuery(view: DetailView): string {
  const query = writeUrlState(view, LIBRARY_DEFAULTS).toString()
  return query === '' ? '' : `?${query}`
}
