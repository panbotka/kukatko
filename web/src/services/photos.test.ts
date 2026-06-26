import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from './auth'
import { buildPhotoQuery, fetchPhotos, type PhotoListResponse, thumbUrl } from './photos'

const RESPONSE: PhotoListResponse = {
  photos: [
    {
      uid: 'ph1',
      file_hash: 'abc',
      file_name: 'a.jpg',
      file_size: 100,
      file_mime: 'image/jpeg',
      file_width: 800,
      file_height: 600,
      taken_at_source: 'exif',
      title: '',
      description: '',
      camera_make: '',
      camera_model: '',
      lens_model: '',
      private: false,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    },
  ],
  total: 1,
  limit: 100,
  offset: 0,
  next_offset: null,
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

describe('buildPhotoQuery', () => {
  it('omits empty and undefined values to keep the query minimal', () => {
    const query = buildPhotoQuery({
      sort: 'newest',
      offset: 0,
      limit: 100,
      q: '',
      camera: 'Canon',
      has_gps: undefined,
    })
    expect(query.get('sort')).toBe('newest')
    expect(query.get('camera')).toBe('Canon')
    expect(query.get('limit')).toBe('100')
    // offset 0 stringifies to "0" (non-empty) and is kept.
    expect(query.get('offset')).toBe('0')
    expect(query.has('q')).toBe(false)
    expect(query.has('has_gps')).toBe(false)
  })
})

describe('fetchPhotos', () => {
  it('requests the photos endpoint with the encoded query and parses the body', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(RESPONSE, 200))
    vi.stubGlobal('fetch', fetchMock)

    await expect(
      fetchPhotos({ sort: 'oldest', limit: 50, offset: 50, has_gps: 'true' }),
    ).resolves.toEqual(RESPONSE)

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toContain('/api/v1/photos?')
    expect(url).toContain('sort=oldest')
    expect(url).toContain('limit=50')
    expect(url).toContain('offset=50')
    expect(url).toContain('has_gps=true')
    expect(init.credentials).toBe('same-origin')
  })

  it('throws ApiError carrying the status on a non-OK response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ error: 'unknown sort' }, 400)))

    await expect(fetchPhotos({ sort: 'newest' })).rejects.toMatchObject({
      name: 'ApiError',
      status: 400,
    })
    await expect(fetchPhotos({ sort: 'newest' })).rejects.toBeInstanceOf(ApiError)
  })
})

describe('thumbUrl', () => {
  it('builds the thumbnail path for a uid and size', () => {
    expect(thumbUrl('ph1', 'tile_500')).toBe('/api/v1/photos/ph1/thumb/tile_500')
  })

  it('appends a download token when provided', () => {
    expect(thumbUrl('ph1', 'tile_500', 'tok 1')).toBe('/api/v1/photos/ph1/thumb/tile_500?t=tok%201')
  })

  it('omits the token when null or empty', () => {
    expect(thumbUrl('ph1', 'tile_500', null)).toBe('/api/v1/photos/ph1/thumb/tile_500')
    expect(thumbUrl('ph1', 'tile_500', '')).toBe('/api/v1/photos/ph1/thumb/tile_500')
  })
})
