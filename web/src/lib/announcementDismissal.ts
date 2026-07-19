/**
 * localStorage key under which the dismissed announcement's `updated_at` is
 * persisted. Keying on the timestamp (rather than a plain boolean) is what lets a
 * *newly published* announcement — which carries a fresh `updated_at` — reappear
 * even after the user dismissed the previous one.
 */
const STORAGE_KEY = 'kukatko.announcement.dismissedAt'

/**
 * Reads the `updated_at` of the announcement the user last dismissed, or the
 * empty string when none was dismissed or storage is unavailable (private mode).
 */
export function readDismissedAnnouncement(): string {
  try {
    return window.localStorage.getItem(STORAGE_KEY) ?? ''
  } catch {
    // Storage unavailable — treat as "nothing dismissed" so the banner still shows.
    return ''
  }
}

/**
 * Persists the dismissed announcement's `updated_at`. Failures (storage disabled /
 * quota) are swallowed: dismissal is best-effort and must never break the shell.
 */
export function writeDismissedAnnouncement(updatedAt: string): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, updatedAt)
  } catch {
    // Best-effort: ignore storage failures.
  }
}
