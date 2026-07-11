import { useCallback } from 'react'

import { fetchSubjectPhotos } from '../services/people'
import { type PhotoListParams } from '../services/photos'

import { usePaginatedPhotos, type UsePaginatedPhotosResult } from './usePaginatedPhotos'

/** Stable empty params: the subject-photos endpoint needs no filters/sort. */
const NO_PARAMS: PhotoListParams = {}

/** Options for {@link useSubjectPhotos}. */
export interface UseSubjectPhotosOptions {
  /**
   * Extra value folded into the reload key so a change outside the `uid` — a bulk
   * edit the page wants to reflect — resets and reloads the gallery. Named as in
   * the other photo-list hooks so they all refetch the same way.
   */
  reloadKey?: string
}

/**
 * Drives a subject's paginated, infinite-scroll photo gallery. A thin wrapper
 * over {@link usePaginatedPhotos} bound to `GET /subjects/{uid}/photos`; changing
 * `uid` — or `reloadKey` — resets and reloads from the first page. See that hook
 * for the paging and abort semantics.
 */
export function useSubjectPhotos(
  uid: string,
  options: UseSubjectPhotosOptions = {},
): UsePaginatedPhotosResult {
  const fetcher = useCallback(
    (params: PhotoListParams, signal: AbortSignal) => fetchSubjectPhotos(uid, params, signal),
    [uid],
  )
  return usePaginatedPhotos(NO_PARAMS, fetcher, { key: `${uid} ${options.reloadKey ?? ''}` })
}
