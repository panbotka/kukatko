import { describe, expect, it } from 'vitest'

import {
  REKEY_POLL_IDLE_MS,
  REKEY_POLL_RUNNING_MS,
  rekeyEtaSeconds,
  rekeyFraction,
  rekeyInProgress,
  rekeyPercent,
  rekeyPollInterval,
  rekeyRate,
  truncateMiddle,
} from './rekeyProgress'

describe('rekeyPollInterval', () => {
  it('polls a running move fast, anything else slowly and a done one never', () => {
    expect(rekeyPollInterval('running')).toBe(REKEY_POLL_RUNNING_MS)
    expect(rekeyPollInterval('paused')).toBe(REKEY_POLL_IDLE_MS)
    expect(rekeyPollInterval('failed')).toBe(REKEY_POLL_IDLE_MS)
    expect(rekeyPollInterval(null)).toBe(REKEY_POLL_IDLE_MS)
    expect(rekeyPollInterval('done')).toBeNull()
  })
})

describe('rekeyInProgress', () => {
  it('is true for running and paused only', () => {
    expect(rekeyInProgress('running')).toBe(true)
    expect(rekeyInProgress('paused')).toBe(true)
    expect(rekeyInProgress('done')).toBe(false)
    expect(rekeyInProgress('failed')).toBe(false)
  })
})

describe('rekeyFraction / rekeyPercent', () => {
  it('divides moved by total', () => {
    expect(rekeyFraction({ done_photos: 50, total_photos: 200 })).toBe(0.25)
    expect(rekeyPercent({ done_photos: 50, total_photos: 200 })).toBe(25)
  })

  it('rounds the percent down, so 100 means everything moved', () => {
    expect(rekeyPercent({ done_photos: 999, total_photos: 1000 })).toBe(99)
    expect(rekeyPercent({ done_photos: 1000, total_photos: 1000 })).toBe(100)
  })

  it('treats an empty run as complete and clamps an overshoot', () => {
    expect(rekeyFraction({ done_photos: 0, total_photos: 0 })).toBe(1)
    expect(rekeyFraction({ done_photos: 12, total_photos: 10 })).toBe(1)
  })
})

describe('rekeyRate', () => {
  it('is photos per minute between two polls', () => {
    expect(rekeyRate({ donePhotos: 100, at: 0 }, { donePhotos: 130, at: 3_000 })).toBe(600)
  })

  it('is unknown without an earlier sample, without elapsed time or after a reset', () => {
    expect(rekeyRate(null, { donePhotos: 10, at: 1_000 })).toBeNull()
    expect(rekeyRate({ donePhotos: 10, at: 1_000 }, { donePhotos: 12, at: 1_000 })).toBeNull()
    expect(rekeyRate({ donePhotos: 10, at: 0 }, { donePhotos: 4, at: 3_000 })).toBeNull()
  })

  it('is zero when nothing moved', () => {
    expect(rekeyRate({ donePhotos: 10, at: 0 }, { donePhotos: 10, at: 3_000 })).toBe(0)
  })
})

describe('rekeyEtaSeconds', () => {
  it('divides what is left by the rate', () => {
    expect(rekeyEtaSeconds({ done_photos: 400, total_photos: 1000 }, 600)).toBe(60)
  })

  it('is unknown for an unknown or zero rate', () => {
    expect(rekeyEtaSeconds({ done_photos: 1, total_photos: 10 }, null)).toBeNull()
    expect(rekeyEtaSeconds({ done_photos: 1, total_photos: 10 }, 0)).toBeNull()
  })

  it('is zero when nothing is left, whatever the rate', () => {
    expect(rekeyEtaSeconds({ done_photos: 10, total_photos: 10 }, null)).toBe(0)
  })
})

describe('truncateMiddle', () => {
  it('leaves a short text alone', () => {
    expect(truncateMiddle('2019/07/a.jpg', 20)).toBe('2019/07/a.jpg')
  })

  it('cuts the middle, keeping the start and the end', () => {
    const out = truncateMiddle('2019/07/IMG_20190714_123456_0042.jpg', 15)
    expect(out).toBe('2019/07…042.jpg')
    expect(Array.from(out)).toHaveLength(15)
  })

  it('degrades to the ellipsis for a tiny budget', () => {
    expect(truncateMiddle('abcdef', 1)).toBe('…')
    expect(truncateMiddle('abcdef', 2)).toBe('a…')
  })
})
