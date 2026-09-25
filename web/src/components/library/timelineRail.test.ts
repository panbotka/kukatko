import { describe, expect, it } from 'vitest'

import { type TimelineBucket } from '../../services/photos'
import { realisticTimeline } from '../../test/timeline'

import {
  FALLBACK_RAIL_HEIGHT_PX,
  LABEL_MIN_GAP_PX,
  TICK_MIN_GAP_PX,
  TOUCH_TARGET_PX,
  buildRail,
  foldImplausible,
  fractionForRank,
  rankForFraction,
  rankForIndex,
  spanMonths,
  touchTargets,
} from './timelineRail'

/** Rail heights a real viewport produces, from a short laptop to a tall monitor. */
const HEIGHTS = [120, 300, 549, 900, 1400]

const { buckets } = realisticTimeline()

describe('timelineRail positions', () => {
  it('maps a rank to a fraction and back to the same rank', () => {
    // This is the invariant the whole rail rests on: the position a tick is
    // drawn at and the position a drag reads back have to name the same month,
    // otherwise every drag lands in the wrong one.
    const count = buckets.length
    for (let rank = 0; rank < count; rank++) {
      expect(rankForFraction(fractionForRank(rank, count), count)).toBe(rank)
    }
  })

  it('clamps out-of-range fractions to the ends of the rail', () => {
    expect(rankForFraction(-0.5, 10)).toBe(0)
    expect(rankForFraction(0, 10)).toBe(0)
    expect(rankForFraction(1, 10)).toBe(9)
    expect(rankForFraction(2, 10)).toBe(9)
  })

  it('finds the bucket owning a photo index', () => {
    const count = buckets.length
    expect(rankForIndex(buckets, 0)).toBe(0)
    expect(rankForIndex(buckets, buckets[0].count - 1)).toBe(0)
    expect(rankForIndex(buckets, buckets[0].count)).toBe(1)
    expect(rankForIndex(buckets, buckets[count - 1].cumulative)).toBe(count - 1)
    // Past the last bucket (undated photos sort after every month) stays on the
    // oldest bucket rather than running off the end.
    expect(rankForIndex(buckets, Number.MAX_SAFE_INTEGER)).toBe(count - 1)
    expect(rankForIndex([], 0)).toBe(-1)
  })
})

