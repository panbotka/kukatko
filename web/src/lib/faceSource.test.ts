import { describe, expect, it } from 'vitest'

import { faceSourceSize } from './faceSource'

import { type Bbox } from '../services/people'

/** A 4032x3024 landscape photo, the shape most of the catalogue is. */
const FRAME = { width: 4032, height: 3024 }

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
    expect(faceSourceSize(crop(0.1), FRAME, 300)).toBe('fit_1920')
  })

  it('takes the biggest available for a face too tiny for any of them', () => {
    // This is the Dana Levová case: a face ~2 % across the frame is 13px in
    // fit_720. No thumbnail makes it sharp, but the biggest is the least bad —
    // and it must not silently keep serving the 13px one.
    expect(faceSourceSize(crop(0.02), FRAME, 300)).toBe('fit_1920')
  })

  it('never makes a 24px chip pay for a tile-sized thumbnail', () => {
    // The same small face that a tile escalates for is fine in a chip.
    expect(faceSourceSize(crop(0.1), FRAME, 48)).toBe('fit_720')
  })

  it('picks the smallest size that clears the target, not merely a bigger one', () => {
    // 0.06 x 4032 = 242px; fit_1280 scales by 1280/4032 = 0.318 → 77px, short of
    // 100. fit_1920 scales by 0.476 → 115px, which clears it.
    expect(faceSourceSize(crop(0.06), FRAME, 100)).toBe('fit_1920')
    // Halve the target and the smaller source is enough.
    expect(faceSourceSize(crop(0.06), FRAME, 50)).toBe('fit_1280')
  })

  it('accounts for the long side on a portrait frame', () => {
    // fit_N bounds the LONGEST side, so on a portrait photo the same N yields a
    // narrower thumbnail — the rule must scale by the height, not the width.
    const portrait = { width: 3024, height: 4032 }
    const share = 0.15
    const tall: Bbox = [0.1, 0.1, share, (share * portrait.width) / portrait.height]
    expect(faceSourceSize(tall, portrait, 300)).toBe('fit_1920')
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
})
