import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { useAuth } from '../auth/AuthContext'
import { fetchUnreadNotificationCount } from '../services/notifications'

import { NotificationInboxContext, type NotificationInboxState } from './NotificationInboxContext'

/**
 * How often the unread count is refetched while the tab is visible. A
 * notification is news, not a ticker: a minute is soon enough to notice one,
 * and the count is also refetched the moment the tab or window comes back.
 */
export const REFRESH_INTERVAL_MS = 60_000

/**
 * Provides the in-app inbox's unread count (`GET /api/v1/notifications/unread-count`)
 * to the shell — the bell's badge reads it, and so does anything that wants to
 * know whether there is news.
 *
 * **Only for a signed-in reader.** Nothing is fetched while the session is
 * anything but authenticated, and signing out clears the count at once.
 *
 * Fetched once on mount, then every {@link REFRESH_INTERVAL_MS} while the tab is
 * visible (a hidden tab pauses the timer), and immediately whenever the tab
 * becomes visible again or the window regains focus — the moment somebody comes
 * back is the moment the number matters. A failed fetch clears the count, so
 * the badge hides silently.
 */
export function NotificationInboxProvider({ children }: { children: ReactNode }) {
  const { status } = useAuth()
  const enabled = status === 'authenticated'
  const [unread, setUnread] = useState<number | null>(null)
  // The in-flight request, so a refresh supersedes a poll instead of racing it:
  // only the newest request may set state.
  const controller = useRef<AbortController | null>(null)

  const load = useCallback(() => {
    if (!enabled || document.hidden) {
      return
    }
    controller.current?.abort()
    const current = new AbortController()
    controller.current = current
    void fetchUnreadNotificationCount(current.signal)
      .then((next) => {
        if (controller.current === current) {
          setUnread(next)
        }
      })
      .catch(() => {
        // Silent: an unknown count hides the badge, it never breaks a page.
        if (controller.current === current) {
          setUnread(null)
        }
      })
  }, [enabled])

  useEffect(() => {
    if (!enabled) {
      controller.current?.abort()
      controller.current = null
      setUnread(null)
      return
    }
    let timer: number | null = null
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

    load()
    if (!document.hidden) {
      startTimer()
    }
    document.addEventListener('visibilitychange', onVisibilityChange)
    window.addEventListener('focus', load)

    return () => {
      stopTimer()
      document.removeEventListener('visibilitychange', onVisibilityChange)
      window.removeEventListener('focus', load)
      controller.current?.abort()
      controller.current = null
    }
  }, [enabled, load])

  const value = useMemo<NotificationInboxState>(() => ({ unread, refresh: load }), [unread, load])

  return (
    <NotificationInboxContext.Provider value={value}>{children}</NotificationInboxContext.Provider>
  )
}
