import { describe, expect, it } from 'vitest'

import { displayFrame, padBbox, readingOrder, squareCrop } from './faceGeometry'

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

  // The production regression: a 5472 × 3648 original with orientation 8 is
  // displayed portrait. Feeding the STORED pair (the invariant the function
  // documents) yields the frame the viewer sizes its figure from; feeding
  // PhotoPrism's already-rotated pair instead yields the transpose, which is what
  // letterboxed the photo and drifted every face box off the faces.
  describe('the four orientations the library actually holds', () => {
    const stored = { width: 5472, height: 3648 }
    const cases: { orientation: number; width: number; height: number }[] = [
      { orientation: 1, width: 5472, height: 3648 },
      { orientation: 3, width: 5472, height: 3648 },
      { orientation: 6, width: 3648, height: 5472 },
      { orientation: 8, width: 3648, height: 5472 },
    ]

    it.each(cases)('resolves orientation $orientation from the stored pair', (tc) => {
      expect(displayFrame(stored.width, stored.height, tc.orientation)).toEqual({
        width: tc.width,
        height: tc.height,
      })
    })

    it('is its own inverse, so a double-rotated pair is exactly the transpose', () => {
      for (const { orientation, width, height } of cases) {
        expect(displayFrame(width, height, orientation)).toEqual(stored)
      }
    })
  })

  it('leaves a degenerate frame alone rather than inventing one', () => {
    expect(displayFrame(0, 0, 8)).toEqual({ width: 0, height: 0 })
    expect(displayFrame(-1, 100, 1)).toEqual({ width: -1, height: 100 })
  })
})

describe('readingOrder', () => {
  /** A named face box, so an assertion reads as the order of the people. */
  function face(name: string, bbox: Bbox) {
    return { name, bbox }
  }

  /** The order the function put them in, by name. */
  function names(faces: { name: string; bbox: Bbox }[]): string[] {
    return readingOrder(faces).map((f) => f.name)
  }

  it('orders one row of faces left to right', () => {
    // Handed to it in the arbitrary order a detector emits.
    expect(
      names([
        face('c', [0.7, 0.3, 0.1, 0.15]),
        face('a', [0.1, 0.3, 0.1, 0.15]),
        face('b', [0.4, 0.31, 0.1, 0.15]),
      ]),
    ).toEqual(['a', 'b', 'c'])
  })

  it('takes the top row before the one below it', () => {
    expect(
      names([
        face('back-right', [0.6, 0.1, 0.1, 0.12]),
        face('front-left', [0.1, 0.5, 0.14, 0.18]),
        face('back-left', [0.2, 0.1, 0.1, 0.12]),
        face('front-right', [0.6, 0.52, 0.14, 0.18]),
      ]),
    ).toEqual(['back-left', 'back-right', 'front-left', 'front-right'])
  })

  it('keeps a row together when the heads are not perfectly level', () => {
    // A tall person beside a short one: the centres differ, but the boxes still
    // overlap, so this is one row and must not be split into two.
    expect(
      names([
        face('right', [0.6, 0.3, 0.12, 0.16]),
        face('tall', [0.35, 0.24, 0.12, 0.16]),
        face('left', [0.1, 0.31, 0.12, 0.16]),
      ]),
    ).toEqual(['left', 'tall', 'right'])
  })

  it('does not chain a slow drift down a crowd into one endless row', () => {
    // Each face is only a little lower than the one before it, but the third is a
    // long way below the first. A band anchored on the face last added would take
    // all three and answer `c, a, b`; anchoring on the band's topmost member
    // breaks the chain and leaves `c` where it belongs — on the row below.
    expect(
      names([
        face('a', [0.45, 0.1, 0.1, 0.1]),
        face('b', [0.85, 0.14, 0.1, 0.1]),
        face('c', [0.05, 0.18, 0.1, 0.1]),
      ]),
    ).toEqual(['a', 'b', 'c'])
  })

  it('is stable for faces sharing a position', () => {
    const same: Bbox = [0.1, 0.2, 0.3, 0.4]
    expect(names([face('first', same), face('second', same), face('third', same)])).toEqual([
      'first',
      'second',
      'third',
    ])
  })

  it('leaves the input array alone', () => {
    const faces = [face('b', [0.5, 0.1, 0.1, 0.1]), face('a', [0.1, 0.1, 0.1, 0.1])]
    readingOrder(faces)
    expect(faces.map((f) => f.name)).toEqual(['b', 'a'])
  })

  it('handles the empty and single-face photos', () => {
    expect(readingOrder([])).toEqual([])
    expect(names([face('only', [0.4, 0.4, 0.2, 0.2])])).toEqual(['only'])
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
