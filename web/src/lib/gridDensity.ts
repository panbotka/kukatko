/**
 * How many photo tiles a grid puts side by side. Always a concrete column count
 * in {@link GRID_COLUMNS_MIN}..{@link GRID_COLUMNS_MAX}: the user picks the exact
 * number and it is persisted. "Auto" is no longer a mode — it only seeds the very
 * first value from the screen width (see {@link initialColumnsForWidth}).
 */
export type GridDensity = number

/** The fewest columns the user may pin the grid to: one photo per row. */
export const GRID_COLUMNS_MIN = 1

/** The most columns the user may pin the grid to. */
export const GRID_COLUMNS_MAX = 10

/** The pinnable column counts, ascending, as offered by the density control. */
export const GRID_COLUMN_CHOICES: readonly number[] = Array.from(
  { length: GRID_COLUMNS_MAX - GRID_COLUMNS_MIN + 1 },
  (_, i) => GRID_COLUMNS_MIN + i,
)

/**
 * The width a tile targets when the initial column count is seeded from the
 * screen: roughly this many pixels per tile decides how many fit across the
 * viewport on first use. It is not a runtime floor — once seeded, the chosen
 * count is honoured verbatim on every viewport.
 */
export const GRID_TILE_MIN_PX = 140

/**
 * The gap between tiles in the photo grid, in pixels. Kept to a hairline so the
 * library reads as a dense, edge-to-edge wall of images rather than a page of
 * spaced-out cards — the photographs, not the gutters, are the hero.
 */
export const GRID_GAP_PX = 3

/**
 * The column count to fall back to when the viewport width cannot be measured
 * (no `window`, e.g. server-side render). A comfortable desktop-ish default; in
 * a real browser the width-based {@link initialColumnsForWidth} always wins.
 */
export const GRID_DENSITY_DEFAULT = 5

/** localStorage key under which the density is persisted. Per-device, never synced. */
const STORAGE_KEY = 'kukatko.grid.density'

/**
 * Rounds a finite number to the nearest whole column and clamps it into
 * `GRID_COLUMNS_MIN..GRID_COLUMNS_MAX`. A non-finite input falls back to
 * {@link GRID_DENSITY_DEFAULT} so the result is always a usable count.
 */
function clampColumns(n: number): number {
  if (!Number.isFinite(n)) {
    return GRID_DENSITY_DEFAULT
  }
  return Math.min(GRID_COLUMNS_MAX, Math.max(GRID_COLUMNS_MIN, Math.round(n)))
}

/** The current viewport width, or `0` when there is no `window` (SSR / non-DOM tests). */
function viewportWidth(): number {
  return typeof window === 'undefined' ? 0 : window.innerWidth
}

/**
 * The concrete column count to seed the grid with for a viewport of `width`
 * pixels: roughly how many {@link GRID_TILE_MIN_PX}-wide tiles (plus the hairline
 * gaps between them) fit across it, clamped into 1..{@link GRID_COLUMNS_MAX}. This
 * is the concrete resolution of the old responsive "auto" intent — used once to
 * pick the initial value, never as an ongoing recompute. A phone lands at one or
 * two columns, a very wide monitor at the maximum.
 */
export function initialColumnsForWidth(width: number): number {
  if (!Number.isFinite(width) || width <= 0) {
    return GRID_DENSITY_DEFAULT
  }
  const fit = Math.floor((width + GRID_GAP_PX) / (GRID_TILE_MIN_PX + GRID_GAP_PX))
  return clampColumns(fit)
}

/** The seed column count for the current viewport — auto's one and only job. */
export function initialColumns(): number {
  return initialColumnsForWidth(viewportWidth())
}

/**
 * Narrows a raw value to a usable column count. A finite number is rounded and
 * clamped into 1..{@link GRID_COLUMNS_MAX}; anything else — a legacy `'auto'`
 * string, `null`, `NaN`, a tampered object — is coerced to a concrete count
 * seeded from the current viewport width. Never throws, and always returns a
 * number in range.
 */
export function sanitizeDensity(raw: unknown): number {
  if (typeof raw === 'number' && Number.isFinite(raw)) {
    return clampColumns(raw)
  }
  return initialColumns()
}

/**
 * Steps a density one rung along the picker's ladder. A positive `delta` pins
 * more columns (smaller tiles), a negative one fewer (larger tiles), and both
 * ends clamp: one-per-row ({@link GRID_COLUMNS_MIN}) is the floor and
 * {@link GRID_COLUMNS_MAX} the ceiling. The input is sanitized first, so a
 * tampered value can never step off the ladder.
 */
export function stepDensity(density: number, delta: number): number {
  const current = sanitizeDensity(density)
  if (delta < 0) {
    return Math.max(GRID_COLUMNS_MIN, current - 1)
  }
  if (delta > 0) {
    return Math.min(GRID_COLUMNS_MAX, current + 1)
  }
  return current
}

/**
 * Reads the persisted column count, or `null` when the device has no usable
 * numeric preference — empty storage, storage unavailable (private mode / no
 * `window`), corrupt JSON, or a legacy `'auto'` string. The distinction lets the
 * caller seed a concrete count from the viewport width exactly once, on first
 * use, and migrate a legacy `'auto'` to a real number rather than recomputing it
 * on every render. A stored number is clamped into range before it is returned.
 */
export function readStoredDensity(): number | null {
  let raw: string | null
  try {
    raw = window.localStorage.getItem(STORAGE_KEY)
  } catch {
    // Storage unavailable (private mode / no window) — treat as "no preference".
    return null
  }
  if (raw === null) {
    return null
  }
  try {
    const parsed: unknown = JSON.parse(raw)
    // Only a finite number is a real preference; a legacy `'auto'`, an object, or
    // anything non-numeric is treated as "no preference" so the caller re-seeds.
    return typeof parsed === 'number' && Number.isFinite(parsed) ? clampColumns(parsed) : null
  } catch {
    // Value not parseable — treat as "no preference".
    return null
  }
}

/**
 * Persists the column count. Failures (storage disabled / quota) are swallowed:
 * persistence is best-effort and must never break the grid.
 */
export function writeDensity(density: number): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(density))
  } catch {
    // Best-effort: ignore storage failures.
  }
}

/**
 * Builds the `grid-template-columns` value for a density: exactly `count` equal
 * tracks, `repeat(count, 1fr)`. The count is honoured verbatim on every viewport
 * — the responsive `auto-fill` fallback is gone, because the user now always
 * picks a concrete number rather than leaning on a width-driven "auto". The
 * inter-tile gap is applied separately via the container's `gap`.
 */
export function gridTemplateColumns(density: number): string {
  return `repeat(${clampColumns(density)}, 1fr)`
}
