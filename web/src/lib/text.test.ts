import { describe, expect, it } from 'vitest'

import { foldedEquals, foldedIncludes, foldText } from './text'

describe('foldText', () => {
  it('lower-cases, trims and strips diacritics', () => {
    expect(foldText('  Náměstí ')).toBe('namesti')
    expect(foldText('ŽLUŤOUČKÝ')).toBe('zlutoucky')
    expect(foldText('')).toBe('')
  })
})

describe('foldedIncludes', () => {
  it('matches case- and accent-insensitively', () => {
    expect(foldedIncludes('Náměstí Míru', 'namesti')).toBe(true)
    expect(foldedIncludes('Holidays', 'HOLI')).toBe(true)
    expect(foldedIncludes('Work', 'sun')).toBe(false)
  })

  it('treats a blank needle as matching everything', () => {
    expect(foldedIncludes('anything', '')).toBe(true)
    expect(foldedIncludes('anything', '   ')).toBe(true)
  })
})

describe('foldedEquals', () => {
  it('equates names differing only in case, accents or padding', () => {
    expect(foldedEquals('Dovolená', ' dovolena ')).toBe(true)
    expect(foldedEquals('sunset', 'SUNSET')).toBe(true)
  })

  it('keeps distinct names apart', () => {
    expect(foldedEquals('sunset', 'sun')).toBe(false)
    expect(foldedEquals('Dovolená', '')).toBe(false)
  })
})
