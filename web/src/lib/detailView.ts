import { type PhotoListParams } from '../services/photos'

import { LIBRARY_DEFAULTS, LIBRARY_PATH, type LibraryView, viewToParams } from './libraryView'
import { writeUrlState } from './urlState'

/**
 * The detail page's view state: the originating library view plus the scope it
 * was opened from (an album, a label, or the user's favorites). Carrying this in
 * the URL lets the detail page page through the same list for prev/next and build
 * a Back link to the exact originating view — the project's "Back always works".
 */
// A type alias (not interface) so it satisfies the urlState Record<string,string>
// constraint, like LibraryView.
export type DetailView = LibraryView & {
  album: string
  label: string
  favorite: string
}

/** Defaults: the library defaults plus an empty (no) scope. */
export const DETAIL_DEFAULTS: DetailView = {
  ...LIBRARY_DEFAULTS,
  album: '',
  label: '',
  favorite: '',
}

/**
 * Maps the detail view to the list params used to fetch the neighbouring photos,
 * folding in the album/label/favorite scope on top of the library filters/sort.
 */
export function detailToParams(view: DetailView): PhotoListParams {
  return {
    ...viewToParams(view),
    album: view.album,
    label: view.label,
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
 */
export function backHref(view: DetailView): string {
  const libraryQuery = writeUrlState(view, LIBRARY_DEFAULTS).toString()
  const suffix = libraryQuery === '' ? '' : `?${libraryQuery}`
  if (view.album !== '') {
    return `/albums/${view.album}${suffix}`
  }
  if (view.label !== '') {
    return `/labels/${view.label}${suffix}`
  }
  if (view.favorite === 'true') {
    return `/favorites${suffix}`
  }
  return `${LIBRARY_PATH}${suffix}`
}
