import { describe, expect, it } from 'vitest'

import { displayFrame, padBbox, squareCrop } from './faceGeometry'

import { type Bbox } from '../services/people'

describe('displayFrame', () => {
  it('keeps the frame for an upright photo', () => {
    expect(displayFrame(4000, 3000, 1)).toEqual({ width: 4000, height: 3000 })
  })

  it('keeps the frame when the orientation tag is absent', () => {
    expect(displayFrame(4000, 3000, 0)).toEqual({ width: 4000, height: 3000 })
  })

  it('swaps the sides for the quarter-turn orientations', () => {
    for (const orientation of [5, 6, 7, 8]) {
      expect(displayFrame(4000, 3000, orientation)).toEqual({ width: 3000, height: 4000 })
    }
  })

  it('keeps the sides for the flip-only orientations', () => {
    for (const orientation of [2, 3, 4]) {
      expect(displayFrame(4000, 3000, orientation)).toEqual({ width: 4000, height: 3000 })
    }
  })
})

describe('squareCrop', () => {
  const frame = { width: 4000, height: 2000 }

  /** The crop's pixel sides, which are what must come out equal. */
  function pixels(crop: Bbox, f: { width: number; height: number }) {
    return { w: crop[2] * f.width, h: crop[3] * f.height }
  }

  it('squares a normalised-square box in pixel space', () => {
    // 0.2 x 0.2 on a 2:1 frame is 800x400 pixels — an oblong. Squaring must grow
    // the short pixel side to 800, i.e. the full height of a 2000px frame is not
    // needed but 800/2000 = 0.4 normalised is.
    const crop = squareCrop([0.4, 0.4, 0.2, 0.2], frame)
    const px = pixels(crop, frame)
    expect(px.w).toBeCloseTo(px.h, 6)
    expect(px.w).toBeCloseTo(800, 6)
  })

  it('centres the crop on the box it was given', () => {
    const crop = squareCrop([0.4, 0.4, 0.2, 0.2], frame)
    const centerX = (crop[0] + crop[2] / 2) * frame.width
    const centerY = (crop[1] + crop[3] / 2) * frame.height
    expect(centerX).toBeCloseTo(0.5 * frame.width, 6)
    expect(centerY).toBeCloseTo(0.5 * frame.height, 6)
  })

  it('slides a crop at the edge back inside instead of clipping it square', () => {
    const crop = squareCrop([0.9, 0.9, 0.1, 0.1], frame)
    const px = pixels(crop, frame)
    expect(px.w).toBeCloseTo(px.h, 6)
    expect(crop[0] + crop[2]).toBeLessThanOrEqual(1 + 1e-9)
    expect(crop[1] + crop[3]).toBeLessThanOrEqual(1 + 1e-9)
    expect(crop[0]).toBeGreaterThanOrEqual(0)
    expect(crop[1]).toBeGreaterThanOrEqual(0)
  })

  it('shrinks to the frame when the square would not fit', () => {
    // A box taller than the frame is wide cannot be squared at its own size.
    const crop = squareCrop([0, 0, 1, 1], frame)
    const px = pixels(crop, frame)
    expect(px.w).toBeCloseTo(px.h, 6)
    expect(px.w).toBeCloseTo(2000, 6)
  })

  it('stays square on a portrait frame', () => {
    const portrait = { width: 2000, height: 4000 }
    const crop = squareCrop([0.3, 0.1, 0.3, 0.1], portrait)
    const px = pixels(crop, portrait)
    expect(px.w).toBeCloseTo(px.h, 6)
  })

  it('falls back to the whole frame for a degenerate frame', () => {
    expect(squareCrop([0.1, 0.1, 0.2, 0.2], { width: 0, height: 0 })).toEqual([0, 0, 1, 1])
  })

  it('falls back to the whole frame for a zero-area box', () => {
    expect(squareCrop([0.1, 0.1, 0, 0], frame)).toEqual([0, 0, 1, 1])
  })

  it('squares a padded box, the way the tile uses it', () => {
    const crop = squareCrop(padBbox([0.4, 0.4, 0.1, 0.2], 0.3), frame)
    const px = pixels(crop, frame)
    expect(px.w).toBeCloseTo(px.h, 6)
  })
})
