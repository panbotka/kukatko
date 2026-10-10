import { useEffect, useMemo, useRef, useState } from 'react'

import {
  batchBytes,
  INITIAL_THROUGHPUT,
  remainingSeconds,
  sampleThroughput,
} from '../lib/uploadEta'
import { awaitingVerdicts, batchCompletion } from '../lib/uploadProgress'

import { type UploadQueueItem } from './useUploadQueue'

/** How often the throughput is sampled while a batch runs. */
export const ETA_SAMPLE_MS = 1_000

/** What {@link useUploadEta} reports. */
export interface UploadEta {
  /**
   * The batch's completion as a fraction in `[0, 1]`, weighted by bytes and live
   * with every progress event; sending fills 90 % of a file's share and its
   * verdict the rest (`lib/uploadProgress`), so it is 1 only once every file has
   * one.
   */
  fraction: number
  /** True once every byte has gone and the batch only waits for the server's verdicts. */
  processing: boolean
  /** Seconds left at the measured rate, `null` while there is no honest estimate yet. */
  seconds: number | null
}

/**
 * The batch's progress and its remaining time. The fraction follows the queue
 * directly; the estimate is sampled once a second while `active` through
 * the pure estimator in `lib/uploadEta` (sliding window + moving average), and
 * forgotten when the batch stops, so the next one starts at "calculating".
 */
export function useUploadEta(items: readonly UploadQueueItem[], active: boolean): UploadEta {
  const bytes = useMemo(() => batchBytes(items), [items])
  const bytesRef = useRef(bytes)
  bytesRef.current = bytes
  const [seconds, setSeconds] = useState<number | null>(null)

  useEffect(() => {
    if (!active) {
      return
    }
    let state = sampleThroughput(INITIAL_THROUGHPUT, Date.now(), bytesRef.current.done)
    const timer = window.setInterval(() => {
      const { done, total } = bytesRef.current
      state = sampleThroughput(state, Date.now(), done)
      setSeconds(remainingSeconds(state, total - done))
    }, ETA_SAMPLE_MS)
    return () => {
      window.clearInterval(timer)
      setSeconds(null)
    }
  }, [active])

  const fraction = useMemo(() => batchCompletion(items, 'bytes'), [items])
  const processing = useMemo(() => awaitingVerdicts(items), [items])
  return { fraction, processing, seconds }
}
