import { createContext, useContext } from 'react'

import { type TaskSummary } from '../services/tasks'

/**
 * The queue's counts as the shell holds them, plus the handle to refetch them.
 *
 * `summary` is null until the first response arrives, after a failed fetch, and
 * for anybody not signed in — and null means "draw nothing": the navigation
 * badge disappears rather than showing a stale or invented number, and the
 * queue's chips fall back to plain labels.
 */
export interface TaskSummaryState {
  summary: TaskSummary | null
  /**
   * Refetches the counts now. Pages call it on mount when they are about to
   * show the numbers; the services call it — through the task-change bus —
   * after every write from this browser. A no-op outside the provider.
   */
  refresh: () => void
}

/**
 * The safe default: nothing known, and nothing to do about it. A component
 * rendered outside a `TaskSummaryProvider` (a focused unit test, say) simply
 * shows no badge and no counts.
 */
export const TASK_SUMMARY_DEFAULT: TaskSummaryState = {
  summary: null,
  refresh: () => {
    // Nothing to refresh without a provider.
  },
}

/**
 * Provides the queue's counts. Like the capabilities context it carries a
 * non-null default and its hook never throws outside a provider: the counts are
 * a hint layered over the navigation, so a missing provider degrades to "no
 * badge" rather than crashing the tree.
 */
export const TaskSummaryContext = createContext<TaskSummaryState>(TASK_SUMMARY_DEFAULT)

/**
 * Returns the queue's counts from the nearest `TaskSummaryProvider`, or the
 * empty {@link TASK_SUMMARY_DEFAULT} when there is none.
 */
export function useTaskSummary(): TaskSummaryState {
  return useContext(TaskSummaryContext)
}

/**
 * The number on the navigation badge: how many tasks wait on the signed-in
 * reader, or 0 when the counts are unknown — a badge must never be drawn out of
 * a guess.
 */
export function useWaitingOnMe(): number {
  return useTaskSummary().summary?.waiting_on_me ?? 0
}
