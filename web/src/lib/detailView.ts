import { type PhotoListParams } from '../services/photos'

import { LIBRARY_DEFAULTS, LIBRARY_PATH, type LibraryView, viewToParams } from './libraryView'
import { writeUrlState } from './urlState'

/**
 * The detail page's view state: the originating library view plus the favorites
 * scope it may have been opened from. Carrying this in the URL lets the detail
 * page page through the same list for prev/next and build a Back link to the
 * exact originating view — the project's "Back always works".
 *
 * The album and label scopes need no field of their own: they are library filters
 * ({@link LibraryView}'s `album`/`label` facets), and the `album`/`label` query
 * param means the same thing whether it came from a facet or from an album/label
 * page. Favorites is not expressible as a library filter, so it stays here.
 */
// A type alias (not interface) so it satisfies the urlState Record<string,string>
// constraint, like LibraryView.
export type DetailView = LibraryView & {
  favorite: string
}

/** Defaults: the library defaults plus an empty (no) favorites scope. */
export const DETAIL_DEFAULTS: DetailView = {
  ...LIBRARY_DEFAULTS,
  favorite: '',
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
 * the favorites page, or the library (the homepage), each carrying the library
 * filters/sort so the prior view is restored exactly.
 *
 * When an album or label scope names the destination route, it is dropped from
 * the query — the page already scopes itself, so repeating the filter would only
 * make the URL redundant.
 */
export function backHref(view: DetailView): string {
  if (view.album !== '') {
    return `/albums/${view.album}${libraryQuery({ ...view, album: '', label: '' })}`
  }
  if (view.label !== '') {
    return `/labels/${view.label}${libraryQuery({ ...view, album: '', label: '' })}`
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
