/**
 * The justified row layout behind the photo wall: a pure function turning a run
 * of aspect ratios plus a container width into rows of tiles that keep their
 * natural proportions, share one height per row and fill the row edge to edge —
 * the layout every modern gallery uses, and the reason a panorama no longer
 * looks like a square with its ends cut off.
 *
 * Nothing here touches the DOM, React or the catalogue: the grid measures its
 * own width, this decides the shape, and the two meet in `PhotoGrid`. That is
 * what makes the geometry unit-testable, which for a layout with this many
 * boundary cases (one panorama on a phone, a single portrait left over on the
 * last row, a page of photos whose dimensions have not arrived yet) is the whole
 * point.
 */

/**
 * The shape assumed for a photo whose dimensions are unknown — a not-yet-loaded
 * slot of the windowed library list, or a row catalogued before dimensions were
 * recorded. 3:2 is the classic camera frame and the commonest ratio in a family
 * archive, so a placeholder sized this way is replaced by something close to it
 * far more often than not, which is what keeps the row from visibly re-cutting
 * itself when the photo lands.
 */
export const DEFAULT_TILE_RATIO = 3 / 2

/**
 * The narrowest and widest a tile may be *laid out* as. A 10:1 panorama or a
 * scanned strip of negatives would otherwise own a row of its own at a height
 * the rest of the wall never reaches (or, on the last row, a height of thirty
 * pixels). Clamping trades a slice of an extreme photo — the tile is filled with
 * `object-fit: cover`, so the middle survives and the ends are cropped — for a
 * wall that stays readable, which is the same bargain every gallery makes.
 */
export const MIN_TILE_RATIO = 1 / 3

/** The widest a tile may be laid out as; see {@link MIN_TILE_RATIO}. */
export const MAX_TILE_RATIO = 3

/**
 * The aspect ratio the density control's column count is translated through: a
 * density of five means "five *landscape* photographs across", so the target row
 * height is the width one tile gets divided by this. Portraits then sit fewer to
 * a row and panoramas more, which is exactly what a justified wall is for.
 */
export const REFERENCE_TILE_RATIO = 3 / 2

/**
 * How far the last row may be stretched past the target height to fill the width.
 * A last row holding almost enough photos is justified like any other; one
 * holding a single portrait would have to be blown up several times over to
 * reach the right edge, so past this factor the row keeps the target height and
 * simply ends where its photos end.
 */
export const LAST_ROW_MAX_STRETCH = 1.4

/** The smallest row height the layout will ever produce, in CSS pixels. */
export const MIN_ROW_HEIGHT = 40

/** One tile's box within a row, in CSS pixels. */
export interface JustifiedTile {
  /** The tile's absolute index in the input run. */
  index: number
  /** The tile's laid-out width, in CSS pixels. */
  width: number
}

/** One row of the justified wall. */
export interface JustifiedRow {
  /** Absolute index of the row's first tile — the rows tile a contiguous run. */
  start: number
  /** The height every tile in the row is drawn at, in CSS pixels. */
  height: number
  /** The row's tiles, left to right. */
  tiles: JustifiedTile[]
}

/** Inputs for {@link justifiedRows}. */
export interface JustifiedLayoutOptions {
  /** The width the rows must fill, in CSS pixels. */
  containerWidth: number
  /** The height a full row aims for, in CSS pixels (see {@link rowHeightForColumns}). */
  targetRowHeight: number
  /** The gutter between tiles (and between rows), in CSS pixels. */
  gap: number
  /**
   * The most tiles one row may hold, whatever the target height would allow.
   * Omitted (or unusable: non-finite, below one) means no ceiling — the greedy
   * rule alone decides, which is what every viewport wide enough to carry the
   * density ladder gets.
   *
   * It exists for the narrow viewport, where the target height is a poor proxy
   * for "how many photographs across": a row of portraits at a phone's target
   * height holds twice the pinned count, at a tile width where the controls
   * drawn on a tile are bigger than the photograph under it. See
   * `lib/gridDensity.maxTilesPerRowForWidth`.
   */
  maxTilesPerRow?: number
}

