import { describe, expect, it } from 'vitest'

import { type MapFeature } from '../services/map'

import {
  boundsContain,
  formatBBox,
  normalizeBounds,
  parseBBox,
  photosInBounds,
  sameBBox,
} from './mapArea'

function at(uid: string, lat: number, lng: number): MapFeature {
  return {
    type: 'Feature',
    geometry: { type: 'Point', coordinates: [lng, lat] },
    properties: { uid, title: '', media_type: 'image', thumb: '' },
  }
}

describe('normalizeBounds', () => {
  it('keeps an ordinary box as it is', () => {
    const box = { south: 49, west: 16, north: 50, east: 17 }
    expect(normalizeBounds(box)).toEqual(box)
  })

  it('wraps the longitudes of a world copy the map was panned to', () => {
    expect(normalizeBounds({ south: 49, west: 376, north: 50, east: 377 })).toEqual({
      south: 49,
      west: 16,
      north: 50,
      east: 17,
    })
  })

  it('leaves west east of east for a box across the antimeridian', () => {
    expect(normalizeBounds({ south: -20, west: 170, north: -10, east: 190 })).toEqual({
      south: -20,
      west: 170,
      north: -10,
      east: -170,
    })
  })

  it('turns a box a whole world wide into the whole world, and clamps the poles', () => {
    expect(normalizeBounds({ south: -95, west: -400, north: 95, east: 400 })).toEqual({
      south: -90,
      west: -180,
      north: 90,
      east: 180,
    })
  })
})

describe('boundsContain', () => {
  it('includes the edges', () => {
    const box = { south: 49, west: 16, north: 50, east: 17 }
    expect(boundsContain(box, 49, 16)).toBe(true)
    expect(boundsContain(box, 50, 17)).toBe(true)
    expect(boundsContain(box, 50.0001, 16.5)).toBe(false)
  })

  it('covers both sides of the antimeridian', () => {
    const box = { south: -20, west: 170, north: -10, east: -170 }
    expect(boundsContain(box, -15, 178)).toBe(true)
    expect(boundsContain(box, -15, -172)).toBe(true)
    expect(boundsContain(box, -15, 0)).toBe(false)
  })
})

describe('photosInBounds', () => {
  const features = [at('newest', 49.2, 16.6), at('prague', 50.08, 14.42), at('oldest', 49.3, 16.7)]

  it('keeps the feed order (newest first) and drops what is outside', () => {
    const inside = photosInBounds(features, { south: 49, west: 16, north: 50, east: 17 })
    expect(inside.map((f) => f.properties.uid)).toEqual(['newest', 'oldest'])
  })

  it('lists nothing before the map has reported a box', () => {
    expect(photosInBounds(features, null)).toEqual([])
  })

  it('reads a panned world copy the same as the original', () => {
    const inside = photosInBounds(features, { south: 49, west: 376, north: 50, east: 377 })
    expect(inside).toHaveLength(2)
  })
})

describe('formatBBox / parseBBox', () => {
  it('rounds outward, so a photo on the edge is never rounded out of the area', () => {
    expect(
      formatBBox({ south: 49.123456, west: 16.123456, north: 50.123451, east: 17.123451 }),
    ).toBe('49.12345,16.12345,50.12346,17.12346')
  })

  it('round-trips a box through the URL', () => {
    const box = { south: 49.1, west: 16.5, north: 49.4, east: 16.9 }
    expect(parseBBox(formatBBox(box))).toEqual(box)
  })

  it('rejects anything malformed or out of range', () => {
    expect(parseBBox('')).toBeNull()
    expect(parseBBox('1,2,3')).toBeNull()
    expect(parseBBox('a,2,3,4')).toBeNull()
    expect(parseBBox('50,16,49,17')).toBeNull()
    expect(parseBBox('-91,16,49,17')).toBeNull()
    expect(parseBBox('49,16,50,181')).toBeNull()
  })

  it('compares boxes as the URL would write them', () => {
    const a = { south: 49.000001, west: 16, north: 50, east: 17 }
    const b = { south: 49.000002, west: 16, north: 50, east: 17 }
    expect(sameBBox(a, b)).toBe(true)
    expect(sameBBox(a, { ...b, east: 18 })).toBe(false)
    expect(sameBBox(null, null)).toBe(true)
    expect(sameBBox(a, null)).toBe(false)
  })
})
