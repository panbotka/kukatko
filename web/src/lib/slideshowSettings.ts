/**
 * The transition effect played between slides. `fade` cross-fades, `slide` moves
 * the next photo in from the side, `kenburns` slowly zooms and pans across the
 * photo for the whole slide, and `none` swaps instantly. Mirrors the effects
 * offered in the slideshow settings (`docs/ARCHITECTURE.md` §13).
 */
export type SlideshowEffect = 'fade' | 'slide' | 'kenburns' | 'none'

/** User-configurable slideshow preferences, persisted to localStorage. */
export interface SlideshowSettings {
  /** The transition effect between slides. */
  effect: SlideshowEffect
  /** Auto-advance interval, in milliseconds. */
  intervalMs: number
}

/** The transition effects offered in the settings, in display order. */
export const SLIDESHOW_EFFECTS: readonly SlideshowEffect[] = ['fade', 'slide', 'kenburns', 'none']

/** The auto-advance intervals offered in the settings, ascending, in milliseconds. */
export const SLIDESHOW_INTERVALS_MS: readonly number[] = [
  1000, 2000, 3000, 5000, 10000, 15000, 30000,
]

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

/**
 * Snaps a raw value to the nearest offered interval, falling back to the default
 * when it is not a usable number. Snapping (rather than clamping) also migrates
 * an interval that was persisted while it was still offered — the retired 7 s
 * option resolves to 5 s — so the picker never has to show a value it lacks an
 * option for. Ties go to the shorter interval.
 */
function sanitizeInterval(raw: unknown): number {
  if (typeof raw !== 'number' || !Number.isFinite(raw)) {
    return SLIDESHOW_DEFAULTS.intervalMs
  }
  return SLIDESHOW_INTERVALS_MS.reduce((nearest, ms) =>
    Math.abs(ms - raw) < Math.abs(nearest - raw) ? ms : nearest,
  )
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
