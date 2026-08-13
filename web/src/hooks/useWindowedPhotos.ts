import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { fetchPhotos, type Photo, type PhotoListParams } from '../services/photos'

import { type ListStatus, PAGE_SIZE } from './usePaginatedPhotos'

/**
 * Extra pages fetched on each side of the visible range, so a slow scroll meets
 * loaded photos rather than placeholders. One page (100 photos) is several
 * screens of tiles at any density.
 */
export const WINDOW_PREFETCH_PAGES = 1

/**
 * Upper bound on the pages kept in memory. Reaching the oldest photo of a 20 000
 * item library used to mean holding all 20 000 of them; the window keeps at most
 * this many pages — the ones nearest the visible range — and drops the rest,
 * which the grid never had mounted anyway.
 */
export const WINDOW_MAX_PAGES = 24

/**
 * How many times a page is refetched after a failure before the window gives up
 * on it and surfaces the error. A failed page is retried on the next range
 * change — any scroll — so a blip heals itself; the cap is what stops an offline
 * backend from being hammered once per scroll event.
 */
export const WINDOW_MAX_ATTEMPTS = 3

/** Result of {@link useWindowedPhotos}. */
export interface UseWindowedPhotosResult {
  /**
   * One slot per matching photo, in server order: the photo once its page is
   * loaded, `undefined` while it is not. The array is always `total` long, so an
   * index in it is the photo's absolute position in the result — which is what
   * makes a jump to an arbitrary position possible without loading anything in
   * between.
   */
  photos: readonly (Photo | undefined)[]
  /** Total number of photos matching the current query. */
  total: number
  /** Status of the first load (drives the page-level loading/error UI). */
  status: ListStatus
  /** True when a page fetch failed after the first one succeeded. */
  moreError: boolean
  /**
   * Filter-shaped `q` tokens the query language did not understand. They
   * degraded to free text server-side, so the grid below is a real (usually
   * empty) result rather than an error — the caller shows a hint so a mistyped
   * key does not read as "not in the library".
   */
  unknownTokens: string[]
  /**
   * Machine-readable reasons the page is what it is, as the backend reported
   * them in `notices` — currently only `person_me_unlinked`, an empty grid
   * because the caller asked for `person:me` without their account having said
   * which person that is. The caller turns each code into a sentence.
   */
  notices: string[]
  /**
   * Declares which absolute index range the grid is showing; the hook loads the
   * pages covering it (plus {@link WINDOW_PREFETCH_PAGES} on each side) and drops
   * pages that have drifted out of the window. Cheap and idempotent — call it on
   * every range change.
   */
  ensureRange: (start: number, end: number) => void
  /** Re-runs the failed request(s): the first load, or the failed pages. */
  retry: () => void
}

/** Options for {@link useWindowedPhotos}. */
export interface UseWindowedPhotosOptions {
  /**
   * A reload token. Changing it refetches the pages currently loaded, in the
   * background: the photos stay mounted and `status` stays `ready`, so a bulk
   * edit reflects in place instead of blanking the grid.
   */
  reloadKey?: string
}

/** Internal state; every field is replaced as a whole on each update. */
interface WindowState {
  total: number
  pages: ReadonlyMap<number, Photo[]>
  status: ListStatus
  moreError: boolean
  unknownTokens: string[]
  notices: string[]
}

const INITIAL: WindowState = {
  total: 0,
  pages: new Map<number, Photo[]>(),
  status: 'loading',
  moreError: false,
  unknownTokens: [],
  notices: [],
}

/**
 * Drops the pages furthest from the requested page range until at most
 * {@link WINDOW_MAX_PAGES} remain, so memory stays bounded however far the reader
 * travels. Returns the map unchanged when nothing has to go.
 */
function evict(
  pages: ReadonlyMap<number, Photo[]>,
  first: number,
  last: number,
): ReadonlyMap<number, Photo[]> {
  if (pages.size <= WINDOW_MAX_PAGES) {
    return pages
  }
  const distance = (page: number): number =>
    page < first ? first - page : page > last ? page - last : 0
  const kept = [...pages.keys()]
    .sort((a, b) => distance(a) - distance(b))
    .slice(0, WINDOW_MAX_PAGES)
  const next = new Map<number, Photo[]>()
  for (const page of kept) {
    const photos = pages.get(page)
    if (photos !== undefined) {
      next.set(page, photos)
    }
  }
  return next
}

