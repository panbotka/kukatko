import { useCallback } from 'react'

import { type PhotoListParams, searchPhotos, type SearchMode } from '../services/photos'

import { usePaginatedPhotos, type UsePaginatedPhotosResult } from './usePaginatedPhotos'

/** Result of {@link usePhotoSearch}: identical to the library list result. */
export type UsePhotoSearchResult = UsePaginatedPhotosResult

/** Options for {@link usePhotoSearch}. */
export interface UsePhotoSearchOptions {
  /**
   * Extra value folded into the reload key so a change outside `params` — a bulk
   * edit the page wants to reflect — re-runs the search. Named as in
   * `usePhotoLibrary`/`useScopedPhotos` so every photo-list hook refetches alike.
   */
  reloadKey?: string
}

/**
 * Drives the search results list, backed by `GET /search`. A thin wrapper over
 * {@link usePaginatedPhotos} that injects the search `mode` and disables itself
 * while the query (`params.q`) is empty — so an empty search box shows the
 * `idle` state and never sends the backend a query it would reject with 400.
 *
 * Changing the query text, the `mode` or the `reloadKey` resets and reloads from
 * the first page; the result also surfaces the effective `mode` and the
 * `degraded` flag the server sets when a semantic/hybrid search fell back to
 * full-text.
 *
 * `params` should be memoised by the caller so its identity changes only when
 * the query actually changes.
 */
export function usePhotoSearch(
  params: PhotoListParams,
  mode: SearchMode,
  options: UsePhotoSearchOptions = {},
): UsePhotoSearchResult {
  const fetcher = useCallback(
    (p: PhotoListParams, signal: AbortSignal) => searchPhotos(p, mode, signal),
    [mode],
  )
  const enabled = (params.q ?? '').trim() !== ''
  // `mode` discriminates the query (switching it re-runs the search with the
  // skeleton); `reloadKey` refetches in the background to reflect a bulk edit.
  return usePaginatedPhotos(params, fetcher, {
    enabled,
    key: mode,
    reloadKey: options.reloadKey,
  })
}
