import { createContext, useContext } from 'react'

import type { UnreadByProject } from '../services/notifications'

/**
 * The in-app inbox's unread count as the shell holds it, plus the handle to
 * refetch it.
 *
 * `unread` is null until the first response arrives, after a failed fetch, and
 * for anybody not signed in — and null means "draw no number": the bell stays,
 * its badge does not, rather than showing a count nobody can vouch for.
 */
export interface NotificationInboxState {
  unread: number | null
  /**
   * The unread count of every project the account is in standing in, keyed by
   * project uid (a project with none is absent) — the marks on the other
   * projects in the switcher, the phone drawer and the chooser. Null, like
   * `unread`, until known.
   */
  unreadByProject: UnreadByProject | null
  /**
   * Refetches the count now. The bell calls it when it opens and after "mark all
   * read", the notification page after it marked one read. A no-op outside the
   * provider.
   */
  refresh: () => void
}

/**
 * The safe default: nothing known, nothing to do. A component rendered outside
 * a `NotificationInboxProvider` (a focused unit test, say) shows a bare bell.
 */
export const NOTIFICATION_INBOX_DEFAULT: NotificationInboxState = {
  unread: null,
  unreadByProject: null,
  refresh: () => {
    // Nothing to refresh without a provider.
  },
}

/**
 * Provides the unread count. Like the task summary it has a non-null default
 * and its hook never throws outside a provider: the count is a hint over the
 * navigation, so a missing provider degrades to "no badge".
 */
export const NotificationInboxContext = createContext<NotificationInboxState>(
  NOTIFICATION_INBOX_DEFAULT,
)

/**
 * Returns the inbox state from the nearest `NotificationInboxProvider`, or the
 * empty {@link NOTIFICATION_INBOX_DEFAULT} when there is none.
 */
export function useNotificationInbox(): NotificationInboxState {
  return useContext(NotificationInboxContext)
}

/**
 * Returns how many unread notifications the project `projectUID` holds for the
 * reader: 0 when it has none or nothing is known yet — the badge then simply
 * is not drawn.
 */
export function useProjectUnread(): (projectUID: string) => number {
  const { unreadByProject } = useNotificationInbox()
  return (projectUID) => unreadByProject?.[projectUID] ?? 0
}
