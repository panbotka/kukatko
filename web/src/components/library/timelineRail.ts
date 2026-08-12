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
 * The app's finger-target floor in pixels (2.75rem, the same 44 px every other
 * coarse-pointer rule in `app.css` uses), and therefore the minimum distance
 * between two year labels on a rail that is going to be *tapped*.
 *
 * A rail thinned for a mouse is unusable with a thumb: on production (390×844)
 * it drew 31 year ticks 16 px tall at a 20 px pitch and 62 month ticks 5 px
 * tall, against a fingertip of 34–45 px. Nothing about that is rescuable by CSS
 * alone — 44 px targets at a 20 px pitch simply overlap — so the *layout* has to
 * know: pass this as `buildRail`'s label gap and its labelled ticks come out a
 * finger apart, which is what lets each of them carry a 44 px box that touches
 * its neighbours without covering them. See {@link touchTargets} for what
 * happens to the ticks in between.
 */
export const TOUCH_TARGET_PX = 44

/**
 * Rail height assumed before the first measurement lands — and in environments
 * without layout, such as jsdom. It only has to be in the right order of
 * magnitude: the first paint is already thinned sensibly and the ResizeObserver
 * replaces it with the true height within a frame.
 */
export const FALLBACK_RAIL_HEIGHT_PX = 600

/**
 * The month a bucket covers, as the `YYYY-MM` anchor the library carries in its
 * URL so a jump survives Back, a reload and being shared. Zero-padded so the
 * value sorts and compares as text.
 */
export function anchorOf(bucket: TimelineBucket): string {
  return `${String(bucket.year).padStart(4, '0')}-${String(bucket.month).padStart(2, '0')}`
}

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
 * The number of calendar months the buckets span, counting both ends: one month
 * for a single bucket, 24 for two full years. The buckets are in grid order —
 * which may run either way — so this reads the two ends and takes the distance
 * between them, and it counts the *span*, not the buckets: an album holding one
 * photo from 1910 and one from 2026 spans 116 years, however few months of it
 * hold a photograph.
 */
export function spanMonths(buckets: TimelineBucket[]): number {
  if (buckets.length === 0) {
    return 0
  }
  const first = buckets[0]
  const last = buckets[buckets.length - 1]
  const months = (last.year - first.year) * 12 + (last.month - first.month)
  return Math.abs(months) + 1
}

/**
 * The rank of the bucket owning a grid `index`: the last bucket whose cumulative
 * start is at or before the index. Buckets are in grid order with ascending
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
  /** Newest bucket of the collapsed range, by date — whichever way the rail runs. */
  newest: TimelineBucket
  /** Oldest bucket of the collapsed range (equals `newest` when nothing collapsed). */
  oldest: TimelineBucket
  /**
   * The bucket a click jumps to: the first of the range in rail order, except on
   * the rail's last tick, which anchors to its final month so the far end of the
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
 * prints a year label only where it clears the previous one by `labelGapPx`.
 * Both are pure functions of the measured height, so a library of 12 buckets
 * renders all of them and one of 441 renders as many as fit — a dozen readable
 * years beat 103 illegible ones.
 *
 * `labelGapPx` defaults to {@link LABEL_MIN_GAP_PX}, which is what a year label
 * needs to be *read*. A rail that is going to be **tapped** asks for
 * {@link TOUCH_TARGET_PX} instead: there the labels are also the hit areas, so
 * the gap that matters is a fingertip, not a line of text.
 *
 * The returned ticks partition the buckets: every bucket falls in exactly one
 * tick's `[firstRank, lastRank]` range, so nothing becomes unreachable and the
 * active month always highlights some tick. `heightPx <= 0` (not measured yet)
 * falls back to {@link FALLBACK_RAIL_HEIGHT_PX}.
 */
export function buildRail(
  buckets: TimelineBucket[],
  heightPx: number,
  labelGapPx: number = LABEL_MIN_GAP_PX,
): RailTick[] {
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
    // Rail order, then date order: which end of the range a click lands on is a
    // question about the rail (its far end must stay one click away), while what
    // the tick is *called* is a question about dates — and an album read
    // oldest-first runs the rail the other way round from the library.
    const first = buckets[firstRank]
    const last = buckets[lastRank]
    const target = lastRank === count - 1 ? last : first
    const ascending =
      last.year > first.year || (last.year === first.year && last.month > first.month)
    const newest = ascending ? last : first
    const oldest = ascending ? first : last
    const topFraction = (firstRank + (lastRank - firstRank + 1) / 2) / count
    const topPx = topFraction * height
    // A label is worth printing when it names a year no earlier label already
    // named and there is room for it below the previous one.
    const labelled = target.year !== lastLabelYear && topPx - lastLabelTop >= labelGapPx
    if (labelled) {
      lastLabelTop = topPx
      lastLabelYear = target.year
    }
    ticks.push({
      key: bucketKey(first),
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

  // The last tick is the archive's far end, and the bottom of a long-tailed
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
      if (tops[ticks.length - 1] - tops[i] < labelGapPx || ticks[i].year === final.target.year) {
        ticks[i].year = null
      }
      break
    }
  }
  return ticks
}

/** The bucket's month as one comparable number, for ordering by date. */
function monthIndex(bucket: TimelineBucket): number {
  return bucket.year * 12 + bucket.month
}

/**
 * Reduces a rail to the ticks a **finger** can hit: its labelled ones, each
 * extended to own the buckets of the unlabelled ticks that follow it in rail
 * order.
 *
 * Built with a label gap of {@link TOUCH_TARGET_PX}, a rail's labelled ticks are
 * a fingertip apart and can each carry a 44 px box. The ticks between them stay
 * drawn — they are the scale's texture, the thing that makes the rail read as a
 * ruler rather than as a list of years — but they are 5 px tall and must not be
 * controls: a target that small is a mis-tap, and on this rail a mis-tap is a
 * jump of tens of thousands of pixels with no undo but scrolling back.
 *
 * Extending the ranges is what keeps that honest. The returned ticks still
 * **partition** the buckets — every month falls in exactly one of them — so
 * dropping the small ticks costs no reachability: a target that swallowed three
 * of them jumps to the first month of the range and names the whole range
 * (`oldest`/`newest`, which is what the "Jump to <from> – <to>" label reads), and
 * the active month still highlights exactly one target. What it costs is
 * precision: a tap lands on a range where it used to land on a month, and the
 * drag — which reads the rail's position directly, not its ticks — keeps the
 * month grain for a reader who wants it.
 *
 * The first tick of a rail is always labelled (`buildRail` prints a label
 * wherever one fits, and nothing precedes the first), so in practice no tick is
 * ever orphaned before the first target; a rail that somehow began unlabelled
 * would promote its first tick rather than drop it.
 */
export function touchTargets(ticks: RailTick[]): RailTick[] {
  const targets: RailTick[] = []
  for (const tick of ticks) {
    if (tick.year !== null || targets.length === 0) {
      // A copy: `buildRail`'s output is memoized by its caller and must not be
      // rewritten under it.
      targets.push({ ...tick })
      continue
    }
    const leader = targets[targets.length - 1]
    leader.lastRank = tick.lastRank
    leader.newest =
      monthIndex(tick.newest) > monthIndex(leader.newest) ? tick.newest : leader.newest
    leader.oldest =
      monthIndex(tick.oldest) < monthIndex(leader.oldest) ? tick.oldest : leader.oldest
  }
  return targets
}
