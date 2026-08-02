import { type TimelineBucket } from '../../services/photos'

/**
 * Minimum vertical distance between two rendered month ticks, in pixels. A mark
 * is 2 px tall, so ticks any closer than this stop reading as separate months
 * and merge into one continuous bar — which is what a real library (441 month
 * buckets in a 549 px rail) does without collapsing.
 */
export const TICK_MIN_GAP_PX = 6

/**
 * Minimum vertical distance between two rendered year labels, in pixels. A
 * caption-sized label is ~16 px tall, so this leaves a clear gap between two of
 * them; it is what makes "no two labels overlap" hold by construction rather
 * than by luck.
 */
export const LABEL_MIN_GAP_PX = 20

/**
 * Rail height assumed before the first measurement lands — and in environments
 * without layout, such as jsdom. It only has to be in the right order of
 * magnitude: the first paint is already thinned sensibly and the ResizeObserver
 * replaces it with the true height within a frame.
 */
export const FALLBACK_RAIL_HEIGHT_PX = 600

/** A stable key for a month bucket (year+month uniquely identifies it). */
export function bucketKey(bucket: TimelineBucket): string {
  return `${bucket.year}-${bucket.month}`
}

/**
 * The rail fraction (0..1) of the bucket at `rank`. Every month bucket owns an
 * equal slice of the rail and sits at its centre, so the rail is a scale of the
 * months the library actually contains — not of its photo counts.
 *
 * Positioning by photo count (`cumulative / total`) is what made the rail
 * unusable on a long-tailed archive: 121 years of history in which the last two
 * decades hold ~98 % of the photos collapsed six decades into a couple of
 * pixels. Per-month slices give a sparse year the same room as a dense one, so
 * the historical tail stays both visible and clickable.
 *
 * @see rankForFraction for the inverse used by drag, which must stay its exact mirror.
 */
export function fractionForRank(rank: number, count: number): number {
  if (count <= 0) {
    return 0
  }
  return (rank + 0.5) / count
}

/**
 * The bucket rank under a rail fraction (0..1) — the inverse of
 * {@link fractionForRank}, so a drag lands on exactly the month drawn under the
 * pointer. Out-of-range fractions clamp to the ends.
 */
export function rankForFraction(fraction: number, count: number): number {
  if (count <= 0) {
    return 0
  }
  const rank = Math.floor(fraction * count)
  return Math.min(count - 1, Math.max(0, rank))
}

/**
 * The rank of the bucket owning a grid `index`: the last bucket whose cumulative
 * start is at or before the index. Buckets are newest-first with ascending
 * cumulatives, so this maps a scroll position back to its month. Returns `-1`
 * only for an empty list.
 */
export function rankForIndex(buckets: TimelineBucket[], index: number): number {
  if (buckets.length === 0) {
    return -1
  }
  // Binary search: this runs on every scroll update, and a real library has
  // hundreds of buckets.
  let low = 0
  let high = buckets.length - 1
  let found = 0
  while (low <= high) {
    const mid = (low + high) >> 1
    if (buckets[mid].cumulative <= index) {
      found = mid
      low = mid + 1
    } else {
      high = mid - 1
    }
  }
  return found
}

/** One tick actually drawn on the rail, standing for one or more month buckets. */
export interface RailTick {
  /** Stable React key. */
  key: string
  /** Newest bucket of the collapsed range. */
  newest: TimelineBucket
  /** Oldest bucket of the collapsed range (equals `newest` when nothing collapsed). */
  oldest: TimelineBucket
  /**
   * The bucket a click jumps to: the newest of the range, except on the rail's
   * last tick, which anchors to the library's oldest month so the start of the
   * archive is always exactly one click away.
   */
  target: TimelineBucket
  /** Distance from the rail's top, in percent. */
  top: number
  /** The year to print beside the mark, or `null` when no label fits here. */
  year: number | null
  /** Rank of the newest bucket in the range (inclusive). */
  firstRank: number
  /** Rank of the oldest bucket in the range (inclusive). */
  lastRank: number
}

/**
 * Decides what the rail can actually show at `heightPx` pixels tall: it collapses
 * runs of month buckets into ticks no closer than {@link TICK_MIN_GAP_PX} and
 * prints a year label only where it clears the previous one by
 * {@link LABEL_MIN_GAP_PX}. Both are pure functions of the measured height, so a
 * library of 12 buckets renders all of them and one of 441 renders as many as fit
 * — a dozen readable years beat 103 illegible ones.
 *
 * The returned ticks partition the buckets: every bucket falls in exactly one
 * tick's `[firstRank, lastRank]` range, so nothing becomes unreachable and the
 * active month always highlights some tick. `heightPx <= 0` (not measured yet)
 * falls back to {@link FALLBACK_RAIL_HEIGHT_PX}.
 */
export function buildRail(buckets: TimelineBucket[], heightPx: number): RailTick[] {
  const count = buckets.length
  if (count === 0) {
    return []
  }
  const height = heightPx > 0 ? heightPx : FALLBACK_RAIL_HEIGHT_PX
  // Buckets per tick: how many have to merge before two marks are far enough
  // apart to be told apart.
  const perTick = Math.max(1, Math.ceil((TICK_MIN_GAP_PX * count) / height))

  const ticks: RailTick[] = []
  const tops: number[] = []
  let lastLabelTop = Number.NEGATIVE_INFINITY
  let lastLabelYear: number | null = null

  for (let firstRank = 0; firstRank < count; ) {
    let lastRank = Math.min(count - 1, firstRank + perTick - 1)
    // A trailing remainder too short to fill a slice is absorbed here instead of
    // becoming a tick of its own drawn too close to this one.
    if (count - lastRank - 1 < perTick) {
      lastRank = count - 1
    }
    const newest = buckets[firstRank]
    const oldest = buckets[lastRank]
    const target = lastRank === count - 1 ? oldest : newest
    const topFraction = (firstRank + (lastRank - firstRank + 1) / 2) / count
    const topPx = topFraction * height
    // A label is worth printing when it names a year no earlier label already
    // named and there is room for it below the previous one.
    const labelled = target.year !== lastLabelYear && topPx - lastLabelTop >= LABEL_MIN_GAP_PX
    if (labelled) {
      lastLabelTop = topPx
      lastLabelYear = target.year
    }
    ticks.push({
      key: bucketKey(newest),
      newest,
      oldest,
      target,
      top: topFraction * 100,
      year: labelled ? target.year : null,
      firstRank,
      lastRank,
    })
    tops.push(topPx)
    firstRank = lastRank + 1
  }

  // The last tick is the archive's first month, and the bottom of a long-tailed
  // rail is exactly where a reader needs a year printed. It always names its own,
  // even at the cost of the label above it — which is then the only one that can
  // sit too close, since the rest are a full gap apart from each other.
  const final = ticks[ticks.length - 1]
  if (final.year === null) {
    final.year = final.target.year
    for (let i = ticks.length - 2; i >= 0; i--) {
      if (ticks[i].year === null) {
        continue
      }
      if (
        tops[ticks.length - 1] - tops[i] < LABEL_MIN_GAP_PX ||
        ticks[i].year === final.target.year
      ) {
        ticks[i].year = null
      }
      break
    }
  }
  return ticks
}
