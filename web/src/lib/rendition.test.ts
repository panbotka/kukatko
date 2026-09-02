import { describe, expect, it } from 'vitest'

import {
  fitRenditionName,
  MAX_RENDITION_DPR,
  paintedLongestSide,
  pickRendition,
  renditionDpr,
  squareRenditionName,
  stageRenditionName,
} from './rendition'

describe('renditionDpr', () => {
  it('caps the ratio', () => {
    expect(renditionDpr(3)).toBe(MAX_RENDITION_DPR)
    expect(renditionDpr(1.5)).toBe(1.5)
  })

  it('falls back to 1 for an unusable ratio', () => {
    expect(renditionDpr(undefined)).toBe(1)
    expect(renditionDpr(0)).toBe(1)
    expect(renditionDpr(Number.NaN)).toBe(1)
    expect(renditionDpr(-2)).toBe(1)
  })
})

describe('pickRendition', () => {
  it('takes the smallest rung that covers the need', () => {
    expect(pickRendition([100, 224, 500], 90)).toBe(100)
    expect(pickRendition([100, 224, 500], 200)).toBe(224)
    expect(pickRendition([100, 224, 500], 300)).toBe(500)
  })

  it('tolerates a few per cent of upscale rather than stepping a rung', () => {
    expect(pickRendition([100, 224, 500], 110)).toBe(100)
  })

  it('never goes past the largest rung', () => {
    expect(pickRendition([100, 224, 500], 9000)).toBe(500)
  })

  it('falls back to the smallest rung for an unmeasured box', () => {
    expect(pickRendition([100, 224, 500], 0)).toBe(100)
    expect(pickRendition([100, 224, 500], Number.NaN)).toBe(100)
  })
})

describe('fitRenditionName / squareRenditionName', () => {
  it('names a registered thumbnail size', () => {
    expect(fitRenditionName(300, 2)).toBe('fit_720')
    expect(fitRenditionName(900, 2)).toBe('fit_1920')
    expect(squareRenditionName(32, 2)).toBe('tile_100')
    expect(squareRenditionName(180, 2)).toBe('tile_500')
  })

  it('sizes a retina screen up and a plain one down', () => {
    expect(squareRenditionName(160, 1)).toBe('tile_224')
    expect(squareRenditionName(160, 2)).toBe('tile_500')
  })
})

describe('paintedLongestSide', () => {
  it('measures the photograph as the box actually paints it', () => {
    // A 4:3 landscape in a portrait phone is width-bound: it paints 390 across.
    expect(paintedLongestSide({ width: 390, height: 844 }, { width: 4000, height: 3000 })).toBe(390)
    // A 3:4 portrait in the same phone is still width-bound, but taller.
    expect(paintedLongestSide({ width: 390, height: 844 }, { width: 3000, height: 4000 })).toBe(520)
  })

  it('is height-bound when the box is wider than the photograph', () => {
    expect(paintedLongestSide({ width: 1920, height: 1080 }, { width: 4000, height: 3000 })).toBe(
      1440,
    )
  })

  it('falls back to the box for unknown or nonsense proportions', () => {
    expect(paintedLongestSide({ width: 390, height: 844 }, null)).toBe(844)
    expect(paintedLongestSide({ width: 390, height: 844 }, { width: 0, height: 0 })).toBe(844)
  })
})

describe('stageRenditionName', () => {
  it('drops a phone stage to the smaller rung', () => {
    const phone = { width: 390, height: 844 }
    expect(stageRenditionName(phone, { width: 4000, height: 3000 }, 2)).toBe('fit_1280')
    expect(stageRenditionName(phone, { width: 3000, height: 4000 }, 2)).toBe('fit_1280')
  })

  it('keeps a desktop stage on the rendition it always had', () => {
    const desktop = { width: 1920, height: 1080 }
    expect(stageRenditionName(desktop, { width: 4000, height: 3000 }, 1)).toBe('fit_1920')
    expect(stageRenditionName(desktop, { width: 4000, height: 3000 }, 2)).toBe('fit_1920')
  })

  it('never asks for more than the fixed size the stages used to fetch', () => {
    expect(
      stageRenditionName({ width: 5120, height: 2880 }, { width: 8000, height: 6000 }, 2),
    ).toBe('fit_1920')
  })

  it('is conservative when the photograph is not known yet', () => {
    expect(stageRenditionName({ width: 390, height: 844 }, null, 2)).toBe('fit_1920')
  })
})
