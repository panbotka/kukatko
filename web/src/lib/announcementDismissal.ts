import { type Announcement } from '../services/announcement'

import { accountStorageKey } from './accountStorage'

/**
 * localStorage key prefix under which an account's dismissed announcement is
 * persisted; the account's uid and the project's slug complete it
 * (`<prefix>.<uid>.<slug>`, see {@link accountStorageKey}). Per account because
 * one family member closing a maintenance notice must not hide it from the next
 * one to sign in on the same browser; per project because every project has its
 * own banner, and closing one must neither hide another nor be forgotten when
 * the reader looks at another. Cleared at sign-out with the rest of the
 * account's browser state.
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

/** The storage key of `user`'s dismissal in the project `slug`, or null without a user. */
function dismissalKey(user: string | undefined, slug: string): string | null {
  const key = accountStorageKey(ANNOUNCEMENT_DISMISSAL_PREFIX, user)
  return key === null ? null : `${key}.${slug}`
}

/**
 * Reads the token (see {@link announcementDismissalToken}) of the announcement
 * `user` last dismissed in the project `slug`, or the empty string when none
 * was dismissed, there is no user, or storage is unavailable (private mode) —
 * all of which show the banner.
 */
export function readDismissedAnnouncement(user: string | undefined, slug: string): string {
  const key = dismissalKey(user, slug)
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
 * Persists that `user` dismissed the announcement identified by `token` in the
 * project `slug`. Failures (storage disabled / quota) are swallowed: dismissal
 * is best-effort and must never break the shell.
 */
export function writeDismissedAnnouncement(
  user: string | undefined,
  slug: string,
  token: string,
): void {
  const key = dismissalKey(user, slug)
  if (key === null) {
    return
  }
  try {
    window.localStorage.setItem(key, token)
  } catch {
    // Best-effort: ignore storage failures.
  }
}
