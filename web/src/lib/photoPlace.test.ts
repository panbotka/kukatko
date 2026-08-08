import { describe, expect, it } from 'vitest'

import { type PlaceNames, placeLabel, placeName } from './photoPlace'

/** A fully resolved place, the shape the `places` job caches on a photo. */
function place(overrides: Partial<PlaceNames> = {}): PlaceNames {
  return {
    country: 'Česko',
    region: 'Jihomoravský kraj',
    city: 'Brno',
    place_name: 'Špilberk',
    ...overrides,
  }
}

describe('placeName', () => {
  it('picks the narrowest resolved level', () => {
    expect(placeName(place())).toBe('Špilberk')
    expect(placeName(place({ place_name: '' }))).toBe('Brno')
    expect(placeName(place({ place_name: '', city: '' }))).toBe('Česko')
  })

  it('is empty when the photo has no place or nothing was resolved', () => {
    expect(placeName(undefined)).toBe('')
    expect(placeName(place({ place_name: '  ', city: '', country: '' }))).toBe('')
  })
})

describe('placeLabel', () => {
  it('reads narrowest to widest', () => {
    expect(placeLabel(place())).toBe('Špilberk, Brno, Česko')
  })

  it('skips the levels the geocoder left blank', () => {
    expect(placeLabel(place({ place_name: '' }))).toBe('Brno, Česko')
    expect(placeLabel(place({ city: '' }))).toBe('Špilberk, Česko')
    expect(placeLabel(place({ place_name: '', city: '' }))).toBe('Česko')
  })

  it('says a village once, not twice', () => {
    // A hamlet is its own named place, and "Veselice, Veselice, Česko" is the
    // common case rather than the exception.
    expect(placeLabel(place({ place_name: 'Veselice', city: 'Veselice' }))).toBe('Veselice, Česko')
  })

  it('leaves the region out — it is an address, not a caption', () => {
    expect(placeLabel(place())).not.toContain('Jihomoravský kraj')
  })

  it('is empty when there is nothing to name', () => {
    expect(placeLabel(undefined)).toBe('')
    expect(placeLabel(place({ place_name: '', city: '', country: '' }))).toBe('')
  })
})
