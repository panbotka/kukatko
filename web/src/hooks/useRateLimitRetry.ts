import { useCallback, useEffect, useRef, useState } from 'react'

import { RATE_LIMIT_MAX_RETRIES, isRateLimited, rateLimitDelayMs } from '../lib/rateLimit'

/** What {@link useRateLimitRetry} hands a page. */
export interface RateLimitRetry {
  /**
   * True from the first 429 until the page's request finally gets through
   * ({@link reset}) or the hook gives up — the page shows
   * {@link import('../components/RateLimitNotice').RateLimitNotice} meanwhile,
   * in place of (or next to) its loading state.
   */
  waiting: boolean
  /**
   * Hands the hook a failed request's error. On a 429 under the retry cap it
   * schedules `retry` after the backoff and answers `true` — the page shows
   * the notice and nothing else. For any other error, or once the cap is spent,
   * it forgets the run, clears {@link waiting} and answers `false`: the page
   * shows its ordinary failure state for `err`.
   */
  handle: (err: unknown, retry: () => void) => boolean
  /**
   * Forgets the run and cancels a scheduled retry. Call it when the request
   * succeeded, and before a new user action replaces the one being waited for.
   */
  reset: () => void
}

/**
 * Waits out the server's rate limiter for one kind of request on a page: a 429
 * turns into a calm "too many requests, trying again in a moment" and an
 * automatic retry on the backoff of `lib/rateLimit` instead of a broken-looking
 * failure state. Each page keeps one instance per independent request (the
 * duplicates list keeps one for the first page and one for "load more"), and
 * the scheduled retry is cancelled when the page unmounts.
 */
export function useRateLimitRetry(): RateLimitRetry {
  const [waiting, setWaiting] = useState(false)
  const attempt = useRef(0)
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const cancel = useCallback(() => {
    if (timer.current !== null) {
      clearTimeout(timer.current)
      timer.current = null
    }
  }, [])

  const reset = useCallback(() => {
    cancel()
    attempt.current = 0
    setWaiting(false)
  }, [cancel])

  const handle = useCallback(
    (err: unknown, retry: () => void): boolean => {
      cancel()
      if (!isRateLimited(err) || attempt.current >= RATE_LIMIT_MAX_RETRIES) {
        attempt.current = 0
        setWaiting(false)
        return false
      }
      const delay = rateLimitDelayMs(err, attempt.current)
      attempt.current += 1
      setWaiting(true)
      timer.current = setTimeout(() => {
        timer.current = null
        retry()
      }, delay)
      return true
    },
    [cancel],
  )

  useEffect(() => cancel, [cancel])

  return { waiting, handle, reset }
}
