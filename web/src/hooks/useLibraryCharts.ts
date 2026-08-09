import { useCallback, useEffect, useState } from 'react'

import { fetchLibraryCharts, type LibraryCharts } from '../services/system'

/** Fetch lifecycle of the library chart series. */
export type LibraryChartsState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; data: LibraryCharts }

/** The state plus a stable retry callback; see {@link useLibraryCharts}. */
export interface UseLibraryChartsResult {
  state: LibraryChartsState
  reload: () => void
}

/**
 * Loads the chart series from `GET /system/stats/charts` — photos per year,
 * arrivals per month, the top cameras and the two storage breakdowns.
 *
 * It is a sibling of `useLibraryStats` rather than part of it: the counts are
 * cheap and the charts are not, so the two requests run side by side and the
 * numbers are never held up by the histograms. A failure is surfaced as an
 * explicit `error` state rather than swallowed into empty series, which would
 * draw as an empty library; an aborted request (unmount, or a retry superseding
 * the previous fetch) is not a failure and leaves the state alone. `reload`
 * re-runs the fetch, so the caller can offer a retry.
 *
 * @param enabled Whether to fetch at all; false leaves the hook loading and
 *   issues no request.
 */
export function useLibraryCharts(enabled = true): UseLibraryChartsResult {
  const [state, setState] = useState<LibraryChartsState>({ status: 'loading' })
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
    fetchLibraryCharts(controller.signal)
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
