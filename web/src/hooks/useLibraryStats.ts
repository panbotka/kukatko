import { useCallback, useEffect, useState } from 'react'

import { fetchLibraryStats, type LibraryStats } from '../services/system'

/** Fetch lifecycle of the library statistics. */
export type LibraryStatsState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; data: LibraryStats }

/** The state plus a stable retry callback; see {@link useLibraryStats}. */
export interface UseLibraryStatsResult {
  state: LibraryStatsState
  reload: () => void
}

/**
 * Loads the instance-wide library counts from `GET /system/stats`, the single
 * source shared by the statistics page and the System page's Library section
 * (the backend memoises the aggregation, so two readers cost one query).
 *
 * A failure is surfaced as an explicit `error` state rather than swallowed into
 * zeroes: an empty library and an unavailable count must not look the same. An
 * aborted request (unmount, or a retry superseding the previous fetch) is not a
 * failure and leaves the state alone. `reload` re-runs the fetch, so the caller
 * can offer a retry.
 *
 * @param enabled Whether to fetch at all; false leaves the hook loading and
 *   issues no request (used to skip the call for a role that cannot read it).
 */
export function useLibraryStats(enabled = true): UseLibraryStatsResult {
  const [state, setState] = useState<LibraryStatsState>({ status: 'loading' })
  const [attempt, setAttempt] = useState(0)

  const reload = useCallback(() => {
    setAttempt((previous) => previous + 1)
  }, [])

  useEffect(() => {
    if (!enabled) {
      return
    }
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchLibraryStats(controller.signal)
      .then((data) => {
        setState({ status: 'ready', data })
      })
      .catch(() => {
        // A cancelled fetch (unmount, or a retry) is not a failure; the next run
        // — or nothing, on unmount — owns the state from here.
        if (controller.signal.aborted) {
          return
        }
        setState({ status: 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [enabled, attempt])

  return { state, reload }
}
