import { createContext, useContext } from 'react'

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