describe('buildRail', () => {
  it('starts from a genuinely long-tailed library', () => {
    // Guards the fixture itself: the thinning below only proves anything if the
    // input is the shape production has.
    expect(buckets.length).toBeGreaterThan(400)
    expect(buckets[0].year - buckets[buckets.length - 1].year).toBeGreaterThanOrEqual(120)
    const total = buckets.reduce((sum, bucket) => sum + bucket.count, 0)
    expect(total).toBeGreaterThan(10000)
  })

  it.each(HEIGHTS)('keeps ticks and labels apart at %i px', (height) => {
    const ticks = buildRail(buckets, height)
    expect(ticks.length).toBeGreaterThan(0)

    const tops = ticks.map((tick) => (tick.top / 100) * height)
    for (let i = 1; i < tops.length; i++) {
      // Floating-point slack only; the gap is a genuine >= TICK_MIN_GAP_PX.
      expect(tops[i] - tops[i - 1]).toBeGreaterThan(TICK_MIN_GAP_PX - 1e-9)
    }

    const labels = ticks.filter((tick) => tick.year !== null)
    const labelTops = labels.map((tick) => (tick.top / 100) * height)
    for (let i = 1; i < labelTops.length; i++) {
      expect(labelTops[i] - labelTops[i - 1]).toBeGreaterThan(LABEL_MIN_GAP_PX - 1e-9)
    }
    // Years read as a scale: strictly decreasing downwards, never repeated.
    const years = labels.map((tick) => tick.year)
    expect(years).toEqual([...years].sort((a, b) => (b ?? 0) - (a ?? 0)))
    expect(new Set(years).size).toBe(years.length)
    // Something readable is actually drawn, and never more than fits.
    expect(labels.length).toBeGreaterThanOrEqual(4)
    expect(labels.length).toBeLessThanOrEqual(Math.ceil(height / LABEL_MIN_GAP_PX))
  })

  it.each(HEIGHTS)('covers every bucket exactly once at %i px', (height) => {
    const ticks = buildRail(buckets, height)
    expect(ticks[0].firstRank).toBe(0)
    expect(ticks[ticks.length - 1].lastRank).toBe(buckets.length - 1)
    for (let i = 0; i < ticks.length; i++) {
      expect(ticks[i].lastRank).toBeGreaterThanOrEqual(ticks[i].firstRank)
      if (i > 0) {
        expect(ticks[i].firstRank).toBe(ticks[i - 1].lastRank + 1)
      }
    }
  })

  it.each(HEIGHTS)('anchors the ends of the rail to the ends of the library at %i px', (height) => {
    const ticks = buildRail(buckets, height)
    const oldest = buckets[buckets.length - 1]
    expect(ticks[0].target).toBe(buckets[0])
    expect(ticks[0].year).toBe(buckets[0].year)
    // Whatever the thinning does in between, the last tick names the archive's
    // very first year and reaches its very first month — that is what keeps 1905
    // both visible and one click away, at any rail height.
    expect(ticks[ticks.length - 1].target).toBe(oldest)
    expect(ticks[ticks.length - 1].year).toBe(oldest.year)
  })

  it('draws every bucket of a small library', () => {
    const small = buckets.slice(0, 10)
    const ticks = buildRail(small, 549)
    expect(ticks).toHaveLength(10)
    expect(ticks.every((tick) => tick.firstRank === tick.lastRank)).toBe(true)
  })

  it('falls back to a nominal height before the rail is measured', () => {
    expect(buildRail(buckets, 0)).toEqual(buildRail(buckets, FALLBACK_RAIL_HEIGHT_PX))
    expect(buildRail([], 549)).toEqual([])
  })
})

/**
 * The rail as a *touch* control. Its ticks used to be laid out for a mouse and
 * then handed to a thumb: on production (390×844, coarse pointer) that was 31
 * year ticks 16px tall at a 20px pitch and 62 month ticks 5px tall. A 44px box
 * cannot be given to a tick 20px from its neighbour — the two would overlap and
 * the one under the finger would not be the one that answers — so the *layout*
 * is what has to know, and this is where it is pinned.
 */
describe('buildRail for a finger', () => {
  it.each(HEIGHTS)('keeps the year ticks a fingertip apart at %i px', (height) => {
    const ticks = buildRail(buckets, height, TOUCH_TARGET_PX)
    const labels = ticks.filter((tick) => tick.year !== null)
    const tops = labels.map((tick) => (tick.top / 100) * height)
    for (let i = 1; i < tops.length; i++) {
      // Floating-point slack only. This is the whole point: 44px boxes centred
      // on these positions touch their neighbours and never cover them.
      expect(tops[i] - tops[i - 1]).toBeGreaterThan(TOUCH_TARGET_PX - 1e-9)
    }
    // …and the rail is still a scale, not two labels and a gap.
    expect(labels.length).toBeGreaterThanOrEqual(2)
  })

  it('draws fewer, bigger targets than the mouse rail does', () => {
    const mouse = buildRail(buckets, 549).filter((tick) => tick.year !== null)
    const finger = buildRail(buckets, 549, TOUCH_TARGET_PX).filter((tick) => tick.year !== null)
    expect(finger.length).toBeLessThan(mouse.length)
    // Enough years survive to steer a 121-year archive by.
    expect(finger.length).toBeGreaterThanOrEqual(8)
    // The texture is untouched: only which ticks are *labelled* changes, so the
    // rail still reads as a ruler rather than as a list of years.
    expect(buildRail(buckets, 549, TOUCH_TARGET_PX).length).toBe(buildRail(buckets, 549).length)
  })
})

