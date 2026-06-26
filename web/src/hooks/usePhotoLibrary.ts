import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { fetchPhotos, type Photo, type PhotoListParams } from '../services/photos'

/** Number of photos requested per page; the API caps a single page at 500. */
export const PAGE_SIZE = 100

/** High-level status of the initial (first-page) load. */
export type LibraryStatus = 'loading' | 'error' | 'ready'

/** Result of {@link usePhotoLibrary}: the accumulated photos plus paging state. */
export interface UsePhotoLibraryResult {
  /** All photos loaded so far across pages, in server order. */
  photos: Photo[]
  /** Total number of photos matching the current filters. */
  total: number
  /** Status of the first-page load (drives the page-level loading/error UI). */
  status: LibraryStatus
  /** True while a subsequent page is being appended. */
  loadingMore: boolean
  /** True when appending a subsequent page failed (the loaded pages remain). */
  moreError: boolean
  /** True when more pages remain to be loaded. */
  hasMore: boolean
  /** Requests the next page; a no-op while a request is in flight or none remain. */
  loadMore: () => void
  /** Re-runs the failed request (the first page, or the failed next page). */
  retry: () => void
}

/** Internal accumulator state, mutated only via {@link fetchPage}. */
interface Data {
  photos: Photo[]
  total: number
  nextOffset: number | null
  loading: boolean
  /** Whether the in-flight / most recent request is the first page. */
  initial: boolean
  error: boolean
}

const INITIAL: Data = {
  photos: [],
  total: 0,
  nextOffset: 0,
  loading: true,
  initial: true,
  error: false,
}

/**
 * Drives a paginated, infinite-scroll photo list. Given the current filter/sort
 * params, it loads the first page and exposes {@link UsePhotoLibraryResult.loadMore}
 * to append further pages. Changing `params` resets the accumulator and reloads
 * from the first page. In-flight requests are aborted on param change and on
 * unmount, and stale responses are ignored, so rapid filter changes never leave
 * the list showing the wrong query's results.
 *
 * `params` should be memoised by the caller (e.g. derived from URL state via
 * `useMemo`) so its identity changes only when the query actually changes.
 */
export function usePhotoLibrary(params: PhotoListParams): UsePhotoLibraryResult {
  // A stable key over the meaningful query so an unchanged filter set does not
  // trigger a reload even if the params object identity changes.
  const key = useMemo(() => JSON.stringify(params), [params])

  const [data, setData] = useState<Data>(INITIAL)

  // Refs let loadMore/retry read the latest state and params synchronously
  // (without being re-created on every change) and guard against overlapping
  // requests before React has re-rendered.
  const paramsRef = useRef(params)
  paramsRef.current = params
  const dataRef = useRef(data)
  dataRef.current = data
  const controllerRef = useRef<AbortController | null>(null)
  const loadingRef = useRef(false)
  const seqRef = useRef(0)

  const fetchPage = useCallback((offset: number, isInitial: boolean) => {
    loadingRef.current = true
    controllerRef.current?.abort()
    const controller = new AbortController()
    controllerRef.current = controller
    const seq = ++seqRef.current

    setData((prev) =>
      isInitial
        ? { ...INITIAL, loading: true }
        : { ...prev, loading: true, initial: false, error: false },
    )

    fetchPhotos({ ...paramsRef.current, limit: PAGE_SIZE, offset }, controller.signal)
      .then((res) => {
        if (seq !== seqRef.current) {
          return
        }
        loadingRef.current = false
        setData((prev) => ({
          photos: isInitial ? res.photos : [...prev.photos, ...res.photos],
          total: res.total,
          nextOffset: res.next_offset,
          loading: false,
          initial: isInitial,
          error: false,
        }))
      })
      .catch((err: unknown) => {
        if (seq !== seqRef.current || (err instanceof DOMException && err.name === 'AbortError')) {
          return
        }
        loadingRef.current = false
        setData((prev) => ({ ...prev, loading: false, initial: isInitial, error: true }))
      })
  }, [])

  // Load the first page whenever the query changes; abort on unmount/change.
  useEffect(() => {
    fetchPage(0, true)
    return () => {
      controllerRef.current?.abort()
    }
  }, [key, fetchPage])

  const loadMore = useCallback(() => {
    const current = dataRef.current
    if (loadingRef.current || current.nextOffset === null) {
      return
    }
    fetchPage(current.nextOffset, false)
  }, [fetchPage])

  const retry = useCallback(() => {
    const current = dataRef.current
    if (current.photos.length === 0) {
      fetchPage(0, true)
      return
    }
    if (current.nextOffset !== null) {
      fetchPage(current.nextOffset, false)
    }
  }, [fetchPage])

  const status: LibraryStatus =
    data.initial && data.loading ? 'loading' : data.initial && data.error ? 'error' : 'ready'

  return {
    photos: data.photos,
    total: data.total,
    status,
    loadingMore: data.loading && !data.initial,
    moreError: data.error && !data.initial,
    hasMore: data.nextOffset !== null,
    loadMore,
    retry,
  }
}
