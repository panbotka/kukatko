import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import {
  type EffectiveSearchMode,
  type Photo,
  type PhotoListParams,
  type PhotoListResponse,
} from '../services/photos'

/** Number of photos requested per page; the API caps a single page at 500. */
export const PAGE_SIZE = 100

/**
 * High-level status of the initial (first-page) load. `idle` is used when the
 * list is disabled (e.g. a search with an empty query): no request is in flight
 * and nothing is shown yet.
 */
export type ListStatus = 'idle' | 'loading' | 'error' | 'ready'

/**
 * A page fetcher: given list params and an abort signal, resolves a page of
 * photos. {@link usePaginatedPhotos} owns paging (it sets `limit`/`offset`), so
 * a fetcher only needs to forward the params and signal to the right endpoint.
 */
export type PageFetcher = (
  params: PhotoListParams,
  signal: AbortSignal,
) => Promise<PhotoListResponse>

/** Options for {@link usePaginatedPhotos}. */
export interface UsePaginatedPhotosOptions {
  /**
   * When false the hook fetches nothing and reports `idle` with an empty list
   * (e.g. a search box that is still empty). Defaults to true.
   */
  enabled?: boolean
  /**
   * Extra value folded into the *query* key so a change that is not part of
   * `params` (e.g. the search mode, or a subject uid) still resets the list and
   * reloads it from the first page — showing the loading skeleton, since it is a
   * different query. Defaults to ''.
   */
  key?: string
  /**
   * A reload token. Changing it while the query (`params`/`key`/`enabled`) is
   * unchanged triggers a *background* refetch of the pages loaded so far: the
   * currently loaded photos stay mounted and `status` stays `ready` — no
   * skeleton, no thumbnail re-stream — so a bulk edit (favorite/archive/…)
   * reflects in place instead of blanking the grid. Every loaded page is
   * refetched, not just the first, so a reader scrolled deep into the list keeps
   * their scroll position and their loaded range. A change to the query still
   * shows the genuine loading skeleton. Defaults to '' (no reloads).
   */
  reloadKey?: string
}

/** Result of {@link usePaginatedPhotos}: the accumulated photos plus paging state. */
export interface UsePaginatedPhotosResult {
  /** All photos loaded so far across pages, in server order. */
  photos: Photo[]
  /** Total number of photos matching the current query. */
  total: number
  /** Status of the first-page load (drives the page-level loading/error UI). */
  status: ListStatus
  /** True while a subsequent page is being appended. */
  loadingMore: boolean
  /** True when appending a subsequent page failed (the loaded pages remain). */
  moreError: boolean
  /** True when more pages remain to be loaded. */
  hasMore: boolean
  /** Effective search mode reported by the server, if the endpoint provides one. */
  mode?: EffectiveSearchMode
  /** True when a semantic/hybrid search fell back to full-text (sidecar offline). */
  degraded: boolean
  /** Query-language tokens the server did not understand (for the inline hint). */
  unknownTokens: string[]
  /**
   * True while a background reload (a `reloadKey` bump) refetches the loaded
   * pages. The current photos stay visible throughout, so this only lets a caller
   * show an optional non-blocking "refreshing" affordance; `status` stays `ready`.
   */
  reloading: boolean
  /** Requests the next page; a no-op while a request is in flight or none remain. */
  loadMore: () => void
  /** Re-runs the failed request (the first page, or the failed next page). */
  retry: () => void
}

/** Internal accumulator state, mutated only via the page fetch. */
interface Data {
  photos: Photo[]
  total: number
  nextOffset: number | null
  loading: boolean
  /** Whether the in-flight / most recent request is the first page. */
  initial: boolean
  /**
   * How many pages have been loaded so far. Counted rather than derived from
   * `photos.length` (a page can come back short), because a background reload
   * has to walk exactly the range the reader already has.
   */
  pages: number
  /**
   * Whether a background reload (a keep-photos refetch of the loaded pages) is
   * in flight. Kept separate from `loading` so it never flips `status` to
   * `loading` nor `loadingMore` to true — the grid stays mounted while it
   * refreshes.
   */
  reloading: boolean
  error: boolean
  mode?: EffectiveSearchMode
  degraded: boolean
  unknownTokens: string[]
}