/**
 * The aspect ratio (width ÷ height) a photo is laid out at: its stored pixel
 * dimensions with the EXIF orientation applied — a quarter turn swaps the sides,
 * and the thumbnail the tile renders has that rotation baked in. Dimensions that
 * cannot be used (missing, zero, negative, non-finite) yield
 * {@link DEFAULT_TILE_RATIO}; every result is clamped into
 * {@link MIN_TILE_RATIO}..{@link MAX_TILE_RATIO}.
 */
export function tileRatio(width: number, height: number, orientation = 0): number {
  const rotated = orientation >= 5 && orientation <= 8
  const w = rotated ? height : width
  const h = rotated ? width : height
  if (!Number.isFinite(w) || !Number.isFinite(h) || w <= 0 || h <= 0) {
    return DEFAULT_TILE_RATIO
  }
  return clampRatio(w / h)
}

/** Clamps a ratio into the laid-out range; unusable input falls back to the default. */
export function clampRatio(ratio: number): number {
  if (!Number.isFinite(ratio) || ratio <= 0) {
    return DEFAULT_TILE_RATIO
  }
  return Math.min(Math.max(ratio, MIN_TILE_RATIO), MAX_TILE_RATIO)
}

/**
 * Translates the grid density (a column count, see `lib/gridDensity`) into the
 * row height the justified layout aims for: the width one of `columns` tiles
 * would get, read as the width of a landscape photograph
 * ({@link REFERENCE_TILE_RATIO}). Five columns therefore still means "about five
 * photos across" for an ordinary landscape shot, while portraits sit fewer to a
 * row and panoramas more — the density control keeps meaning what it meant, and
 * the wall stops cropping.
 *
 * A container too narrow to measure (or a nonsensical column count) yields
 * {@link MIN_ROW_HEIGHT}, never zero or a negative height.
 */
export function rowHeightForColumns(containerWidth: number, columns: number, gap: number): number {
  const cols = Number.isFinite(columns) && columns >= 1 ? Math.round(columns) : 1
  const gutter = Number.isFinite(gap) && gap > 0 ? gap : 0
  const usable = containerWidth - gutter * (cols - 1)
  if (!Number.isFinite(usable) || usable <= 0) {
    return MIN_ROW_HEIGHT
  }
  return Math.max(MIN_ROW_HEIGHT, usable / cols / REFERENCE_TILE_RATIO)
}

/**
 * Lays a run of aspect ratios out into justified rows.
 *
 * The rule is the greedy one every justified gallery uses: keep adding photos to
 * the row until scaling it to the container's width would push it *below* the
 * target height, then close whichever of the two candidate rows (with the last
 * photo, or without it) lands nearer the target. Every closed row is exactly
 * `containerWidth` wide, gutters included, with the rounding error absorbed by
 * its last tile so no row is a pixel short of the edge.
 *
 * A row cap ({@link JustifiedLayoutOptions.maxTilesPerRow}) cuts across that
 * rule: a row that has reached the cap is closed at whatever height its own
 * photographs give it — taller than the target, which is exactly the point on a
 * phone — rather than being allowed to grow past the cap.
 *
 * The final row is the exception: it is justified only while that does not
 * stretch it past {@link LAST_ROW_MAX_STRETCH}× the target. Beyond that — the
 * single leftover portrait — it keeps the target height and ends where its
 * photos end, because a wall that finishes with one enormous photograph reads as
 * a bug, not as a layout.
 *
 * A container with no usable width returns no rows at all: there is nothing to
 * lay out against, and a caller that renders nothing until it has measured
 * itself is doing the right thing.
 */
