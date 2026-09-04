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
 * Upper bound on the pages the initial load will walk to reach a restored
 * position ({@link UsePaginatedPhotosOptions.initialCount}). A reader coming back
 * to where they were waits for these requests with the skeleton up, so the depth
 * that can be restored is bounded rather than the wait: past this the grid opens
 * at the deepest position it did reach, which is still far closer than the top.
 */
export const RESTORE_MAX_PAGES = 12

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
  /**
   * How many photos the initial load should reach before reporting `ready`,
   * rounded up to whole pages and capped at {@link RESTORE_MAX_PAGES}. A list
   * that grows by appending pages otherwise comes back holding only its first
   * page, which is far too short a document to restore a deep scroll position
   * into — so a page returning a reader to where they were passes the length the
   * list had then. Defaults to 0 (one page, as before). Read once per query: a
   * later change does not refetch.
   */
  initialCount?: number
}

/** Result of {@link usePaginatedPhotos}: the accumulated photos plus paging state. */
export interface UsePaginatedPhotosResult {
  /** All photos loaded so far across pages, in server order. */
  photos: Photo[]
  /** Total number of photos matching the current query. */
  total: number
  /**
   * True when `total` is not a count of everything that matches but the size of
   * the ranked set a semantic/hybrid search returned out of its bounded
   * candidate pool. Such a number saturates at the pool size, so a caller must
   * word it as "the best matches", never as a total. False on every list and on
   * the search modes that run a real count.
   */
  totalRanked: boolean
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
   * Machine-readable reasons the page is what it is, as the server reported them
   * in `notices` — currently only `person_me_unlinked`, an empty result because
   * the caller asked for `person:me` without their account having said which
   * person that is. The caller turns each code into a sentence.
   */
  notices: string[]
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
  totalRanked: boolean
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
  notices: string[]
}

const INITIAL: Data = {
  photos: [],
  total: 0,
  totalRanked: false,
  nextOffset: 0,
  loading: true,
  initial: true,
  pages: 0,
  reloading: false,
  error: false,
  degraded: false,
  unknownTokens: [],
  notices: [],
}

/** Idle state used while the list is disabled: nothing loaded, nothing loading. */
const IDLE: Data = { ...INITIAL, loading: false }

