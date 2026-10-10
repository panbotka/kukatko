import { useEffect, useRef } from 'react'

/** The first automatic retry waits this long after an interruption… */
export const RESUME_BASE_DELAY_MS = 3_000
/** …each further one twice as long, but never longer than this. */
export const RESUME_MAX_DELAY_MS = 60_000

/** Options for {@link useUploadResilience}. */
export interface UploadResilienceOptions {
  /** True while the batch runs or waits to resume: the screen is kept awake. */
  active: boolean
  /** True while some file waits for an automatic re-send after an interruption. */
  interrupted: boolean
  /** Whether the browser is online (`useOnline`); nothing is retried while it is not. */
  online: boolean
  /** Re-sends the interrupted files (the queue's `retryInterrupted`); read through a ref. */
  onResume: () => void
}

/** The delay before automatic retry number `attempt` (0-based). */
export function resumeDelay(attempt: number): number {
  return Math.min(RESUME_MAX_DELAY_MS, RESUME_BASE_DELAY_MS * 2 ** attempt)
}

/** True when the page is the one the user is looking at. */
function pageVisible(): boolean {
  return document.visibilityState === 'visible'
}

/**
 * Keeps an upload going through what a phone does to a page left alone.
 *
 * **The screen stays awake** while `active`: a Screen Wake Lock is taken, taken
 * again whenever the page comes back to the foreground (the browser drops it on
 * every hide), and released once the batch is over. A browser without the API —
 * or one that refuses it (battery saver) — simply does not get one; nothing is
 * shown about it.
 *
 * **Interrupted files resume by themselves**: when the page returns to the
 * foreground or the network comes back (`online`), and on a backing-off timer
 * while neither happens (a network blip fires no event at all), `onResume` is
 * called while some file waits. Re-sending is safe because the server
 * deduplicates on the content hash, and an upload the server finished without
 * its client is repaired by the re-send. Nothing is retried while offline — the
 * timer would only burn through attempts that cannot succeed.
 */
export function useUploadResilience({
  active,
  interrupted,
  online,
  onResume,
}: UploadResilienceOptions): void {
  const onResumeRef = useRef(onResume)
  onResumeRef.current = onResume
  const interruptedRef = useRef(interrupted)
  interruptedRef.current = interrupted

  // The wake lock: held while active, re-taken on every return to the page.
  useEffect(() => {
    if (!active || !('wakeLock' in navigator)) {
      return
    }
    let sentinel: WakeLockSentinel | null = null
    let disposed = false
    let requesting = false

    const acquire = (): void => {
      if (disposed || requesting || !pageVisible() || (sentinel !== null && !sentinel.released)) {
        return
      }
      requesting = true
      navigator.wakeLock
        .request('screen')
        .then((lock) => {
          requesting = false
          if (disposed) {
            void lock.release().catch(() => undefined)
            return
          }
          sentinel = lock
        })
        .catch(() => {
          // Refused (battery saver, no permission, page hidden meanwhile): the
          // upload goes on without it; the next return to the page tries again.
          requesting = false
        })
    }

    const onVisibility = (): void => {
      acquire()
    }
    acquire()
    document.addEventListener('visibilitychange', onVisibility)
    return () => {
      disposed = true
      document.removeEventListener('visibilitychange', onVisibility)
      if (sentinel !== null && !sentinel.released) {
        void sentinel.release().catch(() => undefined)
      }
    }
  }, [active])

  // Back to the page, or back online: resume what the interruption stopped.
  useEffect(() => {
    const resume = (): void => {
      if (interruptedRef.current && navigator.onLine && pageVisible()) {
        onResumeRef.current()
      }
    }
    document.addEventListener('visibilitychange', resume)
    window.addEventListener('online', resume)
    return () => {
      document.removeEventListener('visibilitychange', resume)
      window.removeEventListener('online', resume)
    }
  }, [])

  // A blip with no event: retry on a timer that backs off while it keeps failing.
  // The count of attempts survives the file briefly going back to the queue (a
  // retry that fails again must wait longer), and starts over once the batch has
  // gone two maximum delays without an interruption.
  const attempts = useRef(0)
  const lastAttemptAt = useRef(0)
  useEffect(() => {
    if (!interrupted || !online) {
      return
    }
    const now = Date.now()
    if (now - lastAttemptAt.current > 2 * RESUME_MAX_DELAY_MS) {
      attempts.current = 0
    }
    const timer = window.setTimeout(() => {
      attempts.current += 1
      lastAttemptAt.current = Date.now()
      onResumeRef.current()
    }, resumeDelay(attempts.current))
    return () => {
      window.clearTimeout(timer)
    }
  }, [interrupted, online])
}
