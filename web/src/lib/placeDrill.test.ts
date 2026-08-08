import { describe, expect, it } from 'vitest'

import { resolvePlaceDrill } from './placeDrill'
import { type PlaceCountry } from '../services/places'

/** Builds a country entry; `count` defaults to the sum of its cities. */
function country(name: string, cities: [string, number][], count?: number): PlaceCountry {
  const list = cities.map(([city, n]) => ({ city, count: n }))
  return {
    country: name,
    count: count ?? list.reduce((sum, c) => sum + c.count, 0),
    cities: list,
  }
}

describe('resolvePlaceDrill', () => {
  it('shows the country list when there is more than one country', () => {
    const drill = resolvePlaceDrill(
      [country('Česko', [['Brno', 2]]), country('Rakousko', [])],
      '',
      '',
    )
    expect(drill.country).toBe('')
    expect(drill.countryImplied).toBe(false)
  })

  it('steps past a single country', () => {
    const drill = resolvePlaceDrill(
      [
        country('Česko', [
          ['Brno', 2],
          ['Praha', 3],
        ]),
      ],
      '',
      '',
    )
    expect(drill.country).toBe('Česko')
    expect(drill.countryImplied).toBe(true)
    expect(drill.city).toBe('')
    // Nothing to go back to: the country list held this one row.
    expect(drill.canClearCountry).toBe(false)
  })

  it('steps past a single city that holds the whole country', () => {
    const drill = resolvePlaceDrill([country('Česko', [['Brno', 5]])], '', '')
    expect(drill.country).toBe('Česko')
    expect(drill.city).toBe('Brno')
    expect(drill.cityImplied).toBe(true)
    expect(drill.canClearCity).toBe(false)
  })

  it('keeps a single city that leaves the country`s other photos behind', () => {
    // 5 of 40 photos are in the one named town; the rest were never resolved to
    // one, so skipping would silently drop them.
    const drill = resolvePlaceDrill([country('Česko', [['Brno', 5]], 40)], '', '')
    expect(drill.country).toBe('Česko')
    expect(drill.city).toBe('')
  })

  it('honours an explicit choice over the implied one', () => {
    const countries = [
      country('Česko', [
        ['Brno', 2],
        ['Praha', 3],
      ]),
      country('Rakousko', []),
    ]
    const drill = resolvePlaceDrill(countries, 'Rakousko', '')
    expect(drill.country).toBe('Rakousko')
    expect(drill.countryImplied).toBe(false)
    expect(drill.canClearCountry).toBe(true)
    expect(drill.selected?.country).toBe('Rakousko')
  })

  it('reports a chosen city as clearable only when the list below is worth seeing', () => {
    const many = [
      country('Česko', [
        ['Brno', 2],
        ['Praha', 3],
      ]),
    ]
    expect(resolvePlaceDrill(many, 'Česko', 'Brno').canClearCity).toBe(true)
    const one = [country('Česko', [['Brno', 5]])]
    expect(resolvePlaceDrill(one, 'Česko', 'Brno').canClearCity).toBe(false)
  })

  it('leaves an unknown country selected with nothing under it', () => {
    const drill = resolvePlaceDrill([country('Česko', [['Brno', 2]])], 'Narnie', '')
    expect(drill.country).toBe('Narnie')
    expect(drill.selected).toBeUndefined()
    expect(drill.city).toBe('')
  })

  it('shows the country list while the hierarchy is empty', () => {
    const drill = resolvePlaceDrill([], '', '')
    expect(drill.country).toBe('')
    expect(drill.selected).toBeUndefined()
  })
})
