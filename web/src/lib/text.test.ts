import { describe, expect, it } from 'vitest'

import { foldedIncludes, foldText } from './text'

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