describe('touchTargets', () => {
  const height = 549
  const ticks = buildRail(buckets, height, TOUCH_TARGET_PX)
  const targets = touchTargets(ticks)

  it('keeps only the ticks big enough to be tapped', () => {
    expect(targets.length).toBeLessThan(ticks.length)
    expect(targets.every((tick) => tick.year !== null)).toBe(true)
    // Every survivor is a tick that was actually drawn, at the position it was
    // drawn at — the target is the label the reader is aiming for.
    const drawn = new Map(ticks.map((tick) => [tick.key, tick]))
    expect(targets.every((tick) => drawn.get(tick.key)?.top === tick.top)).toBe(true)
  })

  it('still partitions the buckets, so no month becomes unreachable', () => {
    expect(targets[0].firstRank).toBe(0)
    expect(targets[targets.length - 1].lastRank).toBe(buckets.length - 1)
    for (let i = 0; i < targets.length; i++) {
      expect(targets[i].lastRank).toBeGreaterThanOrEqual(targets[i].firstRank)
      if (i > 0) {
        expect(targets[i].firstRank).toBe(targets[i - 1].lastRank + 1)
      }
    }
  })

  it('names the whole range it swallowed, by date', () => {
    const asDate = (b: { year: number; month: number }) => b.year * 12 + b.month
    const widened = targets.find((tick) => tick.firstRank !== tick.lastRank)
    expect(widened).toBeDefined()
    for (const target of targets) {
      expect(asDate(target.oldest)).toBeLessThanOrEqual(asDate(target.newest))
      // The label a reader hears must cover the months the tap will land in.
      for (let rank = target.firstRank; rank <= target.lastRank; rank++) {
        expect(asDate(buckets[rank])).toBeGreaterThanOrEqual(asDate(target.oldest))
        expect(asDate(buckets[rank])).toBeLessThanOrEqual(asDate(target.newest))
      }
    }
  })

  it('keeps both ends of the archive one tap away', () => {
    expect(targets[0].target).toBe(buckets[0])
    expect(targets[targets.length - 1].target).toBe(buckets[buckets.length - 1])
  })

  it('leaves the ticks it was given alone', () => {
    // `buildRail`'s output is memoized by the component; rewriting it in place
    // would leak the touch ranges into the mouse rail on the next render.
    const before = buildRail(buckets, height, TOUCH_TARGET_PX)
    touchTargets(before)
    expect(before).toEqual(buildRail(buckets, height, TOUCH_TARGET_PX))
  })

  it('is the identity on a rail whose ticks are all labelled', () => {
    // The archive's tail: one bucket per year, so every tick names a year of its
    // own and none of them has to be given up.
    const small = buildRail(buckets.slice(-6), height, TOUCH_TARGET_PX)
    expect(small.every((tick) => tick.year !== null)).toBe(true)
    expect(touchTargets(small)).toEqual(small)
  })
})

