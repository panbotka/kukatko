/**
 * How many photo tiles a grid puts side by side. `'auto'` keeps the responsive
 * default — as many tiles as fit at {@link GRID_TILE_MIN_PX} — while a number
 * pins the grid to exactly that many columns on a wide enough viewport.
 */
export type GridDensity = 'auto' | number

/** The fewest columns the user may pin the grid to. */
export const GRID_COLUMNS_MIN = 2

/** The most columns the user may pin the grid to. */
export const GRID_COLUMNS_MAX = 8

/** The pinnable column counts, ascending, as offered by the density control. */
export const GRID_COLUMN_CHOICES: readonly number[] = Array.from(
  { length: GRID_COLUMNS_MAX - GRID_COLUMNS_MIN + 1 },
  (_, i) => GRID_COLUMNS_MIN + i,
)

/**
 * The narrowest a tile may get. It doubles as the responsive floor: a viewport
 * too narrow to give every pinned column this much width drops to fewer columns
 * rather than shrinking the tiles into unusable stamps.
 */
export const GRID_TILE_MIN_PX = 140

/** The gap between tiles in the photo grid, in pixels. */
export const GRID_GAP_PX = 6

/** Nothing pinned: the grid stays width-driven, exactly as it always has been. */
export const GRID_DENSITY_DEFAULT: GridDensity = 'auto'

/** localStorage key under which the density is persisted. Per-device, never synced. */
const STORAGE_KEY = 'kukatko.grid.density'

/**
 * Narrows a raw value to a usable density: a finite number is rounded and clamped
 * into `GRID_COLUMNS_MIN..GRID_COLUMNS_MAX`, and anything else — `'auto'`, a
 * string, `null`, `NaN`, a tampered object — falls back to
 * {@link GRID_DENSITY_DEFAULT}. Never throws.
 */
export function sanitizeDensity(raw: unknown): GridDensity {
  if (typeof raw !== 'number' || !Number.isFinite(raw)) {
    return GRID_DENSITY_DEFAULT
  }
  return Math.min(GRID_COLUMNS_MAX, Math.max(GRID_COLUMNS_MIN, Math.round(raw)))
}

/**
 * Reads the persisted density, falling back to {@link GRID_DENSITY_DEFAULT} when
 * storage is empty, unavailable (private mode / no `window`) or holds invalid JSON.
 */
export function readDensity(): GridDensity {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY)
    if (raw === null) {
      return GRID_DENSITY_DEFAULT
    }
    return sanitizeDensity(JSON.parse(raw))
  } catch {
    // Storage unavailable or value not parseable — fall back to the default.
    return GRID_DENSITY_DEFAULT
  }
}

/**
 * Persists the density. Failures (storage disabled / quota) are swallowed:
 * persistence is best-effort and must never break the grid.
 */
export function writeDensity(density: GridDensity): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(density))
  } catch {
    // Best-effort: ignore storage failures.
  }
}

/**
 * Builds the `grid-template-columns` value for a density.
 *
 * `'auto'` yields today's width-driven template. A pinned count yields tracks
 * whose *minimum* is the exact width `count` columns would need — so `auto-fill`
 * fits exactly `count` of them — floored at {@link GRID_TILE_MIN_PX}. Once the
 * viewport is too narrow for that floor the ideal width loses the `max()`, the
 * tracks stop shrinking, and `auto-fill` fits fewer columns. It can never fit
 * more, because a track is never narrower than its ideal width.
 *
 * The extra pixel subtracted from the gap total is slack: without it, sub-pixel
 * rounding of `100% / count` can overflow the row and drop a column.
 */
export function gridTemplateColumns(density: GridDensity, gapPx: number = GRID_GAP_PX): string {
  if (density === 'auto') {
    return `repeat(auto-fill, minmax(${GRID_TILE_MIN_PX}px, 1fr))`
  }
  const gaps = (density - 1) * gapPx + 1
  const ideal = `calc((100% - ${gaps}px) / ${density})`
  return `repeat(auto-fill, minmax(max(${GRID_TILE_MIN_PX}px, ${ideal}), 1fr))`
}
