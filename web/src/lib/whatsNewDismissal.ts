/**
 * localStorage key under which the `since` of the dismissed digest is persisted.
 *
 * Keying on the digest's reference point (rather than on a plain boolean) is what
 * gives dismissal exactly the lifetime the panel needs: `since` is constant for
 * as long as a visit lasts, so a dismissal survives every reload and every walk
 * around the app within that visit — and the next visit, which carries a fresh
 * `since`, shows its panel again.
 */
const STORAGE_KEY = 'kukatko.whatsNew.dismissedSince'

/**
 * Reads the `since` of the digest the user last dismissed, or the empty string
 * when none was dismissed or storage is unavailable (private mode).
 */
export function readDismissedWhatsNew(): string {
  try {
    return window.localStorage.getItem(STORAGE_KEY) ?? ''
  } catch {
    // Storage unavailable — treat as "nothing dismissed" so the panel still shows.
    return ''
  }
}

/**
 * Persists the dismissed digest's `since`. Failures (storage disabled / quota)
 * are swallowed: dismissal is best-effort and must never break the library.
 */
export function writeDismissedWhatsNew(since: string): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, since)
  } catch {
    // Best-effort: ignore storage failures.
  }
}
