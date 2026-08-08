import { describe, expect, it } from 'vitest'

import {
  FACE_SOURCE_REVIEW_BUDGET_PX,
  FACE_SOURCE_REVIEW_MAX,
  FACE_SOURCE_TILE_BUDGET_PX,
  faceSourceSize,
  OUTLIER_TARGET_PX,
  smallerFaceSource,
} from './faceSource'

import { type Bbox } from '../services/people'

/** A 4032x3024 landscape photo, the shape most of the catalogue is. */
const FRAME = { width: 4032, height: 3024 }

/**
 * Limits that lift the download budget, so a test about the ladder itself is not
 * also a test about what a tile is worth paying. The review views ask for exactly
 * this.
 */
const UNBUDGETED = { budgetPx: FACE_SOURCE_REVIEW_BUDGET_PX }

/** What the review views pass: the high ceiling and no budget at all. */
const REVIEW = { maxSize: FACE_SOURCE_REVIEW_MAX, budgetPx: FACE_SOURCE_REVIEW_BUDGET_PX }

/** A square-in-pixels crop `share` of the frame's width across. */
function crop(share: number): Bbox {
  return [0.1, 0.1, share, (share * FRAME.width) / FRAME.height]
}

describe('faceSourceSize', () => {
  it('cuts a big face from the smallest thumbnail', () => {
    // Half the frame across is ~2000px in the original and ~360px even in
    // fit_720 — far more than a tile needs.
    expect(faceSourceSize(crop(0.5), FRAME, 300)).toBe('fit_720')
  })

  it('climbs the ladder for a smaller face', () => {
    // ~0.1 of a 4032 frame is 403px; in fit_720 that is only 72px, so it has to
    // reach for a bigger source to put 300 real pixels across the tile.
    expect(faceSourceSize(crop(0.1), FRAME, 300, UNBUDGETED)).toBe('fit_1920')
  })

  it('takes the biggest available for a face too tiny for any of them', () => {
    // This is the Dana Levová case: a face ~2 % across the frame is 13px in
    // fit_720. No thumbnail makes it sharp, but the biggest is the least bad —
    // and it must not silently keep serving the 13px one.
    expect(faceSourceSize(crop(0.02), FRAME, 300, UNBUDGETED)).toBe('fit_1920')
  })

  it('never makes a 24px chip pay for a tile-sized thumbnail', () => {
    // The same small face that a tile escalates for is fine in a chip.
    expect(faceSourceSize(crop(0.1), FRAME, 48)).toBe('fit_720')
  })

  it('picks the smallest size that clears the target, not merely a bigger one', () => {
    // 0.06 x 4032 = 242px; fit_1280 scales by 1280/4032 = 0.318 → 77px, short of
    // 100. fit_1920 scales by 0.476 → 115px, which clears it.
    expect(faceSourceSize(crop(0.06), FRAME, 100, UNBUDGETED)).toBe('fit_1920')
    // Halve the target and the smaller source is enough.
    expect(faceSourceSize(crop(0.06), FRAME, 50, UNBUDGETED)).toBe('fit_1280')
  })

  it('accounts for the long side on a portrait frame', () => {
    // fit_N bounds the LONGEST side, so on a portrait photo the same N yields a
    // narrower thumbnail — the rule must scale by the height, not the width.
    const portrait = { width: 3024, height: 4032 }
    const share = 0.15
    const tall: Bbox = [0.1, 0.1, share, (share * portrait.width) / portrait.height]
    expect(faceSourceSize(tall, portrait, 300, UNBUDGETED)).toBe('fit_1920')
  })

  it('falls back to the smallest source for a degenerate crop or frame', () => {
    expect(faceSourceSize([0, 0, 0, 0], FRAME, 300)).toBe('fit_720')
    expect(faceSourceSize(crop(0.5), { width: 0, height: 0 }, 300)).toBe('fit_720')
  })

  it('only ever cuts from a full-frame fit size, never a cropped tile', () => {
    // A tile_* source is a centre-cropped square: the crop would land beside the
    // face. Whatever the inputs, the answer must be a fit_* size.
    for (const share of [0.01, 0.05, 0.2, 0.5, 0.9]) {
      expect(faceSourceSize(crop(share), FRAME, 300)).toMatch(/^fit_/)
    }
  })

  it('caps a chip at fit_1920 but lets a review crop climb higher', () => {
    // The same tiny face: a people-grid chip stops where a dense grid of chips
    // stops being worth the bytes, a card whose whole job is to be judged does not.
    expect(faceSourceSize(crop(0.02), FRAME, 300, UNBUDGETED)).toBe('fit_1920')
    expect(faceSourceSize(crop(0.02), FRAME, 300, REVIEW)).toBe('fit_3840')
  })

  it('still takes the smallest rung that clears the review target', () => {
    // A face that was already sharp at 720 must not be dragged up the ladder just
    // because the review ceiling is higher.
    expect(faceSourceSize(crop(0.4), FRAME, OUTLIER_TARGET_PX, REVIEW)).toBe('fit_720')
  })

  it('never asks for more resolution than the original holds', () => {
    // fit_* never upscales, so on a 1200px original every rung above 1280 is the
    // same pixels under a different URL — and a needless second cache entry.
    const small = { width: 1200, height: 800 }
    expect(faceSourceSize(crop(0.02), small, 300, REVIEW)).toBe('fit_1280')
  })
})