const INITIAL: Data = {
  photos: [],
  total: 0,
  nextOffset: 0,
  loading: true,
  initial: true,
  pages: 0,
  reloading: false,
  error: false,
  degraded: false,
  unknownTokens: [],
}

/** Idle state used while the list is disabled: nothing loaded, nothing loading. */
const IDLE: Data = { ...INITIAL, loading: false }

/**
 * Drives a paginated, infinite-scroll photo list backed by an arbitrary page
 * `fetcher` (the library list, search, …). Given the current filter/sort params,
 * it loads the first page and exposes {@link UsePaginatedPhotosResult.loadMore}
 * to append further pages. Changing `params` (or `options.key`, or `enabled`)
 * resets the accumulator and reloads from the first page with the loading
 * skeleton. Bumping `options.reloadKey` while the query is unchanged instead
 * refetches the pages loaded so far in the *background*, keeping the current
 * photos mounted (see {@link UsePaginatedPhotosOptions.reloadKey}) — so a bulk
 * edit reflects in place without blanking the grid or losing the reader's place
 * in it. In-flight requests are aborted on change and on unmount, and stale
 * responses are ignored, so rapid query changes never leave the list showing the
 * wrong query's results.
 *
 * `params` should be memoised by the caller (e.g. derived from URL state via
 * `useMemo`) so its identity changes only when the query actually changes; the
 * `fetcher` is read through a ref, so it need not be stable.
 */
