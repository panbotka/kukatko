import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { useAuth } from '../auth/AuthContext'
import { subscribeTaskChanges } from '../lib/taskChanges'
import { useProjectSlug } from '../project/ProjectContext'
import { fetchTaskSummary, type TaskSummary } from '../services/tasks'

import { TaskSummaryContext, type TaskSummaryState } from './TaskSummaryContext'

/**
 * How often the counts are refetched while the app is open. The queue moves at
 * the pace of a conversation, not a ticker, and the writes made from this very
 * browser refresh the counts on their own (see `lib/taskChanges`), so the poll
 * only has to catch what *other* people did — for which a minute is plenty.
 */
export const REFRESH_INTERVAL_MS = 60_000

/**
 * Provides the queue's counts (`GET /api/v1/tasks/summary`) to the shell: the
 * navigation badge, the hamburger's dot and the queue page's chips all read
 * from here, so they can never disagree with one another.
 *
 * **Only for a signed-in reader.** The endpoint is behind auth and the badge is
 * about *your* move, so nothing is fetched — and nothing is held — while the
 * session is anything but authenticated; signing out clears the counts at once.
 *
 * Fetched once on mount, then at most every {@link REFRESH_INTERVAL_MS} while
 * the tab is visible (a hidden tab pauses the timer and refetches on return),
 * and immediately after any task write made from this browser — a comment, a
 * state change, an assignment, a new task — which the services announce on the
 * task-change bus. A failed fetch clears the counts, so the badge hides silently
 * rather than showing a number nobody can vouch for.
 *
 * The counts belong to one project: switching to another aborts the request in
 * flight, drops the old counts and fetches the new project's.
 */
export function TaskSummaryProvider({ children }: { children: ReactNode }) {
  const { status } = useAuth()
  const enabled = status === 'authenticated'
  // Read only as the polling effect's dependency: the request itself goes to
  // whatever project the API is pointed at, which the provider keeps in step.
  const slug = useProjectSlug()
  const [summary, setSummary] = useState<TaskSummary | null>(null)
  // The in-flight request, so a refresh supersedes a poll (and vice versa)
  // instead of racing it: only the newest request may set state.
  const controller = useRef<AbortController | null>(null)

  const load = useCallback(() => {
    if (!enabled || document.hidden) {
      return
    }
    controller.current?.abort()
    const current = new AbortController()
    controller.current = current
    void fetchTaskSummary(current.signal)
      .then((next) => {
        if (controller.current === current) {
          setSummary(next)
        }
      })
      .catch(() => {
        // Silent: an unknown count hides the badge, it never breaks a page.
        if (controller.current === current) {
          setSummary(null)
        }
      })
  }, [enabled])

  useEffect(() => {
    if (!enabled) {
      controller.current?.abort()
      controller.current = null
      setSummary(null)
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
    const unsubscribe = subscribeTaskChanges(load)

    return () => {
      unsubscribe()
      stopTimer()
      document.removeEventListener('visibilitychange', onVisibilityChange)
      controller.current?.abort()
      controller.current = null
      // The counts were the old project's (or the old session's): never show
      // them in the next one while its own are on the way.
      setSummary(null)
    }
  }, [enabled, load, slug])

  const value = useMemo<TaskSummaryState>(() => ({ summary, refresh: load }), [summary, load])

  return <TaskSummaryContext.Provider value={value}>{children}</TaskSummaryContext.Provider>
}
