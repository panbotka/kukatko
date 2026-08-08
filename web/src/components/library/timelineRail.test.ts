import { describe, expect, it } from 'vitest'

import { realisticTimeline } from '../../test/timeline'

import {
  FALLBACK_RAIL_HEIGHT_PX,
  LABEL_MIN_GAP_PX,
  TICK_MIN_GAP_PX,
  buildRail,
  fractionForRank,
  rankForFraction,
  rankForIndex,
  spanMonths,
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
