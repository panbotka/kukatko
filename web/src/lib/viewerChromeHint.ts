/**
 * localStorage key under which the "the controls come back" hint records that it
 * has been shown.
 *
 * A plain boolean, deliberately: the gesture is learnt once and stays learnt, so
 * the hint has to be a one-off per device. A flag keyed on anything shorter-lived
 * (the session, the photo) would bring it back on the fiftieth photograph, which
 * is worse than never showing it at all.
 */
const STORAGE_KEY = 'kukatko.viewer.chromeHintSeen'

/**
 * Whether this device has already been told that a tap brings the viewer's
 * controls back.
 *
 * Storage being unavailable (private mode) reads as "not yet shown": a hint shown
 * once too often is a smaller failure than a first-time reader left staring at a
 * photograph with no visible way to act on it.
 */
export function readChromeHintSeen(): boolean {
  try {
    return window.localStorage.getItem(STORAGE_KEY) === 'true'
  } catch {
    // Storage unavailable — treat as "not shown yet".
    return false
  }
}

/**
 * Records that the hint has been shown. Failures (storage disabled / quota) are
 * swallowed: this is best-effort bookkeeping and must never break the viewer.
 */
export function markChromeHintSeen(): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, 'true')
  } catch {
    // Best-effort: ignore storage failures.
  }
}
