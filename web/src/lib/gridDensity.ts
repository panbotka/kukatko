/**
 * How many photo tiles a grid puts side by side. `'auto'` keeps the responsive
 * default — as many tiles as fit at {@link GRID_TILE_MIN_PX} — while a number
 * pins the grid to exactly that many columns on a wide enough viewport.
 */
export type GridDensity = 'auto' | number

/** The fewest columns the user may pin the grid to: one photo per row. */
export const GRID_COLUMNS_MIN = 1

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
 * Steps a density one rung along the picker's ladder. A positive `delta` pins
 * more columns (smaller tiles), a negative one fewer (larger tiles), and both
 * ends clamp: one-per-row ({@link GRID_COLUMNS_MIN}) is the floor and
 * {@link GRID_COLUMNS_MAX} the ceiling. `'auto'` is the responsive default off
 * to the side of the ladder: stepping up from it enters the smallest
 * *multi-column* count (never one-per-row, which is fewer tiles, not more), and
 * stepping down from it stays put — `'auto'` is reached again only by resetting.
 * The input is sanitized first, so a tampered value can never step off the ladder.
 */
export function stepDensity(density: GridDensity, delta: number): GridDensity {
  const current = sanitizeDensity(density)
  if (delta < 0) {
    // One-per-row is the floor and `'auto'` has no rung below it; the control
    // disables the `−` button in both states.
    if (current === 'auto' || current <= GRID_COLUMNS_MIN) return current
    return current - 1
  }
  if (delta > 0) {
    // Leaving `'auto'` pins the smallest multi-column layout, so "more tiles per
    // row" from the responsive default never collapses to a single column.
    if (current === 'auto') return Math.min(GRID_COLUMNS_MAX, GRID_COLUMNS_MIN + 1)
    return Math.min(GRID_COLUMNS_MAX, current + 1)
  }
  return current
}

/**
 * Reads the persisted density, or `null` when the device has no usable stored
 * preference — empty storage, storage unavailable (private mode / no `window`),
 * or corrupt JSON. The distinction matters so the caller can pick a
 * viewport-aware initial layout (one-per-row on a phone) only while the user has
 * not chosen for themselves; a real stored choice, `'auto'` included, is honoured
 * on every viewport.
 */
export function readStoredDensity(): GridDensity | null {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY)
    if (raw === null) {
      return null
    }
    return sanitizeDensity(JSON.parse(raw))
  } catch {
    // Storage unavailable or value not parseable — treat as "no preference".
    return null
  }
}

/**
 * The density to fall back to when the device has no stored preference. A narrow
 * (phone-width) viewport starts at one photo per row so the tiles aren't cramped;
 * a wider viewport keeps the responsive multi-column default. Only consulted
 * while nothing is persisted — see {@link readStoredDensity}.
 */
export function defaultDensityForViewport(isNarrow: boolean): GridDensity {
  return isNarrow ? GRID_COLUMNS_MIN : GRID_DENSITY_DEFAULT
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
 * `'auto'` yields today's width-driven template and a pinned count of `1` a
 * single full-width column. Any other pinned count yields tracks whose *minimum*
 * is the exact width `count` columns would need — so `auto-fill` fits exactly
 * `count` of them — floored at {@link GRID_TILE_MIN_PX}. Once the
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
  if (density === 1) {
    // One photo per row: a single track spanning the full content width. There
    // is no tile floor to honour and nothing narrower to fall back to, so the
    // auto-fill machinery is skipped entirely. `minmax(0, 1fr)` lets the column
    // shrink with the viewport rather than being pinned to its content's width.
    return 'minmax(0, 1fr)'
  }
  const gaps = (density - 1) * gapPx + 1
  const ideal = `calc((100% - ${gaps}px) / ${density})`
  return `repeat(auto-fill, minmax(max(${GRID_TILE_MIN_PX}px, ${ideal}), 1fr))`
}