/**
 * Reports whether some page has failed {@link WINDOW_MAX_ATTEMPTS} times — the
 * point at which the window stops retrying it by itself and the grid has to
 * offer the reader a retry.
 */
function exhausted(attempts: ReadonlyMap<number, number>): boolean {
  for (const count of attempts.values()) {
    if (count >= WINDOW_MAX_ATTEMPTS) {
      return true
    }
  }
  return false
}

/**
 * Drives the library grid as a *window* over the result rather than as an
 * ever-growing prefix of it. The first request reports the `total`, which fixes
 * the grid's item count; from then on only the pages covering the visible range
 * are fetched, and every index in {@link UseWindowedPhotosResult.photos} is the
 * photo's absolute position in the result.
 *
 * That is what makes the timeline scrubber's jump cost independent of distance:
 * jumping to 2011 in a 20 000 photo library scrolls to the index and fetches the
 * one page that lands there, instead of paging through the 19 000 photos in
 * between (200-odd sequential requests, all of them kept in memory). Slots whose
 * page is not loaded read as `undefined`, which the grid renders as a placeholder
 * tile until it arrives.
 *
 * Requests are aborted on query change and on unmount, and responses from a
 * superseded query are dropped, so rapid filter changes never mix results.
 * `params` should be memoised by the caller; the hook keys its reset on the
 * params' *contents*, so a fresh object with the same query does not refetch.
 */
