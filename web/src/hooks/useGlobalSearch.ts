import { useEffect, useRef, useState } from 'react'

import { type GlobalSearchResult, globalSearch } from '../services/search'

/** Default debounce before a typed query triggers a grouped global search. */
const DEFAULT_DEBOUNCE_MS = 250

/**
 * Lifecycle of a grouped global search: `idle` (no/empty query — nothing
 * fetched), `loading`, `ready` (with a result) or `error`.
 */
export type GlobalSearchStatus = 'idle' | 'loading' | 'ready' | 'error'

/** State returned by {@link useGlobalSearch}. */
export interface GlobalSearchState {
  status: GlobalSearchStatus
  /** The latest result, or `null` until one arrives (idle/loading/error). */
  result: GlobalSearchResult | null
}

/**
 * Debounces `query` and runs a grouped global search
 * ({@link globalSearch}) when it settles to a non-empty value. An empty or
 * whitespace-only query resolves to `idle` with no request (the backend would
 * reject it with 400 anyway). In-flight requests are aborted and stale responses
 * ignored on every change and on unmount, so fast typing never races.
 *
 * Both the navbar type-ahead and the search page's cross-entity sections use
 * this: the navbar passes its raw input (relying on the internal debounce), while
 * the search page passes its already-committed URL query.
 */
export function useGlobalSearch(query: string, debounceMs = DEFAULT_DEBOUNCE_MS): GlobalSearchState {
  const trimmed = query.trim()
  const [state, setState] = useState<GlobalSearchState>({ status: 'idle', result: null })
  const latestRequest = useRef(0)

  useEffect(() => {
    // An empty query is idle: cancel any pending work and fetch nothing.
    if (trimmed === '') {
      latestRequest.current += 1
      setState({ status: 'idle', result: null })
      return
    }

    const requestId = latestRequest.current + 1
    latestRequest.current = requestId
    const controller = new AbortController()

    const timer = setTimeout(() => {
      setState((prev) => ({ status: 'loading', result: prev.result }))
      globalSearch(trimmed, controller.signal)
        .then((result) => {
          if (latestRequest.current !== requestId) {
            return
          }
          setState({ status: 'ready', result })
        })
        .catch((err: unknown) => {
          if (controller.signal.aborted || latestRequest.current !== requestId) {
            return
          }
          if (err instanceof DOMException && err.name === 'AbortError') {
            return
          }
          setState({ status: 'error', result: null })
        })
    }, debounceMs)

    return () => {
      clearTimeout(timer)
      controller.abort()
    }
  }, [trimmed, debounceMs])

  return state
}