describe('timelineRail span', () => {
  it('measures the distance between the two ends, in months, counting both', () => {
    expect(spanMonths([])).toBe(0)
    expect(spanMonths([{ year: 2026, month: 2, count: 1, cumulative: 0 }])).toBe(1)
    expect(
      spanMonths([
        { year: 2026, month: 2, count: 1, cumulative: 0 },
        { year: 2026, month: 1, count: 1, cumulative: 1 },
      ]),
    ).toBe(2)
    // Two full calendar years, the threshold an album is given a rail at.
    expect(
      spanMonths([
        { year: 2025, month: 12, count: 1, cumulative: 0 },
        { year: 2024, month: 1, count: 1, cumulative: 1 },
      ]),
    ).toBe(24)
  })

  it('is the same span whichever way the buckets run', () => {
    // An album read oldest-first arrives ascending; the rail is no shorter for it.
    const descending = [
      { year: 2026, month: 3, count: 1, cumulative: 0 },
      { year: 1910, month: 6, count: 1, cumulative: 1 },
    ]
    const ascending = [
      { year: 1910, month: 6, count: 1, cumulative: 0 },
      { year: 2026, month: 3, count: 1, cumulative: 1 },
    ]
    expect(spanMonths(ascending)).toBe(spanMonths(descending))
    expect(spanMonths(ascending)).toBe((2026 - 1910) * 12 + (3 - 6) + 1)
  })

  it('counts the months between the ends, not the buckets that hold photos', () => {
    // One photo from 1910 and one from 2026 is two buckets and 116 years.
    const sparse = [
      { year: 1910, month: 1, count: 1, cumulative: 0 },
      { year: 2026, month: 1, count: 1, cumulative: 1 },
    ]
    expect(spanMonths(sparse)).toBe(116 * 12 + 1)
  })
})

describe('buildRail on an ascending rail', () => {
  // The album grid runs oldest-first, so its histogram does too. Everything the
  // rail draws is in rail order; only what a tick is *called* is about dates.
  const ascending = [...buckets].reverse().map((bucket, index, all) => ({
    ...bucket,
    cumulative: all.slice(0, index).reduce((sum, b) => sum + b.count, 0),
  }))

  it('anchors both ends of the rail to both ends of the album', () => {
    const ticks = buildRail(ascending, 549)
    expect(ticks[0].target).toBe(ascending[0])
    expect(ticks[ticks.length - 1].target).toBe(ascending[ascending.length - 1])
    expect(ticks[ticks.length - 1].year).toBe(ascending[ascending.length - 1].year)
  })

  it('names a collapsed tick by date, not by rail position', () => {
    // `oldest`/`newest` feed the "Jump to <from> – <to>" label; read off rail
    // position they would name the range backwards on an ascending rail.
    const ticks = buildRail(ascending, 120)
    const collapsed = ticks.find((tick) => tick.firstRank !== tick.lastRank)
    expect(collapsed).toBeDefined()
    if (collapsed === undefined) {
      return
    }
    const asDate = (b: { year: number; month: number }) => b.year * 12 + b.month
    expect(asDate(collapsed.oldest)).toBeLessThan(asDate(collapsed.newest))
  })

  it('keeps a key per tick that no other tick shares', () => {
    const ticks = buildRail(ascending, 300)
    expect(new Set(ticks.map((tick) => tick.key)).size).toBe(ticks.length)
  })
})

