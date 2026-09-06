import { describe, expect, it } from 'vitest'

import { type PhotoEdit } from '../services/photos'

import {
  cropClipPath,
  editDelta,
  editedFrame,
  editFilter,
  editPreviewStyle,
  editTransform,
  hasCrop,
  isIdentityEdit,
  isQuarterTurn,
  NEUTRAL_EDIT,
  normalizeRotation,
  rotateBy,
  rotateLeft,
  rotateRight,
} from './photoEdit'

const crop: PhotoEdit = {
  rotation: 0,
  brightness: 0,
  contrast: 0,
  crop_x: 0.1,
  crop_y: 0.2,
  crop_w: 0.5,
  crop_h: 0.6,
}

describe('photoEdit helpers', () => {
  it('detects a complete crop rectangle', () => {
    expect(hasCrop(crop)).toBe(true)
    expect(hasCrop(NEUTRAL_EDIT)).toBe(false)
    // A partial crop is not a crop.
    expect(hasCrop({ rotation: 0, brightness: 0, contrast: 0, crop_x: 0.1 })).toBe(false)
  })

  it('recognises the identity (no-op) edit', () => {
    expect(isIdentityEdit(NEUTRAL_EDIT)).toBe(true)
    expect(isIdentityEdit({ ...NEUTRAL_EDIT, rotation: 90 })).toBe(false)
    expect(isIdentityEdit({ ...NEUTRAL_EDIT, brightness: 0.2 })).toBe(false)
    expect(isIdentityEdit(crop)).toBe(false)
  })

  it('rotates clockwise by quarter turns and wraps', () => {
    expect(rotateRight(0)).toBe(90)
    expect(rotateRight(90)).toBe(180)
    expect(rotateRight(270)).toBe(0)
  })

  it('rotates counter-clockwise by quarter turns and never goes negative', () => {
    expect(rotateLeft(90)).toBe(0)
    expect(rotateLeft(180)).toBe(90)
    // The wrap the backend's allow-list depends on: -90 would be rejected.
    expect(rotateLeft(0)).toBe(270)
  })

  it('turns by any number of quarters in either direction', () => {
    expect(rotateBy(0, 2)).toBe(180)
    expect(rotateBy(0, -2)).toBe(180)
    expect(rotateBy(90, 4)).toBe(90)
    expect(rotateBy(90, -5)).toBe(0)
  })

  it('builds a brightness/contrast filter matching the backend rendering', () => {
    expect(editFilter(NEUTRAL_EDIT)).toBe('none')
    expect(editFilter({ ...NEUTRAL_EDIT, brightness: 0.5 })).toBe('brightness(1.5)')
    expect(editFilter({ ...NEUTRAL_EDIT, brightness: -0.5, contrast: 0.25 })).toBe(
      'brightness(0.5) contrast(1.25)',
    )
  })

  it('builds a rotation transform only when rotated', () => {
    expect(editTransform(NEUTRAL_EDIT)).toBe('none')
    expect(editTransform({ ...NEUTRAL_EDIT, rotation: 90 })).toBe('rotate(90deg)')
  })

  it('builds an inset clip-path for a crop and nothing without one', () => {
    expect(cropClipPath(NEUTRAL_EDIT)).toBeUndefined()
    const clip = cropClipPath(crop)
    // inset(top right bottom left) — top=crop_y, left=crop_x.
    expect(clip).toContain('inset(')
    expect(clip).toContain('20.0000%') // top = crop_y
    expect(clip).toContain('10.0000%') // left = crop_x
  })

  it('combines a non-neutral edit into a minimal CSS style', () => {
    expect(editPreviewStyle(NEUTRAL_EDIT)).toEqual({})
    const style = editPreviewStyle({ ...crop, rotation: 90, brightness: 0.5 })
    expect(style.transform).toBe('rotate(90deg)')
    expect(style.filter).toBe('brightness(1.5)')
    expect(style.clipPath).toContain('inset(')
  })
})

