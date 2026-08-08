import { describe, expect, it } from 'vitest'

import { countryDisplayName, localizeCountryNames } from './countryNames'

describe('countryDisplayName', () => {
  it('translates a known country', () => {
    expect(countryDisplayName('Czech Republic', 'cs')).toBe('Česko')
    expect(countryDisplayName('Czechia', 'cs')).toBe('Česko')
    expect(countryDisplayName('United Kingdom', 'cs')).toBe('Spojené království')
  })

  it('ignores case and surrounding space, as stored data does', () => {
    expect(countryDisplayName('  czech republic ', 'cs')).toBe('Česko')
  })

  it('has nothing to say about an unknown name', () => {
    expect(countryDisplayName('Narnia', 'cs')).toBeUndefined()
  })

  it('leaves the English UI alone — the names are already in its language', () => {
    expect(countryDisplayName('Czech Republic', 'en')).toBeUndefined()
  })

  it('serves a region subtag too', () => {
    expect(countryDisplayName('Czech Republic', 'cs-CZ')).toBe('Česko')
  })
})

describe('localizeCountryNames', () => {
  it('translates a country standing as its own segment of a composed title', () => {
    // The shape the photo-sorter import composed: name / country / year.
    expect(localizeCountryNames('Jan Novák / Czech Republic / 2026', 'cs')).toBe(
      'Jan Novák / Česko / 2026',
    )
  })

  it('keeps a trailing year with the country, as the place albums are named', () => {
    expect(localizeCountryNames('Czech Republic 2026', 'cs')).toBe('Česko 2026')
    expect(localizeCountryNames('New Zealand 1998', 'cs')).toBe('Nový Zéland 1998')
  })

  it('translates a whole title that is nothing but the country', () => {
    expect(localizeCountryNames('Germany', 'cs')).toBe('Německo')
  })

  it('handles a comma-composed name', () => {
    expect(localizeCountryNames('Brno, Czech Republic', 'cs')).toBe('Brno, Česko')
  })

  it('never half-translates a name somebody wrote themselves', () => {
    // The whole point of matching segments rather than substrings.
    expect(localizeCountryNames('New Zealand trip', 'cs')).toBe('New Zealand trip')
    expect(localizeCountryNames('Cesta do Chile a zpět', 'cs')).toBe('Cesta do Chile a zpět')
    expect(localizeCountryNames('Indian summer', 'cs')).toBe('Indian summer')
  })

  it('leaves an unknown country exactly as it is stored', () => {
    expect(localizeCountryNames('Jan / Narnia / 2026', 'cs')).toBe('Jan / Narnia / 2026')
  })

  it('keeps the spacing of the segments it does translate', () => {
    expect(localizeCountryNames('Jan/Czech Republic/2026', 'cs')).toBe('Jan/Česko/2026')
  })

  it('leaves the English UI alone', () => {
    expect(localizeCountryNames('Jan / Czech Republic / 2026', 'en')).toBe(
      'Jan / Czech Republic / 2026',
    )
  })

  it('passes an empty or blank string through', () => {
    expect(localizeCountryNames('', 'cs')).toBe('')
    expect(localizeCountryNames('   ', 'cs')).toBe('   ')
  })
})
