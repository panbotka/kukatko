import { accountStorageKey } from './accountStorage'

/**
 * localStorage key prefix under which the `since` of an account's dismissed
 * digest is persisted; the account's uid completes it (see
 * {@link accountStorageKey}), so one family member closing their digest does
 * not close the next one's. Cleared at sign-out with the rest of the account's
 * browser state.
 *
 * Keying on the digest's reference point (rather than on a plain boolean) is what
 * gives dismissal exactly the lifetime the panel needs: `since` is constant for
 * as long as a visit lasts, so a dismissal survives every reload and every walk
 * around the app within that visit — and the next visit, which carries a fresh
 * `since`, shows its panel again.
 */
export const WHATS_NEW_DISMISSAL_PREFIX = 'kukatko.whatsNew.dismissedSince'

/**
 * Reads the `since` of the digest `user` last dismissed, or the empty string
 * when none was dismissed, there is no user, or storage is unavailable
 * (private mode) — all of which show the panel.
 */
export function readDismissedWhatsNew(user: string | undefined): string {
  const key = accountStorageKey(WHATS_NEW_DISMISSAL_PREFIX, user)
  if (key === null) {
    return ''
  }
  try {
    return window.localStorage.getItem(key) ?? ''
  } catch {
    // Storage unavailable — treat as "nothing dismissed" so the panel still shows.
    return ''
  }
}

/**
 * Persists that `user` dismissed the digest with this `since`. Failures
 * (storage disabled / quota) are swallowed: dismissal is best-effort and must
 * never break the library.
 */
export function writeDismissedWhatsNew(user: string | undefined, since: string): void {
  const key = accountStorageKey(WHATS_NEW_DISMISSAL_PREFIX, user)
  if (key === null) {
    return
  }
  try {
    window.localStorage.setItem(key, since)
  } catch {
    // Best-effort: ignore storage failures.
  }
}
