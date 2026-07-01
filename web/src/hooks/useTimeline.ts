import { useEffect, useRef, useState } from 'react'

import { fetchTimeline, type PhotoListParams, type TimelineBucket } from '../services/photos'

/** Lifecycle of a timeline fetch. */
export type TimelineStatus = 'loading' | 'error' | 'ready'

/** State returned by {@link useTimeline}. */
export interface UseTimelineResult {
  /** Month buckets in newest-first order (empty until ready / on error). */
  buckets: TimelineBucket[]
  /** Total matching photos (including undated ones outside any bucket). */
  total: number
  status: TimelineStatus
}

/**
 * Loads the month date-histogram for the given filters, refetching whenever the
 * params change. The whole timeline comes back in one request (no pagination),
 * so this mirrors the map feed's single-shot loader: in-flight requests are
 * aborted on change/unmount and stale responses are ignored, so rapid filter
 * changes never race.
 *
 * `params` should be memoised by the caller so the effect only re-runs when the
 * filters actually change.
 */
export function useTimeline(params: PhotoListParams): UseTimelineResult {
  const [buckets, setBuckets] = useState<TimelineBucket[]>([])
  const [total, setTotal] = useState(0)
  const [status, setStatus] = useState<TimelineStatus>('loading')
  const latestRequest = useRef(0)

  useEffect(() => {
    const requestId = latestRequest.current + 1
    latestRequest.current = requestId
    const controller = new AbortController()
    setStatus('loading')

    fetchTimeline(params, controller.signal)
      .then((timeline) => {
        if (latestRequest.current !== requestId) {
          return
        }
        setBuckets(timeline.buckets)
        setTotal(timeline.total)
        setStatus('ready')
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted || latestRequest.current !== requestId) {
          return
        }
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setStatus('error')
      })

    return () => {
      controller.abort()
    }
  }, [params])

  return { buckets, total, status }
}
