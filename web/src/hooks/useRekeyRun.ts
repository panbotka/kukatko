import { useCallback, useEffect, useRef, useState } from 'react'

import { type RekeySample, rekeyPollInterval, rekeyRate } from '../lib/rekeyProgress'
import { fetchRekeyRun, type RekeyRun, type RekeyState } from '../services/rekey'

/** What {@link useRekeyRun} reports. */
export interface RekeyWatch {
  /** `loading` until the first answer; `error` when the first read failed. */
  status: 'loading' | 'error' | 'ready'
  /** The project's latest run, or `null` when it never had one. */
  run: RekeyRun | null
  /** Photos per minute between the last two polls of a running move, else `null`. */
  ratePerMinute: number | null
  /** Shows a run an action just answered with, and polls at its cadence. */
  adopt: (run: RekeyRun) => void
  /** Asks again at once (the retry of a failed first read). */
  reload: () => void
}

/**
 * Watches the storage move of the project `uid` (`GET /projects/{uid}/rekey`):
 * one read at once, then a poll every 3 s while the move runs and every 15 s
 * otherwise — paused, failed, or no run yet (a move started from the CLI shows
 * up on its own). A done run never changes again, so the watch stops there.
 *
 * A hidden tab is nobody watching: the poll stands down while
 * `document.visibilityState` is `hidden` and asks at once when the tab comes
 * back. A failed poll after the first answer keeps the run on screen and simply
 * asks again on the next tick.
 *
 * The rate is the browser's own measurement — the moved-photo count of the last
 * two polls of a running move over the time between them — since the API
 * reports counts, not speed.
 */
export function useRekeyRun(uid: string): RekeyWatch {
  const [status, setStatus] = useState<RekeyWatch['status']>('loading')
  const [run, setRun] = useState<RekeyRun | null>(null)
  const [ratePerMinute, setRate] = useState<number | null>(null)

  // The loop's view of the run, read when it picks the next interval.
  const stateRef = useRef<RekeyState | null>(null)
  const sampleRef = useRef<RekeySample | null>(null)
  // Set by the running effect: re-arms the timer, or polls at once.
  const rescheduleRef = useRef<() => void>(() => undefined)
  const pollNowRef = useRef<() => void>(() => undefined)

  /** Records one answer: the run, its state for the loop, the rate. */
  const record = useCallback((next: RekeyRun | null) => {
    stateRef.current = next?.state ?? null
    setRun(next)
    setStatus('ready')
    if (next?.state !== 'running') {
      sampleRef.current = null
      setRate(null)
      return
    }
    const sample = { donePhotos: next.done_photos, at: Date.now() }
    setRate(rekeyRate(sampleRef.current, sample))
    sampleRef.current = sample
  }, [])

  useEffect(() => {
    let active = true
    let timer: number | null = null
    let inFlight = false
    const controller = new AbortController()
    stateRef.current = null
    sampleRef.current = null

    const stop = (): void => {
      if (timer !== null) {
        window.clearTimeout(timer)
        timer = null
      }
    }

    const schedule = (): void => {
      stop()
      if (!active || document.visibilityState === 'hidden') {
        return
      }
      const interval = rekeyPollInterval(stateRef.current)
      if (interval !== null) {
        timer = window.setTimeout(poll, interval)
      }
    }

    function poll(): void {
      if (!active || inFlight) {
        return
      }
      timer = null
      inFlight = true
      fetchRekeyRun(uid, controller.signal)
        .then((next) => {
          inFlight = false
          if (!active) {
            return
          }
          record(next)
          schedule()
        })
        .catch(() => {
          inFlight = false
          if (!active) {
            return
          }
          // Only the first read decides the card; later a failed poll is noise.
          setStatus((prev) => (prev === 'loading' ? 'error' : prev))
          schedule()
        })
    }

    const visibility = (): void => {
      if (document.visibilityState === 'hidden') {
        stop()
        return
      }
      if (timer === null && rekeyPollInterval(stateRef.current) !== null) {
        poll()
      }
    }

    rescheduleRef.current = schedule
    pollNowRef.current = poll
    document.addEventListener('visibilitychange', visibility)
    poll()
    return () => {
      active = false
      stop()
      controller.abort()
      document.removeEventListener('visibilitychange', visibility)
      rescheduleRef.current = () => undefined
      pollNowRef.current = () => undefined
    }
  }, [uid, record])

  const adopt = useCallback(
    (next: RekeyRun) => {
      record(next)
      rescheduleRef.current()
    },
    [record],
  )

  const reload = useCallback(() => {
    setStatus((prev) => (prev === 'error' ? 'loading' : prev))
    pollNowRef.current()
  }, [])

  return { status, run, ratePerMinute, adopt, reload }
}
