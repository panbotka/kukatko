import { describe, expect, it } from 'vitest'

import { GRID_PREVIEW_SIZE } from '../services/photos'

import {
  TILE_MAX_DPR,
  tileRenditionName,
  tileRenditionSize,
  tileUsesPreviewURL,
} from './tileRendition'

describe('tileRenditionSize', () => {
  it('leaves an ordinary tile on the rendition the payload carries', () => {
    expect(tileRenditionName(300, 1)).toBe(GRID_PREVIEW_SIZE)
    expect(tileRenditionName(380, 2)).toBe(GRID_PREVIEW_SIZE)
    expect(tileUsesPreviewURL(300, 2)).toBe(true)
  })

  it('steps up for a tile wide enough to outrun it', () => {
    expect(tileRenditionSize(1000, 1)).toBe(1280)
    expect(tileRenditionSize(1000, 2)).toBe(1920)
    expect(tileRenditionSize(1400, 2)).toBe(2560)
  })

  it('caps the device pixel ratio', () => {
    expect(tileRenditionSize(500, 3)).toBe(tileRenditionSize(500, TILE_MAX_DPR))
  })

  it('never goes past the largest rung', () => {
    expect(tileRenditionSize(10000, 2)).toBe(2560)
  })

  it('falls back to the default rung for an unmeasured tile', () => {
    expect(tileRenditionSize(0)).toBe(720)
    expect(tileRenditionSize(Number.NaN, 2)).toBe(720)
    expect(tileRenditionSize(-10)).toBe(720)
  })
})