export function useWindowedPhotos(
  params: PhotoListParams,
  options: UseWindowedPhotosOptions = {},
): UseWindowedPhotosResult {
  const reloadKey = options.reloadKey ?? ''
  const queryKey = useMemo(() => JSON.stringify(params), [params])

  const [state, setState] = useState<WindowState>(INITIAL)

  const paramsRef = useRef(params)
  paramsRef.current = params
  const stateRef = useRef(state)
  stateRef.current = state
  // Bumped on every query change; a response carrying an older generation is a
  // stale answer to a query nobody is showing any more and is dropped.
  const generationRef = useRef(0)
  const controllersRef = useRef(new Map<number, AbortController>())
  // Failed attempts per page. A page is skipped only once it has run out of
  // them; until then the next range change retries it.
  const attemptsRef = useRef(new Map<number, number>())
  // The page range last asked for, so eviction knows what to keep and a retry
  // knows what to re-request.
  const rangeRef = useRef({ first: 0, last: 0 })

  /** Fetches one page, replacing whatever that page slot held. */
  const load = useCallback((page: number) => {
    const generation = generationRef.current
    controllersRef.current.get(page)?.abort()
    const controller = new AbortController()
    controllersRef.current.set(page, controller)

    fetchPhotos(
      { ...paramsRef.current, limit: PAGE_SIZE, offset: page * PAGE_SIZE },
      controller.signal,
    )
      .then((res) => {
        if (generation !== generationRef.current) {
          return
        }
        controllersRef.current.delete(page)
        attemptsRef.current.delete(page)
        setState((prev) => {
          const pages = new Map(prev.pages)
          pages.set(page, res.photos)
          return {
            total: res.total,
            pages: evict(pages, rangeRef.current.first, rangeRef.current.last),
            status: 'ready',
            moreError: exhausted(attemptsRef.current),
            // Every page of one query carries the same verdict on `q`, so
            // whichever answers last is as good as the first.
            unknownTokens: res.unknown_tokens ?? [],
            notices: res.notices ?? [],
          }
        })
      })
      .catch((err: unknown) => {
        if (generation !== generationRef.current) {
          return
        }
        if (err instanceof DOMException && err.name === 'AbortError') {
          // Whoever aborted already released the slot; a cancelled page is not a
          // failed one, so it costs no attempt either.
          return
        }
        controllersRef.current.delete(page)
        attemptsRef.current.set(page, (attemptsRef.current.get(page) ?? 0) + 1)
        setState((prev) => ({
          ...prev,
          // Only the very first load has no photos to fall back on, so only it
          // turns the whole page into an error state.
          status: prev.status === 'loading' ? 'error' : prev.status,
          moreError: prev.status !== 'loading' && exhausted(attemptsRef.current),
        }))
      })
  }, [])

  const ensureRange = useCallback(
    (start: number, end: number) => {
      const total = stateRef.current.total
      const first = Math.max(0, Math.floor(Math.max(0, start) / PAGE_SIZE) - WINDOW_PREFETCH_PAGES)
      const wanted = Math.floor(Math.max(0, start, end) / PAGE_SIZE) + WINDOW_PREFETCH_PAGES
      const lastPage = total > 0 ? Math.floor((total - 1) / PAGE_SIZE) : 0
      const last = Math.min(wanted, lastPage)
      rangeRef.current = { first, last }
      // Drop what the reader has already travelled past. A jump across the whole
      // library sweeps the range through a dozen intermediate positions before it
      // settles, and each one that stays in flight is bandwidth taken from the
      // page actually being waited for.
      for (const [page, controller] of controllersRef.current) {
        if (page < first || page > last) {
          controllersRef.current.delete(page)
          controller.abort()
        }
      }
      for (let page = first; page <= last; page++) {
        if (
          stateRef.current.pages.has(page) ||
          controllersRef.current.has(page) ||
          (attemptsRef.current.get(page) ?? 0) >= WINDOW_MAX_ATTEMPTS
        ) {
          continue
        }
        load(page)
      }
    },
    [load],
  )

  // Reset and load the first page whenever the query changes. The first response
  // is what supplies `total`, and with it the grid's full index space.
  useEffect(() => {
    // The maps live for the hook's whole life, so holding them locally is safe
    // and keeps the cleanup from reading a ref it may no longer own.
    const controllers = controllersRef.current
    generationRef.current++
    for (const controller of controllers.values()) {
      controller.abort()
    }
    controllers.clear()
    attemptsRef.current.clear()
    rangeRef.current = { first: 0, last: 0 }
    stateRef.current = INITIAL
    setState(INITIAL)
    load(0)
    return () => {
      // Aborting is enough to silence what is still in flight: an aborted fetch
      // lands in the catch as an AbortError, which is ignored there.
      for (const controller of controllers.values()) {
        controller.abort()
      }
      controllers.clear()
    }
  }, [queryKey, load])

  // A reloadKey bump refetches exactly the pages the reader currently has, in
  // the background: the tiles stay mounted while the fresh data replaces them.
  // The ref starts at the initial value so neither the first mount nor React
  // StrictMode's double-invoked mount effect fires a spurious reload.
  const lastReloadKeyRef = useRef(reloadKey)
  useEffect(() => {
    if (lastReloadKeyRef.current === reloadKey) {
      return
    }
    lastReloadKeyRef.current = reloadKey
    attemptsRef.current.clear()
    for (const page of stateRef.current.pages.keys()) {
      load(page)
    }
  }, [reloadKey, load])

  const retry = useCallback(() => {
    attemptsRef.current.clear()
    setState((prev) => ({ ...prev, moreError: false }))
    if (stateRef.current.status === 'error') {
      setState((prev) => ({ ...prev, status: 'loading' }))
      load(0)
      return
    }
    const { first, last } = rangeRef.current
    for (let page = first; page <= last; page++) {
      if (stateRef.current.pages.has(page) || controllersRef.current.has(page)) {
        continue
      }
      load(page)
    }
  }, [load])

  const photos = useMemo(() => {
    const slots = new Array<Photo | undefined>(state.total).fill(undefined)
    for (const [page, loaded] of state.pages) {
      const base = page * PAGE_SIZE
      for (let i = 0; i < loaded.length && base + i < state.total; i++) {
        slots[base + i] = loaded[i]
      }
    }
    return slots
  }, [state.total, state.pages])

  return {
    photos,
    total: state.total,
    status: state.status,
    moreError: state.moreError,
    unknownTokens: state.unknownTokens,
    notices: state.notices,
    ensureRange,
    retry,
  }
}
