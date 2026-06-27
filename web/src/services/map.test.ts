import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from './auth'
import {
  buildMapQuery,
  fetchMapPhotos,
  type MapFeatureCollection,
  tileLayerUrl,
  toMapset,
} from './map'

const COLLECTION: MapFeatureCollection = {
  type: 'FeatureCollection',
  features: [
    {
      type: 'Feature',
      geometry: { type: 'Point', coordinates: [14.42, 50.08] },
      properties: {
        uid: 'ph1',
        title: 'Prague',
        taken_at: '2026-01-01T00:00:00Z',
        media_type: 'image',
        thumb: '/api/v1/photos/ph1/thumb/tile_224',
      },
    },
  ],
}

function jsonResponse(body: unknown, status: number): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('buildMapQuery', () => {
  it('omits empty and undefined values', () => {
    const query = buildMapQuery({
      taken_after: '2026-01-01',
      taken_before: '',
      archived: 'only',
      private: undefined,
      album: 'al1',
    })
    expect(query.get('taken_after')).toBe('2026-01-01')
    expect(query.get('archived')).toBe('only')
    expect(query.get('album')).toBe('al1')
    expect(query.has('taken_before')).toBe(false)
    expect(query.has('private')).toBe(false)
    expect(query.has('label')).toBe(false)
  })
})

describe('tileLayerUrl', () => {
  it('points at the backend proxy and carries no API key', () => {
    const url = tileLayerUrl('basic')
    expect(url).toBe('/api/v1/map/tiles/basic/{z}/{x}/{y}{r}')
    expect(url).not.toMatch(/api[_-]?key/i)
    expect(url).not.toContain('mapy.com')
  })

  it('switches the mapset segment', () => {
    expect(tileLayerUrl('aerial')).toBe('/api/v1/map/tiles/aerial/{z}/{x}/{y}{r}')
  })
})

describe('toMapset', () => {
  it('passes through known mapsets and defaults unknown ones to basic', () => {
    expect(toMapset('outdoor')).toBe('outdoor')
    expect(toMapset('aerial')).toBe('aerial')
    expect(toMapset('winter')).toBe('basic')
    expect(toMapset('')).toBe('basic')
  })
})

describe('fetchMapPhotos', () => {
  it('requests the GeoJSON feed with the filters and parses the body', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(COLLECTION, 200))
    vi.stubGlobal('fetch', fetchMock)

    await expect(fetchMapPhotos({ album: 'al1', taken_after: '2026-01-01' })).resolves.toEqual(
      COLLECTION,
    )

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toContain('/api/v1/map/photos?')
    expect(url).toContain('album=al1')
    expect(url).toContain('taken_after=2026-01-01')
    expect(init.credentials).toBe('same-origin')
  })

  it('omits the query string entirely when there are no filters', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(COLLECTION, 200))
    vi.stubGlobal('fetch', fetchMock)

    await fetchMapPhotos({})
    const [url] = fetchMock.mock.calls[0] as [string]
    expect(url).toBe('/api/v1/map/photos')
  })

  it('throws ApiError carrying the status on a non-OK response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ error: 'bad filter' }, 400)))
    await expect(fetchMapPhotos({ taken_after: 'nope' })).rejects.toMatchObject({
      name: 'ApiError',
      status: 400,
    })
    await expect(fetchMapPhotos({})).rejects.toBeInstanceOf(ApiError)
  })
})
