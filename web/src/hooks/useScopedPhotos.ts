import { useCallback, useMemo } from 'react'

import { fetchPhotos, type PhotoListParams } from '../services/photos'

import { usePaginatedPhotos, type UsePaginatedPhotosResult } from './usePaginatedPhotos'

/**
 * A photo-list scope: restrict the listing to one album, one label, or one
 * place (country and/or city). The detail pages set exactly the field(s) their
 * view needs (an album, a label, or a country + city); all empty would list the
 * whole library, which these callers never do.
 */
export interface PhotoScope {
  album?: string
  label?: string
  /** Scope to photos taken in this country (paired with `city` on the Places page). */
  country?: string
  /** Scope to photos taken in this city. */
  city?: string
}

/** Options for {@link useScopedPhotos}. */
export interface UseScopedPhotosOptions {
  /**
   * Extra value folded into the reload key so a change outside `params` (e.g. a
   * mutation the page wants to reflect) resets and reloads the grid.
   */
  reloadKey?: string
  /**
   * When false the hook fetches nothing and reports `idle` — e.g. the Places
   * page before a city is selected. Defaults to true.
   */
  enabled?: boolean
}

/**
 * Drives a paginated, infinite-scroll photo grid scoped to an album, a label or
 * a place, honouring the library filters/sort in `params`. A thin wrapper over
 * {@link usePaginatedPhotos} bound to `GET /photos` with the scope folded into
 * the query (`?album=`/`?label=`/`?country=&city=`), so album, label and place
 * galleries reuse the same grid, filters and paging as the main library.
 * Changing the scope or `params` resets and reloads from the first page.
 *
 * `params` should be memoised by the caller (e.g. derived from URL state) so its
 * identity changes only when the query actually changes.
 */
export function useScopedPhotos(
  scope: PhotoScope,
  params: PhotoListParams,
  options: UseScopedPhotosOptions = {},
): UsePaginatedPhotosResult {
  // A scope the page does not set must leave the matching filter in `params`
  // untouched: the Places page scopes by country/city while the filter bar may
  // still carry an album or label facet, and an album page may carry a label
  // facet. Overwriting with the absent scope's `undefined` would silently drop it.
  const scoped = useMemo<PhotoListParams>(
    () => ({
      ...params,
      album: scope.album ?? params.album,
      label: scope.label ?? params.label,
      country: scope.country ?? params.country,
      city: scope.city ?? params.city,
    }),
    [params, scope.album, scope.label, scope.country, scope.city],
  )
  const fetcher = useCallback(
    (p: PhotoListParams, signal: AbortSignal) => fetchPhotos(p, signal),
    [],
  )
  return usePaginatedPhotos(scoped, fetcher, {
    key: options.reloadKey,
    enabled: options.enabled,
  })
}
