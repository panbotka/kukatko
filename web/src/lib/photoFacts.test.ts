import { describe, expect, it } from 'vitest'

import {
  aspectRatio,
  formatMime,
  megapixels,
  orientation,
  shortHash,
  splitKeywords,
  takenAtSource,
} from './photoFacts'

describe('aspectRatio', () => {
  it('reduces a classic sensor ratio', () => {
    expect(aspectRatio(4000, 3000, 'cs')).toBe('4 : 3')
    expect(aspectRatio(6000, 4000, 'cs')).toBe('3 : 2')
  })

  it('reduces a widescreen video ratio', () => {
    expect(aspectRatio(1920, 1080, 'cs')).toBe('16 : 9')
    expect(aspectRatio(3840, 2160, 'en')).toBe('16 : 9')
  })

  it('reduces a portrait ratio without flipping it', () => {
    expect(aspectRatio(1080, 1920, 'cs')).toBe('9 : 16')
  })

  it('falls back to a decimal when the sides do not reduce to readable terms', () => {
    // 1001:667 is really "about 3:2", but as a fraction it says nothing.
    expect(aspectRatio(1001, 667, 'cs')).toBe('1,50 : 1')
    expect(aspectRatio(1001, 667, 'en')).toBe('1.50 : 1')
  })

  it('is undefined for a photo whose dimensions are unknown', () => {
    expect(aspectRatio(0, 0, 'cs')).toBeUndefined()
    expect(aspectRatio(4000, 0, 'cs')).toBeUndefined()
  })
})

describe('megapixels', () => {
  it('computes the resolution to one decimal in the active locale', () => {
    expect(megapixels(4000, 3056, 'cs')).toBe('12,2')
    expect(megapixels(4000, 3056, 'en')).toBe('12.2')
    expect(megapixels(6000, 4000, 'cs')).toBe('24,0')
  })

  it('is undefined for a photo whose dimensions are unknown', () => {
    expect(megapixels(0, 3000, 'cs')).toBeUndefined()
  })
})

describe('formatMime', () => {
  it('maps the stored types to their short format label', () => {
    expect(formatMime('image/jpeg')).toBe('JPEG')
    expect(formatMime('image/heic')).toBe('HEIC')
    expect(formatMime('video/quicktime')).toBe('MOV')
  })

  it('degrades an unknown type to its subtype', () => {
    expect(formatMime('image/jxl')).toBe('JXL')
    expect(formatMime('image/svg+xml')).toBe('SVG')
    expect(formatMime('image/x-fuji-raf')).toBe('FUJI-RAF')
  })

  it('is empty for an empty type', () => {
    expect(formatMime('')).toBe('')
  })
})

describe('orientation', () => {
  it('narrows the EXIF values 1–8', () => {
    expect(orientation(1)).toBe(1)
    expect(orientation(8)).toBe(8)
  })

  it('rejects a missing or corrupt tag', () => {
    expect(orientation(0)).toBeUndefined()
    expect(orientation(9)).toBeUndefined()
    expect(orientation(undefined)).toBeUndefined()
  })
})

describe('takenAtSource', () => {
  it('narrows the known sources', () => {
    expect(takenAtSource('exif')).toBe('exif')
    expect(takenAtSource('filename')).toBe('filename')
    expect(takenAtSource('manual')).toBe('manual')
  })

  it('reads an unrecognised source as unknown', () => {
    expect(takenAtSource('sidecar-of-the-future')).toBe('unknown')
  })

  it('is undefined when no source is recorded', () => {
    expect(takenAtSource('')).toBeUndefined()
    expect(takenAtSource(undefined)).toBeUndefined()
  })
})

describe('splitKeywords', () => {
  it('splits the comma-separated IPTC string and drops the blanks', () => {
    expect(splitKeywords('beach, , sunset ')).toEqual(['beach', 'sunset'])
  })

  it('is empty when there are no keywords', () => {
    expect(splitKeywords('')).toEqual([])
    expect(splitKeywords(undefined)).toEqual([])
  })
})

describe('shortHash', () => {
  it('truncates a SHA256 to its leading characters', () => {
    expect(shortHash('a'.repeat(64))).toBe(`${'a'.repeat(12)}…`)
  })

  it('leaves a short value alone', () => {
    expect(shortHash('abc')).toBe('abc')
  })
})