describe('implausible years', () => {
  // The production timeline with what box staging actually carried on top of it:
  // photos a Facebook file name had dated to 9009, which sort before 2026 in the
  // newest-first grid — two months of them here, to prove a run folds into one.
  const bogus: TimelineBucket[] = [
    { year: 9009, month: 3, count: 1, cumulative: 0, implausible: true },
    { year: 9009, month: 1, count: 2, cumulative: 1, implausible: true },
  ]
  const shift = 3
  const raw = [...bogus, ...buckets.map((b) => ({ ...b, cumulative: b.cumulative + shift }))]
  const folded = foldImplausible(raw)

  it('hands back a timeline with nothing implausible as the very same array', () => {
    expect(foldImplausible(buckets)).toBe(buckets)
  })

  it('folds a run of implausible months into one band where its photos sit', () => {
    expect(folded).toHaveLength(buckets.length + 1)
    expect(folded[0]).toEqual({ year: 9009, month: 3, count: 3, cumulative: 0, implausible: true })
    // Real months come through untouched — the same objects, so nothing below
    // the band re-renders for it.
    expect(folded[1]).toBe(raw[2])
    expect(folded.slice(1).every((b) => b.implausible !== true)).toBe(true)
  })

  it('folds each end of the date order into a band of its own', () => {
    const both = foldImplausible([
      raw[0],
      raw[2],
      raw[3],
      { year: 1700, month: 5, count: 4, cumulative: 99, implausible: true },
    ])
    expect(both.map((b) => b.implausible === true)).toEqual([true, false, false, true])
    expect(both[3].count).toBe(4)
  })

  it('keeps every grid index mapped to its band or month', () => {
    // The band's photos are the grid's first three: a scroll there is "in the
    // band", and the fourth photo is 2026 again.
    expect(rankForIndex(folded, 0)).toBe(0)
    expect(rankForIndex(folded, 2)).toBe(0)
    expect(rankForIndex(folded, shift)).toBe(1)
  })

  it('does not let the band stretch the span of the timeline', () => {
    expect(spanMonths(folded)).toBe(spanMonths(buckets))
    expect(spanMonths(folded.slice(0, 1))).toBe(0)
  })

  for (const [name, gap] of [
    ['a mouse', LABEL_MIN_GAP_PX],
    ['a finger', TOUCH_TARGET_PX],
  ] as const) {
    describe(`on a rail for ${name}`, () => {
      for (const height of HEIGHTS) {
        const ticks = buildRail(folded, height, gap)

        it(`offers no impossible year at ${height}px`, () => {
          // The regression itself: the top of the rail read "9009". Every year
          // a tick names is now a real one; the band is a tick of its own.
          const named = ticks.filter((tick) => tick.year !== null && !tick.implausible)
          expect(named.every((tick) => tick.year !== null && tick.year <= 2026)).toBe(true)
          expect(ticks[0].implausible).toBe(true)
          expect(ticks.filter((tick) => tick.implausible)).toHaveLength(1)
        })

        it(`keeps the band reachable and on its own at ${height}px`, () => {
          const band = ticks[0]
          expect([band.firstRank, band.lastRank]).toEqual([0, 0])
          expect(band.target).toBe(folded[0])
          // Labelled, so a finger has a target on it.
          expect(band.year).not.toBeNull()
          // No tick of real months reaches into it.
          expect(ticks.slice(1).every((tick) => tick.firstRank >= 1)).toBe(true)
        })

        it(`still partitions every bucket at ${height}px`, () => {
          const rails = gap === TOUCH_TARGET_PX ? [ticks, touchTargets(ticks)] : [ticks]
          for (const rail of rails) {
            expect(rail[0].firstRank).toBe(0)
            expect(rail[rail.length - 1].lastRank).toBe(folded.length - 1)
            for (let i = 1; i < rail.length; i++) {
              expect(rail[i].firstRank).toBe(rail[i - 1].lastRank + 1)
            }
          }
        })

        it(`still prints the first real year once the band has had its label at ${height}px`, () => {
          // The band names no year, so it must not stop the year after it from
          // counting as new.
          const firstYear = ticks.find((tick) => !tick.implausible && tick.year !== null)
          expect(firstYear).toBeDefined()
        })
      }
    })
  }

  it('keeps the band a target of its own for a finger', () => {
    const targets = touchTargets(buildRail(folded, 549, TOUCH_TARGET_PX))
    expect(targets[0].implausible).toBe(true)
    expect(targets[0].target).toBe(folded[0])
    expect(targets.slice(1).every((tick) => !tick.implausible)).toBe(true)
  })

  it('ends an oldest-first rail on the band, labelled', () => {
    // An album reads oldest-first, so a date nobody can have taken a photo on
    // sits at the bottom — the rail's far end, which always names itself.
    const ascending = [...folded].reverse()
    for (const height of HEIGHTS) {
      const ticks = buildRail(ascending, height)
      const final = ticks[ticks.length - 1]
      expect(final.implausible).toBe(true)
      expect([final.firstRank, final.lastRank]).toEqual([
        ascending.length - 1,
        ascending.length - 1,
      ])
      expect(final.year).not.toBeNull()
      expect(ticks.slice(0, -1).every((tick) => !tick.implausible)).toBe(true)
    }
  })
})