/**
 * Drives a paginated, infinite-scroll photo list backed by an arbitrary page
 * `fetcher` (the library list, search, …). Given the current filter/sort params,
 * it loads the first page and exposes {@link UsePaginatedPhotosResult.loadMore}
 * to append further pages. Changing `params` (or `options.key`, or `enabled`)
 * resets the accumulator and reloads from the first page with the loading
 * skeleton — or from the first few, when `options.initialCount` asks the list to
 * come back as long as it was, so a page returning a reader to where they were
 * has a document tall enough to scroll there. Bumping `options.reloadKey` while
 * the query is unchanged instead
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
  // How many pages the next initial load walks. Held in a ref so growing the
  // restore target never counts as a query change: it is read when a query
  // actually starts, and changing it alone refetches nothing.
  const initialPagesRef = useRef(1)
  initialPagesRef.current = Math.min(
    RESTORE_MAX_PAGES,
    Math.max(1, Math.ceil((options.initialCount ?? 0) / PAGE_SIZE)),
  )

  /** Appends one further page at `offset`, keeping everything already loaded. */
  const fetchNext = useCallback((offset: number) => {
    loadingRef.current = true
    controllerRef.current?.abort()
    const controller = new AbortController()
    controllerRef.current = controller
    const seq = ++seqRef.current

    setData((prev) => ({ ...prev, loading: true, initial: false, error: false }))

    fetcherRef
      .current({ ...paramsRef.current, limit: PAGE_SIZE, offset }, controller.signal)
      .then((res) => {
        if (seq !== seqRef.current) {
          return
        }
        loadingRef.current = false
        setData((prev) => ({
          photos: [...prev.photos, ...res.photos],
          total: res.total,
          totalRanked: res.ranked_total ?? false,
          nextOffset: res.next_offset,
          loading: false,
          initial: false,
          pages: prev.pages + 1,
          reloading: false,
          error: false,
          mode: res.mode,
          degraded: res.degraded ?? false,
          unknownTokens: res.unknown_tokens ?? [],
          notices: res.notices ?? [],
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
          initial: false,
          error: true,
        }))
      })
  }, [])

  /**
   * Loads the list's first `pages` pages in one walk, following the server's
   * `next_offset`, and replaces the list with what came back. Two callers want
   * exactly this: the initial load of a query (one page normally, more when a
   * position is being restored into a list that only ever grew by appending), and
   * the background refresh of everything a reader already has.
   *
   * Refetching only the first page for the latter would replace a deeply-scrolled
   * grid with the first {@link PAGE_SIZE} photos — bouncing the reader to the
   * bottom of a much shorter list and silently dropping the pages in between.
   *
   * `background` decides what the reader sees meanwhile: a background walk keeps
   * the current photos mounted throughout and fails silently; a foreground one
   * shows the loading skeleton and reports a failure — unless some pages did
   * arrive, which are kept, a short list being a better answer than an error.
   */
  const loadPrefix = useCallback((pages: number, background: boolean) => {
    loadingRef.current = true
    controllerRef.current?.abort()
    const controller = new AbortController()
    controllerRef.current = controller
    const seq = ++seqRef.current

    setData((prev) =>
      background
        ? { ...prev, reloading: true, error: false }
        : // A first load or a query change: reset to the loading skeleton.
          { ...INITIAL, loading: true },
    )

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
        if (background || last === null) {
          loadingRef.current = false
          setData((prev) =>
            background
              ? { ...prev, reloading: false }
              : { ...prev, loading: false, reloading: false, initial: true, error: true },
          )
          return
        }
        // Some pages did arrive before the walk died: keep them and fall through.
        // `hasMore` still points past them, so scrolling on simply asks again for
        // the page that failed.
      }
      if (last === null) {
        return
      }
      loadingRef.current = false
      const res = last
      setData(() => ({
        photos,
        total: res.total,
        totalRanked: res.ranked_total ?? false,
        nextOffset: res.next_offset,
        loading: false,
        initial: loaded <= 1,
        pages: loaded,
        reloading: false,
        error: false,
        mode: res.mode,
        degraded: res.degraded ?? false,
        unknownTokens: res.unknown_tokens ?? [],
        notices: res.notices ?? [],
      }))
    })()
  }, [])

  // Load the first page(s) whenever the query changes; abort on unmount/change.
  // When disabled, drop any in-flight request and reset to idle without fetching.
  useEffect(() => {
    if (!enabled) {
      controllerRef.current?.abort()
      loadingRef.current = false
      seqRef.current++
      setData(IDLE)
      return
    }
    loadPrefix(initialPagesRef.current, false)
    return () => {
      controllerRef.current?.abort()
    }
  }, [queryKey, enabled, loadPrefix])

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
    loadPrefix(Math.max(1, dataRef.current.pages), true)
  }, [reloadKey, loadPrefix])

  const loadMore = useCallback(() => {
    const current = dataRef.current
    if (loadingRef.current || current.nextOffset === null) {
      return
    }
    fetchNext(current.nextOffset)
  }, [fetchNext])

  const retry = useCallback(() => {
    const current = dataRef.current
    if (current.photos.length === 0) {
      loadPrefix(initialPagesRef.current, false)
      return
    }
    if (current.nextOffset !== null) {
      fetchNext(current.nextOffset)
    }
  }, [loadPrefix, fetchNext])

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
    totalRanked: data.totalRanked,
    status,
    loadingMore: data.loading && !data.initial,
    moreError: data.error && !data.initial,
    hasMore: data.nextOffset !== null,
    mode: data.mode,
    degraded: data.degraded,
    unknownTokens: data.unknownTokens,
    notices: data.notices,
    reloading: data.reloading,
    loadMore,
    retry,
  }
}