export function justifiedRows(
  ratios: readonly number[],
  { containerWidth, targetRowHeight, gap, maxTilesPerRow }: JustifiedLayoutOptions,
): JustifiedRow[] {
  const gutter = Number.isFinite(gap) && gap > 0 ? gap : 0
  const target = Number.isFinite(targetRowHeight) && targetRowHeight > 0 ? targetRowHeight : 0
  const cap =
    maxTilesPerRow !== undefined && Number.isFinite(maxTilesPerRow) && maxTilesPerRow >= 1
      ? Math.floor(maxTilesPerRow)
      : Number.POSITIVE_INFINITY
  if (!Number.isFinite(containerWidth) || containerWidth <= 0 || target <= 0) {
    return []
  }

  const rows: JustifiedRow[] = []
  // The row being filled: the indices of its photos and the sum of their ratios,
  // which together give its height at any width (height = usable ÷ Σ ratios).
  let start = 0
  let current: number[] = []
  let sum = 0

  const closeRow = (indices: readonly number[], ratioSum: number, height: number) => {
    rows.push(buildRow(start, indices, ratios, ratioSum, height, containerWidth, gutter))
    start += indices.length
  }

  for (let i = 0; i < ratios.length; i++) {
    const ratio = clampRatio(ratios[i] ?? DEFAULT_TILE_RATIO)
    current.push(i)
    sum += ratio
    const height = rowHeight(containerWidth, gutter, current.length, sum)
    // A row still short of the target keeps filling — unless it has reached the
    // cap, which closes it however tall it still is. Everything below is then
    // the ordinary close: a full row too is offered the choice of leaving its
    // last photo to the next one, where that lands nearer the target.
    if (height > target && current.length < cap) {
      continue
    }
    // Adding this photo took the row past full. Closing it *before* the photo
    // may land closer to the target height, in which case the photo opens the
    // next row instead.
    const without = rowHeight(containerWidth, gutter, current.length - 1, sum - ratio)
    if (current.length > 1 && Math.abs(without - target) < Math.abs(height - target)) {
      closeRow(current.slice(0, -1), sum - ratio, without)
      current = [i]
      sum = ratio
      continue
    }
    closeRow(current, sum, height)
    current = []
    sum = 0
  }

  if (current.length > 0) {
    const natural = rowHeight(containerWidth, gutter, current.length, sum)
    closeRow(current, sum, natural <= target * LAST_ROW_MAX_STRETCH ? natural : target)
  }
  return rows
}

/**
 * The index of the row holding tile `index`, or -1 when no row does. The rows
 * tile a contiguous run in order, so this is a binary search — the grid resolves
 * a photo index to a row on every scroll-to, and a library is tens of thousands
 * of photos long.
 */
export function rowOfTile(rows: readonly JustifiedRow[], index: number): number {
  let lo = 0
  let hi = rows.length - 1
  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    const row = rows.at(mid)
    if (row === undefined) {
      return -1
    }
    if (index < row.start) {
      hi = mid - 1
    } else if (index >= row.start + row.tiles.length) {
      lo = mid + 1
    } else {
      return mid
    }
  }
  return -1
}

/** The height a row of `count` tiles whose ratios sum to `ratioSum` takes at `width`. */
function rowHeight(width: number, gap: number, count: number, ratioSum: number): number {
  if (count <= 0 || ratioSum <= 0) {
    return Number.POSITIVE_INFINITY
  }
  return Math.max(MIN_ROW_HEIGHT, (width - gap * (count - 1)) / ratioSum)
}

/**
 * Turns a settled row (its photo indices, their ratio sum and its height) into
 * tile boxes. Widths are rounded to whole pixels and the remainder is handed to
 * the last tile, so a justified row measures exactly `containerWidth` and leaves
 * no hairline of background along the right edge. A row that is *not* justified
 * (the short last row) keeps its natural widths and simply ends early.
 */
function buildRow(
  start: number,
  indices: readonly number[],
  ratios: readonly number[],
  ratioSum: number,
  height: number,
  containerWidth: number,
  gap: number,
): JustifiedRow {
  const rounded = Math.max(MIN_ROW_HEIGHT, Math.round(height))
  const usable = containerWidth - gap * (indices.length - 1)
  // Only a row that actually fills the width may have its rounding error
  // absorbed; a short last row would be stretched by it.
  const justified = Math.abs(ratioSum * height - usable) < 1
  const tiles: JustifiedTile[] = []
  let used = 0
  for (let n = 0; n < indices.length; n++) {
    const index = indices[n] ?? start + n
    const ratio = clampRatio(ratios[index] ?? DEFAULT_TILE_RATIO)
    const last = n === indices.length - 1
    const width =
      last && justified ? Math.max(1, usable - used) : Math.max(1, Math.round(ratio * height))
    used += width
    tiles.push({ index, width })
  }
  return { start, height: rounded, tiles }
}
