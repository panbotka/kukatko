import { describe, expect, it } from 'vitest'

import {
  clampExpandLimit,
  clampExpandThresholdPercent,
  EXPAND_LIMIT_DEFAULT,
  EXPAND_LIMIT_MAX,
  EXPAND_LIMIT_MIN,
  EXPAND_THRESHOLD_DEFAULT_PERCENT,
  expandSources,
  expandThresholdDistance,
  similarityPercent,
  type ExpandSource,
} from './expandSearch'

describe('expandThresholdDistance', () => {
  it('converts the similarity percentage into a cosine distance', () => {
    // The UI never shows a distance; this is the one place percent becomes one.
    expect(expandThresholdDistance(70)).toBe(0.3)
    expect(expandThresholdDistance(20)).toBe(0.8)
    expect(expandThresholdDistance(80)).toBe(0.2)
  })

  it('emits values free of float noise, safe for a URL round-trip', () => {
    // 1 - 0.65 is 0.35000000000000003 in raw IEEE 754; the query stays clean.
    expect(expandThresholdDistance(65)).toBe(0.35)
    expect(String(expandThresholdDistance(65))).toBe('0.35')
  })

  it('maps the default percentage to the backend default distance', () => {
    expect(expandThresholdDistance(EXPAND_THRESHOLD_DEFAULT_PERCENT)).toBe(0.3)
  })
})

describe('clampExpandThresholdPercent', () => {
  it('keeps in-range values', () => {
    expect(clampExpandThresholdPercent(55)).toBe(55)
  })

  it('clamps to the slider bounds', () => {
    expect(clampExpandThresholdPercent(5)).toBe(20)
    expect(clampExpandThresholdPercent(95)).toBe(80)
  })

  it('falls back to the expand default for a non-numeric value', () => {
    expect(clampExpandThresholdPercent(Number.NaN)).toBe(EXPAND_THRESHOLD_DEFAULT_PERCENT)
    expect(clampExpandThresholdPercent(Number.POSITIVE_INFINITY)).toBe(
      EXPAND_THRESHOLD_DEFAULT_PERCENT,
    )
  })
})

describe('clampExpandLimit', () => {
  it('keeps in-range values, truncating fractions', () => {
    expect(clampExpandLimit(50)).toBe(50)
    expect(clampExpandLimit(12.9)).toBe(12)
  })

  it('clamps to 1–200', () => {
    expect(clampExpandLimit(0)).toBe(EXPAND_LIMIT_MIN)
    expect(clampExpandLimit(-3)).toBe(EXPAND_LIMIT_MIN)
    expect(clampExpandLimit(500)).toBe(EXPAND_LIMIT_MAX)
  })

  it('falls back to the default for a non-numeric value', () => {
    expect(clampExpandLimit(Number.NaN)).toBe(EXPAND_LIMIT_DEFAULT)
  })
})

describe('expandSources', () => {
  const sources: ExpandSource[] = [
    { uid: 'a', name: 'Empty', photoCount: 0 },
    { uid: 'b', name: 'Beta', photoCount: 12 },
    { uid: 'c', name: 'Alpha', photoCount: 12 },
    { uid: 'd', name: 'Big', photoCount: 300 },
  ]

  it('excludes collections with zero photos', () => {
    expect(expandSources(sources).map((s) => s.uid)).not.toContain('a')
  })

  it('sorts by photo count descending, then by name', () => {
    expect(expandSources(sources).map((s) => s.uid)).toEqual(['d', 'c', 'b'])
  })

  it('leaves the input array untouched', () => {
    const before = sources.map((s) => s.uid)
    expandSources(sources)
    expect(sources.map((s) => s.uid)).toEqual(before)
  })
})

describe('similarityPercent', () => {
  it('rounds the cosine similarity to a whole percentage', () => {
    expect(similarityPercent(0.914)).toBe(91)
    expect(similarityPercent(0.915)).toBe(92)
    expect(similarityPercent(1)).toBe(100)
  })
})
