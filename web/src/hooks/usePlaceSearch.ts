import { useEffect, useRef, useState } from 'react'

import { ApiError } from '../services/auth'
import { type Place, searchPlaces } from '../services/map'

/**
 * Default debounce before a typed place name reaches the backend. Every uncached
 * lookup is metered mapy.com credits, so this is the client half of the throttle
 * (the server caches and rate-limits regardless): long enough that typing
 * "Veselí nad Moravou" costs one lookup rather than eighteen, short enough that
 * the dropdown still feels like it is keeping up.
 */
const DEFAULT_DEBOUNCE_MS = 300

/**
 * The shortest query worth sending. One letter matches half the country and is
 * never what someone means to search for — it is just a keystroke on the way to a
 * real name, and paying a credit for it is pure waste.
 */
const MIN_QUERY_LENGTH = 2

/** How many suggestions to ask for: enough to disambiguate, few enough to scan. */
const SUGGESTION_LIMIT = 6

/**
 * The HTTP statuses that mean "the map provider side is broken" rather than "your
 * request was": our key was rejected (424), the provider errored (502) or is
 * unreachable / unconfigured (503). Retrying the same keystroke will not help, so
 * they get their own state and their own message.
 */
const UNAVAILABLE_STATUSES = [424, 502, 503]

/**
 * Lifecycle of a place search: `idle` (nothing typed, or too little to search),
 * `loading`, `ready` (with suggestions, possibly none), `error` (a lookup that may
 * be worth retrying) or `unavailable` (place search is off or the provider is
 * down — the editor's other ways in still work).
 */
export type PlaceSearchStatus = 'idle' | 'loading' | 'ready' | 'error' | 'unavailable'

/** State returned by {@link usePlaceSearch}. */
export interface PlaceSearchState {
  status: PlaceSearchStatus
  /** The latest suggestions; empty unless the status is `ready`. */
  places: Place[]
}

/**
 * Debounces `query` and searches places by name ({@link searchPlaces}) once it
 * settles to something long enough to be worth a lookup. A blank or one-character
 * query is `idle` and fires nothing. In-flight requests are aborted and stale
 * responses ignored on every change and on unmount, so fast typing never races a
 * slow answer onto the screen.
 *
 * Mirrors {@link import('./useGlobalSearch').useGlobalSearch}, with one
 * difference that matters: a place lookup costs real money, so this one also
 * refuses to search a query too short to mean anything.
 */
export function usePlaceSearch(query: string, debounceMs = DEFAULT_DEBOUNCE_MS): PlaceSearchState {
  const trimmed = query.trim()
  const [state, setState] = useState<PlaceSearchState>({ status: 'idle', places: [] })
  const latestRequest = useRef(0)

  useEffect(() => {
    // Too short to be a place name: cancel any pending work and fetch nothing.
    if (trimmed.length < MIN_QUERY_LENGTH) {
      latestRequest.current += 1
      setState({ status: 'idle', places: [] })
      return
    }

    const requestId = latestRequest.current + 1
    latestRequest.current = requestId
    const controller = new AbortController()

    const timer = setTimeout(() => {
      setState((prev) => ({ status: 'loading', places: prev.places }))
      searchPlaces(trimmed, SUGGESTION_LIMIT, controller.signal)
        .then((places) => {
          if (latestRequest.current !== requestId) {
            return
          }
          setState({ status: 'ready', places })
        })
        .catch((err: unknown) => {
          if (controller.signal.aborted || latestRequest.current !== requestId) {
            return
          }
          if (err instanceof DOMException && err.name === 'AbortError') {
            return
          }
          const unavailable = err instanceof ApiError && UNAVAILABLE_STATUSES.includes(err.status)
          setState({ status: unavailable ? 'unavailable' : 'error', places: [] })
        })
    }, debounceMs)

    return () => {
      clearTimeout(timer)
      controller.abort()
    }
  }, [trimmed, debounceMs])

  return state
}
