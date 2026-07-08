import { describe, expect, it } from 'vitest'

import { type Coordinates, formatCoordinates, parseCoordinates } from './coordinates'

/** Asserts a successful parse close to the expected coordinate (float slack). */
function expectCoords(input: string, expected: Coordinates, tolerance = 1e-4): void {
  const result = parseCoordinates(input)
  expect(result.ok).toBe(true)
  if (result.ok) {
    expect(result.value.lat).toBeCloseTo(expected.lat, 4)
    expect(result.value.lng).toBeCloseTo(expected.lng, 4)
    // The tolerance argument documents intent even though toBeCloseTo drives it.
    void tolerance
  }
}

describe('parseCoordinates — decimal degrees', () => {
  it('parses comma-separated decimals', () => {
    expectCoords('49.1234, 16.5678', { lat: 49.1234, lng: 16.5678 })
  })

  it('parses whitespace-separated decimals', () => {
    expectCoords('49.1234 16.5678', { lat: 49.1234, lng: 16.5678 })
  })

  it('honours explicit +/- signs', () => {
    expectCoords('-33.8688, +151.2093', { lat: -33.8688, lng: 151.2093 })
  })

  it('tolerates surrounding and extra whitespace', () => {
    expectCoords('   49.1234 ,   16.5678  ', { lat: 49.1234, lng: 16.5678 })
  })

  it('accepts N/S/E/W suffixes on decimals', () => {
    expectCoords('49.1234N 16.5678E', { lat: 49.1234, lng: 16.5678 })
    expectCoords('33.8688S, 151.2093W', { lat: -33.8688, lng: -151.2093 })
  })

  it('accepts integer degrees', () => {
    expectCoords('49 16', { lat: 49, lng: 16 })
  })
})

describe('parseCoordinates — degrees-minutes-seconds', () => {
  it('parses DMS with hemisphere suffixes', () => {
    expectCoords('49°7\'24.2"N 16°34\'12.5"E', { lat: 49.12339, lng: 16.57014 })
  })

  it('tolerates spaces between the DMS parts', () => {
    expectCoords('49° 7\' 24.2" N , 16° 34\' 12.5" E', { lat: 49.12339, lng: 16.57014 })
  })

  it('handles southern/western hemispheres', () => {
    expectCoords('33°52\'7.7"S 151°12\'33.5"E', { lat: -33.86881, lng: 151.20931 })
  })

  it('accepts unicode primes/quotes for minutes and seconds', () => {
    expectCoords('49°7′24.2″N 16°34′12.5″E', { lat: 49.12339, lng: 16.57014 })
  })

  it('accepts a double single-quote for seconds', () => {
    expectCoords("49°7'24.2''N 16°34'12.5''E", { lat: 49.12339, lng: 16.57014 })
  })
})

describe('parseCoordinates — degrees-decimal-minutes', () => {
  it('parses DDM with hemisphere suffixes', () => {
    expectCoords("49°7.4'N, 16°34.2'E", { lat: 49.12333, lng: 16.57 })
  })

  it('parses DDM with southern hemisphere', () => {
    expectCoords("33°52.13'S, 151°12.56'E", { lat: -33.86883, lng: 151.20933 })
  })
})

describe('parseCoordinates — axis ordering', () => {
  it('reorders when hemispheres put longitude first', () => {
    expectCoords('16.5678E, 49.1234N', { lat: 49.1234, lng: 16.5678 })
  })
})

describe('parseCoordinates — invalid input', () => {
  it('reports empty input', () => {
    expect(parseCoordinates('')).toEqual({ ok: false, error: 'empty' })
    expect(parseCoordinates('   ')).toEqual({ ok: false, error: 'empty' })
  })

  it('reports a single value as a format error', () => {
    expect(parseCoordinates('49.1234')).toEqual({ ok: false, error: 'format' })
  })

  it('reports gibberish as a format error', () => {
    expect(parseCoordinates('not coordinates')).toEqual({ ok: false, error: 'format' })
    expect(parseCoordinates('49.1234, abc')).toEqual({ ok: false, error: 'format' })
  })

  it('reports contradictory hemispheres as a format error', () => {
    expect(parseCoordinates('49.1N 16.5N')).toEqual({ ok: false, error: 'format' })
  })

  it('rejects minutes or seconds ≥ 60', () => {
    expect(parseCoordinates("49°60'N 16°0'E")).toEqual({ ok: false, error: 'format' })
    expect(parseCoordinates('49°0\'75"N 16°0\'0"E')).toEqual({ ok: false, error: 'format' })
  })

  it('reports out-of-range coordinates', () => {
    expect(parseCoordinates('120.0, 16.5')).toEqual({ ok: false, error: 'range' })
    expect(parseCoordinates('49.0, 200.0')).toEqual({ ok: false, error: 'range' })
  })

  it('reports too many comma-separated parts as a format error', () => {
    expect(parseCoordinates('49.1, 16.5, 3.0')).toEqual({ ok: false, error: 'format' })
  })
})

describe('formatCoordinates', () => {
  it('formats to canonical decimal degrees', () => {
    expect(formatCoordinates({ lat: 49.1234, lng: 16.5678 })).toBe('49.123400, 16.567800')
  })

  it('round-trips through the parser', () => {
    const text = formatCoordinates({ lat: -33.8688, lng: 151.2093 })
    const result = parseCoordinates(text)
    expect(result.ok).toBe(true)
    if (result.ok) {
      expect(result.value.lat).toBeCloseTo(-33.8688, 4)
      expect(result.value.lng).toBeCloseTo(151.2093, 4)
    }
  })

  it('respects a custom precision', () => {
    expect(formatCoordinates({ lat: 49.1234, lng: 16.5678 }, 2)).toBe('49.12, 16.57')
  })
})
