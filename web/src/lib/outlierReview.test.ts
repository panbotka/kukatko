import { describe, expect, it } from 'vitest'

import { type OutlierFace } from '../services/people'

import {
  canUnassign,
  clampOutlierThresholdPercent,
  distancePercent,
  isActionable,
  OUTLIER_THRESHOLD_DEFAULT_PERCENT,
  OUTLIER_THRESHOLD_MAX_PERCENT,
  OUTLIER_THRESHOLD_MIN_PERCENT,
  outlierKey,
  outlierThresholdDistance,
  toOutlierItems,
} from './outlierReview'

/** A minimal outlier face; overrides tailor it per case. */
function face(overrides: Partial<OutlierFace> = {}): OutlierFace {
  return {
    photo_uid: 'ph1',
    face_index: 0,
    bbox: [0.4, 0.3, 0.2, 0.2],
    det_score: 0.9,
    distance: 0.42,
    marker_uid: 'mk1',
    width: 1200,
    height: 800,
    orientation: 1,
    ...overrides,
  }
}

describe('outlierKey', () => {
  it('identifies a face by its photo and slot', () => {
    expect(outlierKey(face({ photo_uid: 'ph9', face_index: 3 }))).toBe('ph9:3')
  })

  it('separates two faces in the same photo', () => {
    expect(outlierKey(face({ face_index: 0 }))).not.toBe(outlierKey(face({ face_index: 1 })))
  })
})

describe('toOutlierItems', () => {
  it('seeds every face as pending', () => {
    const items = toOutlierItems([face({ face_index: 0 }), face({ face_index: 1 })])
    expect(items).toHaveLength(2)
    expect(items.every((item) => item.status === 'pending')).toBe(true)
  })
})

describe('isActionable', () => {
  it('counts a pending face, and an errored one — its write failed, so it is still undecided', () => {
    expect(isActionable({ face: face(), status: 'pending' })).toBe(true)
    expect(isActionable({ face: face(), status: 'error' })).toBe(true)
  })

  it('does not count a face already decided', () => {
    expect(isActionable({ face: face(), status: 'removed' })).toBe(false)
    expect(isActionable({ face: face(), status: 'confirmed' })).toBe(false)
  })
})

describe('canUnassign', () => {
  it('needs a marker: there is nothing to detach without one', () => {
    expect(canUnassign(face({ marker_uid: 'mk1' }))).toBe(true)
    expect(canUnassign(face({ marker_uid: '' }))).toBe(false)
    expect(canUnassign(face({ marker_uid: undefined }))).toBe(false)
  })
})

describe('clampOutlierThresholdPercent', () => {
  it('keeps a value inside the slider range', () => {
    expect(clampOutlierThresholdPercent(50)).toBe(50)
    expect(clampOutlierThresholdPercent(-10)).toBe(OUTLIER_THRESHOLD_MIN_PERCENT)
    expect(clampOutlierThresholdPercent(9000)).toBe(OUTLIER_THRESHOLD_MAX_PERCENT)
  })

  it('falls back to the default for a garbled URL parameter', () => {
    expect(clampOutlierThresholdPercent(Number('abc'))).toBe(OUTLIER_THRESHOLD_DEFAULT_PERCENT)
  })
})

describe('outlierThresholdDistance', () => {
  it('maps the slider onto the cosine distance the endpoint takes', () => {
    expect(outlierThresholdDistance(0)).toBe(0)
    expect(outlierThresholdDistance(50)).toBe(0.5)
    expect(outlierThresholdDistance(100)).toBe(1)
  })

  it('shows everything at the default, which is what 0 means to the endpoint', () => {
    expect(outlierThresholdDistance(OUTLIER_THRESHOLD_DEFAULT_PERCENT)).toBe(0)
  })

  it('clamps out-of-range input rather than passing it to the API', () => {
    expect(outlierThresholdDistance(-5)).toBe(0)
    expect(outlierThresholdDistance(150)).toBe(1)
  })

  it('rounds off float noise so the value survives the URL', () => {
    // 35 % is the classic 0.35000000000000003 case.
    expect(String(outlierThresholdDistance(35))).toBe('0.35')
  })
})

describe('distancePercent', () => {
  it('reads as distance, not similarity: bigger means further from the person', () => {
    expect(distancePercent(0)).toBe(0)
    expect(distancePercent(0.42)).toBe(42)
    expect(distancePercent(1)).toBe(100)
  })
})
