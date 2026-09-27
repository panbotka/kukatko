import { type Announcement } from '../services/announcement'

import { accountStorageKey } from './accountStorage'

/**
 * localStorage key prefix under which an account's dismissed announcement is
 * persisted; the account's uid completes it (see {@link accountStorageKey}).
 * Per account because the announcement is instance-wide: one family member
 * closing a maintenance notice must not hide it from the next one to sign in
 * on the same browser. Cleared at sign-out with the rest of the account's
 * browser state.
 */
export const ANNOUNCEMENT_DISMISSAL_PREFIX = 'kukatko.announcement.dismissedAt'

/**
 * What a dismissal records about the message it closed. The message's
 * `updated_at` when it has one — a *newly published* announcement carries a
 * fresh timestamp and so reappears even after the previous one was dismissed.
 * Without a timestamp (the field is optional on the wire) the message itself
 * stands in, level included: dismissing still sticks, and publishing anything
 * different still brings the banner back. An empty token is never produced, so
 * "nothing dismissed" and "this was dismissed" cannot be confused.
 */
export function announcementDismissalToken(
  announcement: Pick<Announcement, 'message' | 'level' | 'updated_at'>,
): string {
  const updatedAt = announcement.updated_at ?? ''
  if (updatedAt !== '') {
    return updatedAt
  }
  return `message:${announcement.level ?? ''}:${announcement.message}`
}

/**
 * Reads the token (see {@link announcementDismissalToken}) of the announcement
 * `user` last dismissed, or the empty string when none was dismissed, there is
 * no user, or storage is unavailable (private mode) — all of which show the
 * banner.
 */
export function readDismissedAnnouncement(user: string | undefined): string {
  const key = accountStorageKey(ANNOUNCEMENT_DISMISSAL_PREFIX, user)
  if (key === null) {
    return ''
  }
  try {
    return window.localStorage.getItem(key) ?? ''
  } catch {
    // Storage unavailable — treat as "nothing dismissed" so the banner still shows.
    return ''
  }
}

/**
 * Persists that `user` dismissed the announcement identified by `token`.
 * Failures (storage disabled / quota) are swallowed: dismissal is best-effort
 * and must never break the shell.
 */
export function writeDismissedAnnouncement(user: string | undefined, token: string): void {
  const key = accountStorageKey(ANNOUNCEMENT_DISMISSAL_PREFIX, user)
  if (key === null) {
    return
  }
  try {
    window.localStorage.setItem(key, token)
  } catch {
    // Best-effort: ignore storage failures.
  }
}
