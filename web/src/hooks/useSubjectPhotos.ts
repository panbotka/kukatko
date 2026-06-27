import { useCallback } from 'react'

import { fetchSubjectPhotos } from '../services/people'
import { type PhotoListParams } from '../services/photos'

import { usePaginatedPhotos, type UsePaginatedPhotosResult } from './usePaginatedPhotos'

/** Stable empty params: the subject-photos endpoint needs no filters/sort. */
const NO_PARAMS: PhotoListParams = {}

/**
 * Drives a subject's paginated, infinite-scroll photo gallery. A thin wrapper
 * over {@link usePaginatedPhotos} bound to `GET /subjects/{uid}/photos`; changing
 * `uid` resets and reloads from the first page. See that hook for the paging and
 * abort semantics.
 */
export function useSubjectPhotos(uid: string): UsePaginatedPhotosResult {
  const fetcher = useCallback(
    (params: PhotoListParams, signal: AbortSignal) => fetchSubjectPhotos(uid, params, signal),
    [uid],
  )
  return usePaginatedPhotos(NO_PARAMS, fetcher, { key: uid })
}