describe('faceSourceSize download budget', () => {
  it('refuses to spend a tile’s worth of page on a face that cannot be sharp', () => {
    // A face 2 % across a 12 Mpx frame is 13px in fit_720 and 38px in fit_1920 —
    // an upscale either way, at seven times the bytes. This is the whole reason
    // the people index pulled 125 Mpx to fill 72 squares of 152px.
    expect(faceSourceSize(crop(0.02), FRAME, 300)).toBe('fit_1280')
    expect(faceSourceSize(crop(0.1), FRAME, 300)).toBe('fit_1280')
  })

  it('measures the budget against the frame, not against the rung’s name', () => {
    // fit_* never upscales, so on a small original even the top rung costs only
    // what the original costs — a modest photo must not be punished for the sins
    // of a 24 Mpx one.
    const small = { width: 1000, height: 750 }
    expect(faceSourceSize(crop(0.02), small, 300)).toBe('fit_1280')
  })

  it('still leaves a big face on the cheapest rung that clears the target', () => {
    // The budget is a ceiling, never a floor: it must not drag a sharp face up
    // the ladder.
    expect(faceSourceSize(crop(0.5), FRAME, 300)).toBe('fit_720')
  })

  it('always answers with a rung, however small the budget', () => {
    // A budget below the bottom rung would otherwise leave the crop with no
    // image at all.
    expect(faceSourceSize(crop(0.5), FRAME, 300, { budgetPx: 1 })).toBe('fit_720')
    expect(faceSourceSize(crop(0.02), FRAME, 300, { budgetPx: 1 })).toBe('fit_720')
  })

  it('is generous enough for the fit_1280 rung on a phone-sized original', () => {
    // The tile budget is chosen to admit fit_1280 (~1.2 Mpx here) and refuse
    // fit_1920 (~2.8 Mpx) on the shape most of the catalogue is; pinning it keeps
    // a future retune honest about which rungs it moves.
    const long = Math.max(FRAME.width, FRAME.height)
    const pixels = (size: number) =>
      FRAME.width * FRAME.height * Math.min(1, size / long) * Math.min(1, size / long)
    expect(pixels(1280)).toBeLessThanOrEqual(FACE_SOURCE_TILE_BUDGET_PX)
    expect(pixels(1920)).toBeGreaterThan(FACE_SOURCE_TILE_BUDGET_PX)
  })
})

describe('smallerFaceSource', () => {
  it('steps one rung down the ladder', () => {
    expect(smallerFaceSource('fit_3840')).toBe('fit_2560')
    expect(smallerFaceSource('fit_2560')).toBe('fit_1920')
    expect(smallerFaceSource('fit_1280')).toBe('fit_720')
  })

  it('stops at the bottom rather than wrapping or looping', () => {
    // The card retries on error; a bottom that answered anything but null would
    // make a permanently missing thumbnail retry forever.
    expect(smallerFaceSource('fit_720')).toBeNull()
  })

  it('refuses a size that is not on the ladder', () => {
    expect(smallerFaceSource('tile_500')).toBeNull()
    expect(smallerFaceSource('')).toBeNull()
  })
})
