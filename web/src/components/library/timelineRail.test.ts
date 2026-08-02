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
