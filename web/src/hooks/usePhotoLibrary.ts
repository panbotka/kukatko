import { fetchPhotos, type PhotoListParams } from '../services/photos'

import {
  type ListStatus,
  PAGE_SIZE,
  usePaginatedPhotos,
  type UsePaginatedPhotosResult,
} from './usePaginatedPhotos'

export { PAGE_SIZE }

/** High-level status of the initial (first-page) load. */
export type LibraryStatus = ListStatus

/** Result of {@link usePhotoLibrary}: the accumulated photos plus paging state. */
export type UsePhotoLibraryResult = UsePaginatedPhotosResult

/** Options for {@link usePhotoLibrary}. */
export interface UsePhotoLibraryOptions {
  /**
   * Extra value folded into the reload key so a change outside `params` — a bulk
   * edit the page wants to reflect — resets and reloads the grid. Named as in
   * `useScopedPhotos` so both library hooks refetch the same way.
   */
  reloadKey?: string
}

/**
 * Drives the library's paginated, infinite-scroll photo list. A thin wrapper
 * over {@link usePaginatedPhotos} bound to the `GET /photos` endpoint; see that
 * hook for the paging/abort semantics. Changing `params` — or `reloadKey` —
 * resets and reloads from the first page.
 *
 * `params` should be memoised by the caller (e.g. derived from URL state via
 * `useMemo`) so its identity changes only when the query actually changes.
 */
export function usePhotoLibrary(
  params: PhotoListParams,
  options: UsePhotoLibraryOptions = {},
): UsePhotoLibraryResult {
  return usePaginatedPhotos(params, fetchPhotos, { key: options.reloadKey })
}