describe('editDelta', () => {
  const saved: PhotoEdit = { rotation: 90, brightness: 0.5, contrast: 0 }

  it('is neutral when the rendition already shows the edit', () => {
    // The whole point: a saved rotation is baked into the rendition, so nothing
    // is left for CSS to do — styling it again is the sideways photo.
    expect(editDelta(saved, saved)).toEqual(NEUTRAL_EDIT)
    expect(isIdentityEdit(editDelta(crop, crop))).toBe(true)
  })

  it('is the whole edit over a rendition of the neutral edit', () => {
    expect(editDelta(NEUTRAL_EDIT, saved)).toEqual(saved)
    expect(editDelta(NEUTRAL_EDIT, crop)).toEqual(crop)
  })

  it('turns only by the difference, wrapping in either direction', () => {
    expect(editDelta(saved, { ...saved, rotation: 180 }).rotation).toBe(90)
    expect(editDelta(saved, { ...saved, rotation: 0 }).rotation).toBe(270)
    expect(editDelta({ ...saved, rotation: 270 }, { ...saved, rotation: 0 }).rotation).toBe(90)
  })

  it('takes the ratio of the multipliers for brightness and contrast', () => {
    // brightness(1.5) is already in the rendition; showing 2.0 needs 2 / 1.5.
    expect(editDelta(saved, { ...saved, brightness: 1 }).brightness).toBeCloseTo(1 / 3, 6)
    // Back to neutral over a brightened rendition: 1 / 1.5.
    expect(editDelta(saved, { ...saved, brightness: 0 }).brightness).toBeCloseTo(-1 / 3, 6)
    expect(editDelta({ ...NEUTRAL_EDIT, contrast: 0.25 }, NEUTRAL_EDIT).contrast).toBeCloseTo(
      -0.2,
      6,
    )
    // A pitch-black rendition has no multiplier to divide by; the draft's own
    // value is the only honest preview left.
    expect(
      editDelta({ ...NEUTRAL_EDIT, brightness: -1 }, { ...NEUTRAL_EDIT, brightness: 0.5 }),
    ).toEqual({ ...NEUTRAL_EDIT, brightness: 0.5 })
  })

  it('expresses a tighter draft crop inside the rendered one', () => {
    // Rendered crop [0.1, 0.2, 0.5, 0.6]; the draft keeps the right half of it:
    // x from 0.35 (= 0.1 + 0.25) → (0.35 - 0.1) / 0.5 = 0.5 of the rendered width.
    const tighter: PhotoEdit = { ...crop, crop_x: 0.35, crop_w: 0.25 }
    const delta = editDelta(crop, tighter)
    expect(delta.crop_x).toBeCloseTo(0.5, 6)
    expect(delta.crop_y).toBeCloseTo(0, 6)
    expect(delta.crop_w).toBeCloseTo(0.5, 6)
    expect(delta.crop_h).toBeCloseTo(1, 6)
  })

  it('turns the crop rectangle into a rotated rendition', () => {
    // The server crops, then rotates: the draft's rectangle (top-left quarter of
    // the upright picture) sits top-RIGHT of a rendition turned 90° clockwise.
    const rendered: PhotoEdit = { ...NEUTRAL_EDIT, rotation: 90 }
    const shown: PhotoEdit = { ...rendered, crop_x: 0, crop_y: 0, crop_w: 0.5, crop_h: 0.5 }
    const delta = editDelta(rendered, shown)
    expect(delta.rotation).toBe(0)
    expect([delta.crop_x, delta.crop_y, delta.crop_w, delta.crop_h]).toEqual([0.5, 0, 0.5, 0.5])
  })

  it('cannot bring back pixels the rendition no longer has', () => {
    // Lifting a saved crop, or widening it, previews as the rendered crop.
    expect(hasCrop(editDelta(crop, NEUTRAL_EDIT))).toBe(false)
    expect(hasCrop(editDelta(crop, { ...crop, crop_x: 0, crop_w: 1 }))).toBe(false)
  })
})

describe('editedFrame', () => {
  const frame = { width: 4000, height: 3000 }

  it('keeps the frame for an edit that moves no pixels', () => {
    expect(editedFrame(frame, NEUTRAL_EDIT)).toEqual(frame)
    expect(editedFrame(frame, { ...NEUTRAL_EDIT, rotation: 180, brightness: 0.5 })).toEqual(frame)
  })

  it('transposes the frame for a quarter turn', () => {
    expect(editedFrame(frame, { ...NEUTRAL_EDIT, rotation: 90 })).toEqual({
      width: 3000,
      height: 4000,
    })
    expect(editedFrame(frame, { ...NEUTRAL_EDIT, rotation: 270 })).toEqual({
      width: 3000,
      height: 4000,
    })
  })

  it('crops before it turns, as the server does', () => {
    expect(editedFrame(frame, crop)).toEqual({ width: 2000, height: 1800 })
    expect(editedFrame(frame, { ...crop, rotation: 90 })).toEqual({ width: 1800, height: 2000 })
  })

  it('leaves an unusable frame alone', () => {
    expect(editedFrame({ width: 0, height: 0 }, { ...NEUTRAL_EDIT, rotation: 90 })).toEqual({
      width: 0,
      height: 0,
    })
  })
})

describe('rotation arithmetic', () => {
  it('normalises any multiple of 90 onto the stored range', () => {
    expect(normalizeRotation(-90)).toBe(270)
    expect(normalizeRotation(450)).toBe(90)
    expect(normalizeRotation(0)).toBe(0)
  })

  it('knows which turns swap width and height', () => {
    expect(isQuarterTurn(90)).toBe(true)
    expect(isQuarterTurn(270)).toBe(true)
    expect(isQuarterTurn(-90)).toBe(true)
    expect(isQuarterTurn(180)).toBe(false)
    expect(isQuarterTurn(0)).toBe(false)
  })
})
