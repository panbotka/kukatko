import { useCallback, useMemo } from 'react'

import { fetchPhotos, type PhotoListParams } from '../services/photos'

import { usePaginatedPhotos, type UsePaginatedPhotosResult } from './usePaginatedPhotos'

/**
 * A photo-list scope: restrict the listing to one album or one label. Exactly
 * one field is set by the album/label detail pages; both empty would list the
 * whole library, which these callers never do.
 */
export interface PhotoScope {
  album?: string
  label?: string
}

/**
 * Drives a paginated, infinite-scroll photo grid scoped to an album or label,
 * honouring the library filters/sort in `params`. A thin wrapper over
 * {@link usePaginatedPhotos} bound to `GET /photos` with the scope folded into
 * the query (`?album=`/`?label=`), so album and label galleries reuse the same
 * grid, filters and paging as the main library. Changing the scope or `params`
 * resets and reloads from the first page.
 *
 * `params` should be memoised by the caller (e.g. derived from URL state) so its
 * identity changes only when the query actually changes.
 */
export function useScopedPhotos(
  scope: PhotoScope,
  params: PhotoListParams,
  reloadKey?: string,
): UsePaginatedPhotosResult {
  const scoped = useMemo<PhotoListParams>(
    () => ({ ...params, album: scope.album, label: scope.label }),
    [params, scope.album, scope.label],
  )
  const fetcher = useCallback(
    (p: PhotoListParams, signal: AbortSignal) => fetchPhotos(p, signal),
    [],
  )
  return usePaginatedPhotos(scoped, fetcher, { key: reloadKey })
}
