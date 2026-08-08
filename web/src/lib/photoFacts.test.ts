import { describe, expect, it } from 'vitest'

import {
  addKeywords,
  aspectRatio,
  formatMime,
  joinKeywords,
  megapixels,
  metaValue,
  orientation,
  sameKeywords,
  shortHash,
  splitAiNote,
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

describe('joinKeywords', () => {
  it('round-trips through splitKeywords', () => {
    expect(joinKeywords(['beach', 'sunset'])).toBe('beach, sunset')
    expect(splitKeywords(joinKeywords(['beach', 'sunset']))).toEqual(['beach', 'sunset'])
  })

  it('is empty for no keywords', () => {
    expect(joinKeywords([])).toBe('')
  })
})

describe('addKeywords', () => {
  it('trims the added keyword', () => {
    expect(addKeywords(['beach'], '  sunset ', 2000)).toEqual(['beach', 'sunset'])
  })

  it('splits a comma-separated value into several keywords', () => {
    expect(addKeywords([], 'beach, sunset,', 2000)).toEqual(['beach', 'sunset'])
  })

  it('ignores blanks and keywords the photo already carries', () => {
    expect(addKeywords(['beach'], 'beach', 2000)).toEqual(['beach'])
    expect(addKeywords(['beach'], '   ', 2000)).toEqual(['beach'])
  })

  it('refuses a keyword that would push the joined string past the cap', () => {
    // "beach, sunset" is 13 runes, so a cap of 12 leaves room for the first only.
    expect(addKeywords([], 'beach, sunset', 12)).toEqual(['beach'])
    expect(addKeywords([], 'beach, sunset', 13)).toEqual(['beach', 'sunset'])
  })

  it('counts the cap in runes, not UTF-16 units, as the backend does', () => {
    // "🏖️" is 2 runes but 3 UTF-16 units, so a naive .length would refuse it here.
    expect(addKeywords([], '🏖️', 2)).toEqual(['🏖️'])
    expect(addKeywords([], '🏖️', 1)).toEqual([])
    expect(addKeywords([], 'žluťoučký', 9)).toEqual(['žluťoučký'])
  })
})

describe('sameKeywords', () => {
  it('is true only for the same keywords in the same order', () => {
    expect(sameKeywords(['beach', 'sunset'], ['beach', 'sunset'])).toBe(true)
    expect(sameKeywords(['beach', 'sunset'], ['sunset', 'beach'])).toBe(false)
    expect(sameKeywords(['beach'], ['beach', 'sunset'])).toBe(false)
    expect(sameKeywords([], [])).toBe(true)
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

describe('metaValue', () => {
  it('keeps a real value, trimmed', () => {
    expect(metaValue('Canon EOS 5D')).toBe('Canon EOS 5D')
    expect(metaValue('  NIKON D700  ')).toBe('NIKON D700')
  })

  it("treats the importers' `Unknown` as nothing at all", () => {
    // PhotoPrism stores the literal word for a scan that never had a camera,
    // so the row is not empty — it is an English word in a Czech table.
    expect(metaValue('Unknown')).toBeUndefined()
    expect(metaValue('unknown')).toBeUndefined()
    expect(metaValue(' UNKNOWN ')).toBeUndefined()
  })

  it('treats an empty, blank or absent value as nothing', () => {
    expect(metaValue('')).toBeUndefined()
    expect(metaValue('   ')).toBeUndefined()
    expect(metaValue(undefined)).toBeUndefined()
  })

  it('does not swallow a value that merely mentions the word', () => {
    expect(metaValue('Unknown Camera Co.')).toBe('Unknown Camera Co.')
  })
})

describe('splitAiNote', () => {
  it('takes the model trailer off the description', () => {
    // The shape 2500 photos carry after the photo-sorter import.
    expect(splitAiNote('Skupina mužů v krojích na návsi.\n\nAI_MODEL: gemini-2.5-flash')).toEqual({
      text: 'Skupina mužů v krojích na návsi.',
      model: 'gemini-2.5-flash',
    })
  })

  it('takes it off when it follows the text directly, without a blank line', () => {
    expect(splitAiNote('Pes na louce.\nAI_MODEL: gpt-4o')).toEqual({
      text: 'Pes na louce.',
      model: 'gpt-4o',
    })
  })

  it('keeps the line breaks inside the description itself', () => {
    expect(splitAiNote('První řádek.\nDruhý řádek.\n\nAI_MODEL: x').text).toBe(
      'První řádek.\nDruhý řádek.',
    )
  })

  it('leaves a note without the trailer exactly as it is', () => {
    expect(splitAiNote('Skupina mužů v krojích.')).toEqual({
      text: 'Skupina mužů v krojích.',
      model: '',
    })
  })

  it('only takes a trailing marker, never one in the middle of a sentence', () => {
    const note = 'Popisek zmiňuje AI_MODEL: něco a pokračuje dál.'
    expect(splitAiNote(note)).toEqual({ text: note, model: '' })
  })

  it('reads a note that is nothing but the trailer as having no description', () => {
    expect(splitAiNote('AI_MODEL: gemini-2.5-flash')).toEqual({
      text: '',
      model: 'gemini-2.5-flash',
    })
  })

  it('survives an absent or empty note', () => {
    expect(splitAiNote(undefined)).toEqual({ text: '', model: '' })
    expect(splitAiNote('')).toEqual({ text: '', model: '' })
  })
})
