import { describe, expect, it } from 'vitest'

import { type PhotoEdit } from '../services/photos'

import {
  cropClipPath,
  editFilter,
  editPreviewStyle,
  editTransform,
  hasCrop,
  isIdentityEdit,
  NEUTRAL_EDIT,
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
