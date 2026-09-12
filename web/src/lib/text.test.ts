import { describe, expect, it } from 'vitest'

import { foldedEquals, foldedIncludes, foldText, truncateText } from './text'

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

describe('truncateText', () => {
  it('leaves a string that already fits alone', () => {
    expect(truncateText('Marie Nečasová', 20)).toBe('Marie Nečasová')
    expect(truncateText('Marie', 5)).toBe('Marie')
  })

  it('cuts to the limit and marks the cut', () => {
    expect(truncateText('Bohumil Nečas st.st.', 12)).toBe('Bohumil Neč…')
  })

  it('does not leave a space hanging before the ellipsis', () => {
    expect(truncateText('Marie Nečasová', 7)).toBe('Marie…')
  })

  it('counts letters, not code units', () => {
    // Written with a combining caron: six code units, five letters to a reader.
    const decomposed = 'Z\u030Eofie'
    expect(decomposed).toHaveLength(6)
    expect(truncateText(decomposed, 5)).toBe(decomposed)
  })

  it('has nothing to say in no space at all', () => {
    expect(truncateText('Marie', 0)).toBe('')
  })
})
