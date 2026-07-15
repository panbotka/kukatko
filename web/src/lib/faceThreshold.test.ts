import { describe, expect, it } from 'vitest'

import {
  clampThresholdPercent,
  distanceToPercent,
  percentToDistance,
  THRESHOLD_DEFAULT_PERCENT,
  THRESHOLD_MAX_PERCENT,
  THRESHOLD_MIN_PERCENT,
} from './faceThreshold'

describe('percentToDistance', () => {
  it('maps a similarity percent to its complementary cosine distance', () => {
    expect(percentToDistance(50)).toBeCloseTo(0.5)
    expect(percentToDistance(20)).toBeCloseTo(0.8)
    expect(percentToDistance(80)).toBeCloseTo(0.2)
    expect(percentToDistance(100)).toBeCloseTo(0)
    expect(percentToDistance(0)).toBeCloseTo(1)
  })
})

describe('distanceToPercent', () => {
  it('inverts percentToDistance, rounded to a whole number', () => {
    expect(distanceToPercent(0.5)).toBe(50)
    expect(distanceToPercent(0.2)).toBe(80)
    expect(distanceToPercent(0.234)).toBe(77)
  })

  it('round-trips every slider stop', () => {
    for (let percent = THRESHOLD_MIN_PERCENT; percent <= THRESHOLD_MAX_PERCENT; percent += 5) {
      expect(distanceToPercent(percentToDistance(percent))).toBe(percent)
    }
  })
})

describe('clampThresholdPercent', () => {
  it('holds values inside the slider range', () => {
    expect(clampThresholdPercent(10)).toBe(THRESHOLD_MIN_PERCENT)
    expect(clampThresholdPercent(200)).toBe(THRESHOLD_MAX_PERCENT)
    expect(clampThresholdPercent(45)).toBe(45)
  })

  it('falls back to the default for a non-finite value', () => {
    expect(clampThresholdPercent(Number.NaN)).toBe(THRESHOLD_DEFAULT_PERCENT)
  })
})