export function usePaginatedPhotos(
  params: PhotoListParams,
  fetcher: PageFetcher,
  options: UsePaginatedPhotosOptions = {},
): UsePaginatedPhotosResult {
  const enabled = options.enabled ?? true
  const queryDiscriminator = options.key ?? ''
  const reloadKey = options.reloadKey ?? ''

  // A stable key over the meaningful query so an unchanged query does not
  // trigger a reset even if the params object identity changes. It deliberately
  // excludes `reloadKey`, which drives a background refetch (see the effect
  // below) rather than a reset-to-skeleton.
  const queryKey = useMemo(
    () => JSON.stringify(params) + ' ' + queryDiscriminator + ' ' + String(enabled),
    [params, queryDiscriminator, enabled],
  )

  const [data, setData] = useState<Data>(enabled ? INITIAL : IDLE)

  // Refs let loadMore/retry and the fetch read the latest values synchronously
  // (without being re-created on every change) and guard against overlapping
  // requests before React has re-rendered.
  const paramsRef = useRef(params)
  paramsRef.current = params
  const fetcherRef = useRef(fetcher)
  fetcherRef.current = fetcher
  const dataRef = useRef(data)
  dataRef.current = data
  const controllerRef = useRef<AbortController | null>(null)
  const loadingRef = useRef(false)
  const seqRef = useRef(0)
  // Latest `enabled`, read by the reload effect without re-subscribing it to
  // `enabled` (which would let an enable/disable masquerade as a reload).
  const enabledRef = useRef(enabled)
  enabledRef.current = enabled

  const fetchPage = useCallback((offset: number, isInitial: boolean) => {
    loadingRef.current = true
    controllerRef.current?.abort()
    const controller = new AbortController()
    controllerRef.current = controller
    const seq = ++seqRef.current

    setData((prev) =>
      isInitial
        ? // A first load or a query change: reset to the loading skeleton.
          { ...INITIAL, loading: true }
        : { ...prev, loading: true, initial: false, error: false },
    )

    fetcherRef
      .current({ ...paramsRef.current, limit: PAGE_SIZE, offset }, controller.signal)
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
          pages: isInitial ? 1 : prev.pages + 1,
          reloading: false,
          error: false,
          mode: res.mode,
          degraded: res.degraded ?? false,
          unknownTokens: res.unknown_tokens ?? [],
        }))
      })
      .catch((err: unknown) => {
        if (seq !== seqRef.current || (err instanceof DOMException && err.name === 'AbortError')) {
          return
        }
        loadingRef.current = false
        setData((prev) => ({
          ...prev,
          loading: false,
          reloading: false,
          initial: isInitial,
          error: true,
        }))
      })
  }, [])

  /**
   * Refetches, in the background, every page the reader has already loaded.
   * Refetching only the first page would replace a deeply-scrolled grid with the
   * first {@link PAGE_SIZE} photos — bouncing the reader to the bottom of a much
   * shorter list and silently dropping the pages in between — so the walk follows
   * the server's `next_offset` until the loaded range is covered, or until the
   * (possibly shrunken) result runs out. The current photos stay mounted the
   * whole time and failure is silent, exactly as for a single-page refresh.
   */
  const reloadLoaded = useCallback(() => {
    const pages = Math.max(1, dataRef.current.pages)
    loadingRef.current = true
    controllerRef.current?.abort()
    const controller = new AbortController()
    controllerRef.current = controller
    const seq = ++seqRef.current

    setData((prev) => ({ ...prev, reloading: true, error: false }))

    void (async () => {
      const photos: Photo[] = []
      let offset: number | null = 0
      let last: PhotoListResponse | null = null
      let loaded = 0
      try {
        while (loaded < pages && offset !== null) {
          const res: PhotoListResponse = await fetcherRef.current(
            { ...paramsRef.current, limit: PAGE_SIZE, offset },
            controller.signal,
          )
          if (seq !== seqRef.current) {
            return
          }
          photos.push(...res.photos)
          offset = res.next_offset
          last = res
          loaded++
        }
      } catch (err: unknown) {
        if (seq !== seqRef.current || (err instanceof DOMException && err.name === 'AbortError')) {
          return
        }
        loadingRef.current = false
        // A failed background refresh is silent: keep the current list visible
        // rather than blanking it to the error state.
        setData((prev) => ({ ...prev, reloading: false }))
        return
      }
      if (last === null) {
        return
      }
      loadingRef.current = false
      const res = last
      setData(() => ({
        photos,
        total: res.total,
        nextOffset: res.next_offset,
        loading: false,
        initial: loaded <= 1,
        pages: loaded,
        reloading: false,
        error: false,
        mode: res.mode,
        degraded: res.degraded ?? false,
        unknownTokens: res.unknown_tokens ?? [],
      }))
    })()
  }, [])

  // Load the first page whenever the query changes; abort on unmount/change.
  // When disabled, drop any in-flight request and reset to idle without fetching.
  useEffect(() => {
    if (!enabled) {
      controllerRef.current?.abort()
      loadingRef.current = false
      seqRef.current++
      setData(IDLE)
      return
    }
    fetchPage(0, true)
    return () => {
      controllerRef.current?.abort()
    }
  }, [queryKey, enabled, fetchPage])

  // A `reloadKey` bump (with the query unchanged) refetches the loaded pages in
  // the background, so a bulk edit reflects without unmounting the grid.
  // `lastReloadKey` starts at the initial value so neither the first mount nor
  // React StrictMode's double-invoked mount effect fires a spurious reload — it
  // acts only on an actual change of the token.
  const lastReloadKeyRef = useRef(reloadKey)
  useEffect(() => {
    if (lastReloadKeyRef.current === reloadKey) {
      return
    }
    lastReloadKeyRef.current = reloadKey
    if (!enabledRef.current) {
      return
    }
    reloadLoaded()
  }, [reloadKey, reloadLoaded])

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

  const status: ListStatus = !enabled
    ? 'idle'
    : data.initial && data.loading
      ? 'loading'
      : data.initial && data.error
        ? 'error'
        : 'ready'

  return {
    photos: data.photos,
    total: data.total,
    status,
    loadingMore: data.loading && !data.initial,
    moreError: data.error && !data.initial,
    hasMore: data.nextOffset !== null,
    mode: data.mode,
    degraded: data.degraded,
    unknownTokens: data.unknownTokens,
    reloading: data.reloading,
    loadMore,
    retry,
  }
}
