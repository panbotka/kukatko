import { LEGACY_LANGUAGE_KEY } from '../i18n/accountLanguage'
import { isAccountStorageKey } from '../lib/accountStorage'
import { ANNOUNCEMENT_DISMISSAL_PREFIX } from '../lib/announcementDismissal'
import { GRID_SCROLL_PREFIX } from '../lib/gridScroll'
import { PICKED_LOCATION_KEY } from '../lib/pickedLocation'
import { DAILY_STORAGE_PREFIX } from '../lib/reviewRounds'
import { REVIEW_SNAPSHOT_KEY } from '../lib/reviewSnapshot'
import { WHATS_NEW_DISMISSAL_PREFIX } from '../lib/whatsNewDismissal'

/**
 * The browser state sign-out removes: everything that records what an account
 * has *seen, dismissed, played or scrolled*, in both storages, every account's
 * copy of it (`prefix.<uid>`) as well as the global key an older build kept.
 *
 * What is deliberately absent, and why it survives:
 * - the per-device display preferences — grid density (`kukatko.grid.density`,
 *   `kukatko.review.density`), the slideshow settings, the playback rate, the
 *   viewer's "the controls come back" hint: they describe the screen, not the
 *   person, and are the same answer for whoever sits at it;
 * - the per-account language (`kukatko.language.<uid>`): a preference kept per
 *   uid precisely so it survives signing out and back in, and never read for
 *   anybody else;
 * - the per-account answer to the notification prompt
 *   (`kukatko.pushPrompt.answered.<uid>`): a browser's push subscription
 *   outlives the session, so forgetting the answer would only ask again.
 */
export const SIGN_OUT_CLEARED = [
  ANNOUNCEMENT_DISMISSAL_PREFIX,
  WHATS_NEW_DISMISSAL_PREFIX,
  DAILY_STORAGE_PREFIX,
  REVIEW_SNAPSHOT_KEY,
  GRID_SCROLL_PREFIX,
  PICKED_LOCATION_KEY,
  LEGACY_LANGUAGE_KEY,
] as const

/** The browser's storages, each only if it can be reached at all. */
function reachableStorages(): Storage[] {
  const storages: Storage[] = []
  for (const pick of [() => window.localStorage, () => window.sessionStorage]) {
    try {
      storages.push(pick())
    } catch {
      // Storage disabled: there is nothing in it to leave behind either.
    }
  }
  return storages
}

/**
 * Removes the {@link SIGN_OUT_CLEARED} state from `storages` (by default the
 * browser's local and session storage). Best-effort per key: a storage that
 * throws halfway is left as it is, and every reader of these keys already
 * treats unreadable or foreign state as absent.
 */
export function clearSignedOutState(storages: readonly Storage[] = reachableStorages()): void {
  for (const storage of storages) {
    try {
      // Collected first: removing while indexing would skip the key that
      // slides into the removed one's place.
      const doomed: string[] = []
      for (let i = 0; i < storage.length; i++) {
        const key = storage.key(i)
        if (key !== null && SIGN_OUT_CLEARED.some((prefix) => isAccountStorageKey(key, prefix))) {
          doomed.push(key)
        }
      }
      for (const key of doomed) {
        storage.removeItem(key)
      }
    } catch {
      // Best-effort: a storage that refuses keeps what it has.
    }
  }
}
