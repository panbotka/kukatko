/**
 * The transition effect played between slides. `fade` cross-fades, `slide` moves
 * the next photo in from the side, and `none` swaps instantly. Mirrors the
 * effects offered in the slideshow settings (`docs/ARCHITECTURE.md` §13).
 */
export type SlideshowEffect = 'fade' | 'slide' | 'none'

/** User-configurable slideshow preferences, persisted to localStorage. */
export interface SlideshowSettings {
  /** The transition effect between slides. */
  effect: SlideshowEffect
  /** Auto-advance interval, in milliseconds. */
  intervalMs: number
}

/** The transition effects offered in the settings, in display order. */
export const SLIDESHOW_EFFECTS: readonly SlideshowEffect[] = ['fade', 'slide', 'none']

/** The auto-advance intervals offered in the settings, in milliseconds. */
export const SLIDESHOW_INTERVALS_MS: readonly number[] = [2000, 3000, 5000, 7000, 10000]

/** Lower / upper bounds for a sanitised interval (guards tampered storage). */
const MIN_INTERVAL_MS = 1000
const MAX_INTERVAL_MS = 60000

/** Default preferences: a 5 s cross-fade, used until the user changes them. */
export const SLIDESHOW_DEFAULTS: SlideshowSettings = { effect: 'fade', intervalMs: 5000 }

/** localStorage key under which the preferences are persisted. */
const STORAGE_KEY = 'kukatko.slideshow.settings'

/** Narrows a raw value to a known effect, falling back to the default. */
function sanitizeEffect(raw: unknown): SlideshowEffect {
  return (SLIDESHOW_EFFECTS as readonly unknown[]).includes(raw)
    ? (raw as SlideshowEffect)
    : SLIDESHOW_DEFAULTS.effect
}

/** Clamps a raw value to a sane interval, falling back to the default. */
function sanitizeInterval(raw: unknown): number {
  if (typeof raw !== 'number' || !Number.isFinite(raw)) {
    return SLIDESHOW_DEFAULTS.intervalMs
  }
  return Math.min(MAX_INTERVAL_MS, Math.max(MIN_INTERVAL_MS, Math.round(raw)))
}

/** Sanitises a (possibly partial / tampered) settings object into a valid one. */
export function sanitizeSettings(
  raw: Partial<SlideshowSettings> | null | undefined,
): SlideshowSettings {
  return {
    effect: sanitizeEffect(raw?.effect),
    intervalMs: sanitizeInterval(raw?.intervalMs),
  }
}

/**
 * Reads the persisted slideshow preferences from localStorage, sanitising them
 * and falling back to {@link SLIDESHOW_DEFAULTS} when storage is empty,
 * unavailable (e.g. SSR / private mode) or holds invalid JSON.
 */
export function readSettings(): SlideshowSettings {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY)
    if (raw === null) {
      return { ...SLIDESHOW_DEFAULTS }
    }
    return sanitizeSettings(JSON.parse(raw) as Partial<SlideshowSettings>)
  } catch {
    // Storage unavailable or value not parseable — fall back to defaults.
    return { ...SLIDESHOW_DEFAULTS }
  }
}

/**
 * Persists the slideshow preferences to localStorage. Failures (storage
 * disabled / quota) are swallowed: persistence is best-effort and must never
 * break playback.
 */
export function writeSettings(settings: SlideshowSettings): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(settings))
  } catch {
    // Best-effort: ignore storage failures.
  }
}
