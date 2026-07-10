/**
 * localStorage key under which the face-overlay preference is persisted, so the
 * choice survives navigating between photos and reloading the app.
 */
const STORAGE_KEY = 'kukatko.faces.overlay'

/** Faces are drawn by default — they are primary content on the photo detail. */
export const FACE_OVERLAY_DEFAULT = true

/**
 * Reads the persisted face-overlay preference, falling back to
 * {@link FACE_OVERLAY_DEFAULT} when storage is empty, unavailable (private mode)
 * or holds anything other than the two booleans this module writes.
 */
export function readFaceOverlay(): boolean {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY)
    if (raw === 'true') {
      return true
    }
    if (raw === 'false') {
      return false
    }
    return FACE_OVERLAY_DEFAULT
  } catch {
    // Storage unavailable — fall back to the default.
    return FACE_OVERLAY_DEFAULT
  }
}

/**
 * Persists the face-overlay preference. Failures (storage disabled / quota) are
 * swallowed: persistence is best-effort and must never break the detail page.
 */
export function writeFaceOverlay(visible: boolean): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, String(visible))
  } catch {
    // Best-effort: ignore storage failures.
  }
}
