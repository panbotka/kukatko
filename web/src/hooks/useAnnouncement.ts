import { useEffect, useState } from 'react'

import { fetchAnnouncement, type Announcement } from '../services/announcement'

/**
 * How often the banner re-polls the current announcement. A minute is frequent
 * enough that a freshly published message appears without a reload, yet light
 * enough to be negligible next to the rest of the app's traffic.
 */
const POLL_INTERVAL_MS = 60_000

/**
 * Polls the announcement of the project `slug` — the one the app works in —
 * that backs the top-of-app banner. Every project has its own, so switching
 * projects drops the previous project's message at once and fetches the new
 * one's; a `null` slug (an account with no project to enter) asks nothing.
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
export function useAnnouncement(slug: string | null): Announcement | null {
  // The answer is kept with the project it belongs to, so a changed slug reads
  // as "nothing yet" until its own answer arrives — another project's banner is
  // never shown, not even for the length of a request.
  const [answer, setAnswer] = useState<{ slug: string; announcement: Announcement | null } | null>(
    null,
  )

  useEffect(() => {
    if (slug === null) {
      return
    }
    const project = slug
    const setAnnouncement = (announcement: Announcement | null) => {
      setAnswer({ slug: project, announcement })
    }
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
  }, [slug])

  return slug !== null && answer?.slug === slug ? answer.announcement : null
}
