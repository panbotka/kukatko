import { useEffect, useState } from 'react'

import { fetchAnnouncement, type Announcement } from '../services/announcement'

/**
 * How often the banner re-polls the current announcement. A minute is frequent
 * enough that a freshly published message appears without a reload, yet light
 * enough to be negligible next to the rest of the app's traffic.
 */
const POLL_INTERVAL_MS = 60_000

/**
 * Polls the single instance-wide announcement that backs the top-of-app banner.
 *
 * It fetches once on mount and then on a modest {@link POLL_INTERVAL_MS} interval,
 * pausing entirely while the browser tab is hidden and resuming with an immediate
 * fetch when it becomes visible again. Any failure is swallowed and the state is
 * cleared to null, so the banner simply hides rather than surfacing an error: the
 * shell must never break. Every timer and in-flight request is torn down on
 * unmount, so nothing outlives the hook.
 *
 * @returns The current announcement (its `message` may be empty when nothing is
 *   published), or null while loading or after a failure.
 */
export function useAnnouncement(): Announcement | null {
  const [announcement, setAnnouncement] = useState<Announcement | null>(null)

  useEffect(() => {
    let active = true
    let controller: AbortController | null = null
    let timer: number | null = null

    const load = () => {
      // Never fetch while the tab is hidden; becoming visible triggers a load.
      if (document.hidden) {
        return
      }
      controller?.abort()
      const current = new AbortController()
      controller = current
      void fetchAnnouncement(current.signal)
        .then((next) => {
          if (active && controller === current) {
            setAnnouncement(next)
          }
        })
        .catch(() => {
          // Silent: hide the banner on any failure so the shell never errors.
          if (active && controller === current) {
            setAnnouncement(null)
          }
        })
    }

    const startTimer = () => {
      timer ??= window.setInterval(load, POLL_INTERVAL_MS)
    }

    const stopTimer = () => {
      if (timer !== null) {
        window.clearInterval(timer)
        timer = null
      }
    }

    const onVisibilityChange = () => {
      if (document.hidden) {
        stopTimer()
      } else {
        load()
        startTimer()
      }
    }

    load()
    if (!document.hidden) {
      startTimer()
    }
    document.addEventListener('visibilitychange', onVisibilityChange)

    return () => {
      active = false
      controller?.abort()
      stopTimer()
      document.removeEventListener('visibilitychange', onVisibilityChange)
    }
  }, [])

  return announcement
}
