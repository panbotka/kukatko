import { describe, expect, it } from 'vitest'

import { subjectTileImage } from './subjectTile'

import { type SubjectCount, type SubjectFace } from '../services/people'

/** A landscape face box, big enough to be padded without hitting the frame. */
const FACE: SubjectFace = {
  photo_uid: 'photo1',
  x: 0.4,
  y: 0.3,
  w: 0.2,
  h: 0.2,
  width: 4000,
  height: 3000,
  orientation: 1,
}

/** Builds a subject, overriding whichever fields the case is about. */
function subject(over: Partial<SubjectCount> = {}): SubjectCount {
  return {
    uid: 's1',
    slug: 'anna',
    name: 'Anna',
    type: 'person',
    favorite: false,
    private: false,
    notes: '',
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    marker_count: 3,
    ...over,
  }
}

describe('subjectTileImage', () => {
  it('shows an explicitly chosen cover photo whole', () => {
    const got = subjectTileImage(subject({ cover_photo_uid: 'chosen', cover_face: FACE }))
    expect(got).toEqual({ kind: 'cover', photoUid: 'chosen' })
  })

  it('never lets an automatic face override a chosen cover', () => {
    // The face is the better picture, and it still loses: a guess must not
    // overrule the decision somebody made.
    const got = subjectTileImage(subject({ cover_photo_uid: 'chosen', cover_face: FACE }))
    expect(got.kind).toBe('cover')
  })

  it('falls back to a face crop when there is no cover', () => {
    const got = subjectTileImage(subject({ cover_face: FACE }))
    expect(got.kind).toBe('face')
    if (got.kind !== 'face') {
      return
    }
    expect(got.photoUid).toBe('photo1')
    expect(got.frame).toEqual({ width: 4000, height: 3000 })
  })

  it('pads the crop out beyond the bare face box', () => {
    const got = subjectTileImage(subject({ cover_face: FACE }))
    if (got.kind !== 'face') {
      throw new Error('expected a face crop')
    }
    const [, , cropW] = got.crop
    // The face is 0.2 wide; a padded, squared crop must cover more than that, or
    // the tile is a close-up of a nose.
    expect(cropW).toBeGreaterThan(0.2)
  })

  it('produces a crop that is square in pixels, not in normalised units', () => {
    const got = subjectTileImage(subject({ cover_face: FACE }))
    if (got.kind !== 'face') {
      throw new Error('expected a face crop')
    }
    const [, , cropW, cropH] = got.crop
    // Squareness lives in pixel space: on a 4000x3000 frame the normalised width
    // must be the *smaller* number for the crop to come out square.
    expect(cropW * 4000).toBeCloseTo(cropH * 3000, 6)
    expect(cropW).not.toBeCloseTo(cropH, 3)
  })

  it('swaps the frame for a quarter-turned photo', () => {
    // Orientation 6 rotates the image, so the frame the viewer (and the marker)
    // sees is 3000x4000, not 4000x3000.
    const got = subjectTileImage(subject({ cover_face: { ...FACE, orientation: 6 } }))
    if (got.kind !== 'face') {
      throw new Error('expected a face crop')
    }
    expect(got.frame).toEqual({ width: 3000, height: 4000 })
    const [, , cropW, cropH] = got.crop
    expect(cropW * 3000).toBeCloseTo(cropH * 4000, 6)
  })

  it('keeps the crop inside the frame for a face at the edge', () => {
    const got = subjectTileImage(
      subject({ cover_face: { ...FACE, x: 0.95, y: 0.95, w: 0.05, h: 0.05 } }),
    )
    if (got.kind !== 'face') {
      throw new Error('expected a face crop')
    }
    const [x, y, w, h] = got.crop
    expect(x).toBeGreaterThanOrEqual(0)
    expect(y).toBeGreaterThanOrEqual(0)
    expect(x + w).toBeLessThanOrEqual(1 + 1e-9)
    expect(y + h).toBeLessThanOrEqual(1 + 1e-9)
  })

  it('shows nothing when the subject has no cover and no face', () => {
    expect(subjectTileImage(subject())).toEqual({ kind: 'none' })
  })

  it('shows nothing rather than inventing a face from a frameless photo', () => {
    const got = subjectTileImage(subject({ cover_face: { ...FACE, width: 0, height: 0 } }))
    expect(got).toEqual({ kind: 'none' })
  })

  it('treats an empty cover uid as no cover', () => {
    expect(subjectTileImage(subject({ cover_photo_uid: '' }))).toEqual({ kind: 'none' })
  })
})
