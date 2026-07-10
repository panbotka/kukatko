import { useEffect, useState } from 'react'

import { fetchJobStats, type JobStats } from '../services/import'

/** How often the badges refresh while the browser tab is visible. */
const REFRESH_INTERVAL_MS = 30_000

/**
 * Polls the admin-only job-queue statistics that back the footer badges.
 *
 * It only issues requests while `enabled` is true (i.e. for administrators),
 * refreshes on a modest {@link REFRESH_INTERVAL_MS} interval, and pauses
 * entirely while the browser tab is hidden — resuming with an immediate refresh
 * when the tab becomes visible again. A failing or slow request is swallowed and
 * the stats are cleared, so the caller simply hides the badges rather than
 * surfacing an error: the footer must never break a page. Every timer and
 * in-flight request is torn down when the component unmounts or `enabled` flips
 * off, so nothing outlives the hook.
 *
 * @param enabled Whether polling should run at all (false for non-admins).
 * @returns The latest job-queue stats, or null while loading, disabled, or failed.
 */
export function useJobStats(enabled: boolean): JobStats | null {
  const [stats, setStats] = useState<JobStats | null>(null)

  useEffect(() => {
    if (!enabled) {
      // Clear any stale stats left over from a previous admin session.
      setStats(null)
      return
    }

    let active = true
    let controller: AbortController | null = null
    let timer: number | null = null

    const load = () => {
      // Never fetch while the tab is hidden; becoming visible triggers a load.
      if (document.hidden) {
        return
      }
      // Abort any in-flight request and track the latest one so a superseded or
      // aborted response is ignored rather than clobbering fresher data.
      controller?.abort()
      const current = new AbortController()
      controller = current
      void fetchJobStats(current.signal)
        .then((next) => {
          if (active && controller === current) {
            setStats(next)
          }
        })
        .catch(() => {
          // Silent: hide the badges on any failure so the footer never errors.
          if (active && controller === current) {
            setStats(null)
          }
        })
    }

    const startTimer = () => {
      timer ??= window.setInterval(load, REFRESH_INTERVAL_MS)
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

    // Kick off immediately (unless hidden) and keep polling while visible.
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
  }, [enabled])

  return stats
}
