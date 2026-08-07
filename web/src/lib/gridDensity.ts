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

/**
 * A column ceiling a narrow viewport imposes on the grid, whatever the user's
 * stored density says.
 */
export interface GridColumnCap {
  /** Applies to viewports strictly narrower than this many CSS pixels. */
  belowWidthPx: number
  /** The most columns such a viewport will render. */
  maxColumns: number
}

/**
 * The column ceilings for narrow viewports, ascending by width — Bootstrap's
 * `sm` (576 px) and `md` (768 px) boundaries, so the grid steps down where the
 * rest of the layout already does.
 *
 * The density is a single number per browser profile, so a count pinned on a
 * laptop follows the user onto a phone: eight columns across 393 px leaves tiles
 * under 50 px, where the favourite heart drawn on a tile is larger than the
 * photograph under it. The ceiling only limits what is *rendered* — the stored
 * preference is never rewritten, see {@link clampColumnsToWidth}.
 */
export const GRID_COLUMN_CAPS: readonly GridColumnCap[] = [
  { belowWidthPx: 576, maxColumns: 3 },
  { belowWidthPx: 768, maxColumns: 4 },
]

/**
 * The width an outlier-review tile targets when its column count is seeded from
 * the screen: the 16rem the grid was hard-coded to before it had a control. A
 * face you are asked to judge needs far more room than a library thumbnail, so
 * seeding it from the library's 140 px would open the page at twice the density
 * anyone wants.
 */
export const OUTLIER_TILE_MIN_PX = 256

/**
 * The gap between outlier-review cards, in pixels (Bootstrap's `gap-3`). Unlike
 * the library's hairline these are cards with buttons in them, so they need real
 * gutters — the grid is a worksheet, not a wall of photographs.
 */
export const OUTLIER_GAP_PX = 16

/**
 * Which grid a density belongs to. A scope is the whole per-grid contract: where
 * the count is persisted, what tile width seeds the first value and how wide the
 * gutters are. It exists so a second grid can have the *same* control without
 * sharing the *same* number.
 */
export interface GridDensityScope {
  /** localStorage key under which this grid's density is persisted. */
  storageKey: string
  /** Target tile width used once, to seed the count from the viewport width. */
  tileMinPx: number
  /** The gutter between tiles, in pixels. */
  gapPx: number
}

/**
 * The photo library's grid: the browsing wall shared by `/library`, the album and
 * label galleries, favorites, trash and a subject's photos.
 */
export const LIBRARY_GRID_SCOPE: GridDensityScope = {
  storageKey: 'kukatko.grid.density',
  tileMinPx: GRID_TILE_MIN_PX,
  gapPx: GRID_GAP_PX,
}

/**
 * The `/outliers` review grid, deliberately on its **own** key. Browsing a
 * library and judging whether a 4 %-wide face is the right person are different
 * jobs at different comfortable densities: sharing one number would mean every
 * trip to `/outliers` re-densifies the library on the way back, and every trip
 * back re-densifies the review. The control is shared; the preference is not.
 */
export const OUTLIER_GRID_SCOPE: GridDensityScope = {
  storageKey: 'kukatko.outliers.density',
  tileMinPx: OUTLIER_TILE_MIN_PX,
  gapPx: OUTLIER_GAP_PX,
}

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
 * pixels: roughly how many `scope.tileMinPx`-wide tiles (plus the gaps between
 * them) fit across it, clamped into 1..{@link GRID_COLUMNS_MAX}. This is the
 * concrete resolution of the old responsive "auto" intent — used once to pick the
 * initial value, never as an ongoing recompute. A phone lands at one or two
 * columns, a very wide monitor at the maximum.
 */
export function initialColumnsForWidth(
  width: number,
  scope: GridDensityScope = LIBRARY_GRID_SCOPE,
): number {
  if (!Number.isFinite(width) || width <= 0) {
    return GRID_DENSITY_DEFAULT
  }
  const fit = Math.floor((width + scope.gapPx) / (scope.tileMinPx + scope.gapPx))
  return clampColumns(fit)
}

/** The seed column count for the current viewport — auto's one and only job. */
export function initialColumns(scope: GridDensityScope = LIBRARY_GRID_SCOPE): number {
  return initialColumnsForWidth(viewportWidth(), scope)
}

/**
 * The most columns a viewport of `width` pixels will render, per
 * {@link GRID_COLUMN_CAPS}: at most 3 below 576 px, at most 4 below 768 px, and
 * the full {@link GRID_COLUMNS_MAX} from there up. A width that cannot be used
 * (0, negative, non-finite — no `window`, or a non-DOM test) imposes no ceiling:
 * without a measurement the user's own choice is the better guess.
 */
export function maxColumnsForWidth(width: number): number {
  if (!Number.isFinite(width) || width <= 0) {
    return GRID_COLUMNS_MAX
  }
  for (const cap of GRID_COLUMN_CAPS) {
    if (width < cap.belowWidthPx) {
      return cap.maxColumns
    }
  }
  return GRID_COLUMNS_MAX
}

/** The column ceiling of the current viewport, unmeasurable width → no ceiling. */
export function maxColumnsForViewport(): number {
  return maxColumnsForWidth(viewportWidth())
}

/**
 * Narrows a raw value to a usable column count. A finite number is rounded and
 * clamped into 1..{@link GRID_COLUMNS_MAX}; anything else — a legacy `'auto'`
 * string, `null`, `NaN`, a tampered object — is coerced to a concrete count
 * seeded from the current viewport width. Never throws, and always returns a
 * number in range.
 */
export function sanitizeDensity(
  raw: unknown,
  scope: GridDensityScope = LIBRARY_GRID_SCOPE,
): number {
  if (typeof raw === 'number' && Number.isFinite(raw)) {
    return clampColumns(raw)
  }
  return initialColumns(scope)
}

/**
 * The column count a grid actually renders on a viewport of `width` pixels: the
 * sanitized preference, lowered to that viewport's ceiling when the screen is
 * too narrow to carry it. This is a display-time clamp and nothing else — the
 * persisted preference stays exactly as the user set it, so widening the window
 * (or opening the same library on a laptop) restores their density verbatim.
 */
export function clampColumnsToWidth(
  density: number,
  width: number,
  scope: GridDensityScope = LIBRARY_GRID_SCOPE,
): number {
  return Math.min(sanitizeDensity(density, scope), maxColumnsForWidth(width))
}

/**
 * Steps a density one rung along the picker's ladder. A positive `delta` pins
 * more columns (smaller tiles), a negative one fewer (larger tiles), and both
 * ends clamp: one-per-row ({@link GRID_COLUMNS_MIN}) is the floor and
 * {@link GRID_COLUMNS_MAX} the ceiling. The input is sanitized first, so a
 * tampered value can never step off the ladder.
 */
export function stepDensity(
  density: number,
  delta: number,
  scope: GridDensityScope = LIBRARY_GRID_SCOPE,
): number {
  const current = sanitizeDensity(density, scope)
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
export function readStoredDensity(scope: GridDensityScope = LIBRARY_GRID_SCOPE): number | null {
  let raw: string | null
  try {
    raw = window.localStorage.getItem(scope.storageKey)
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
export function writeDensity(density: number, scope: GridDensityScope = LIBRARY_GRID_SCOPE): void {
  try {
    window.localStorage.setItem(scope.storageKey, JSON.stringify(density))
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
