import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from './auth'
import {
  buildPhotoQuery,
  fetchPhotos,
  fetchSimilar,
  fetchTimeline,
  type PhotoListResponse,
  saveEdit,
  searchPhotos,
  type SimilarResponse,
  type Timeline,
  thumbUrl,
} from './photos'

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
      thumb_url: '/api/v1/photos/ph1/thumb/tile_500',
      download_url: '/api/v1/photos/ph1/download?original=true',
      title: '',
      description: '',
      camera_make: '',
      camera_model: '',
      lens_model: '',
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

/**
 * The parsed JSON body a recorded `fetch` call was made with. `BodyInit` also
 * covers Blob/FormData, so the string check is what makes reading it back safe —
 * and a non-JSON body fails the test loudly instead of stringifying to junk.
 */
function sentBody(init: RequestInit | undefined): unknown {
  const body = init?.body
  if (typeof body !== 'string') {
    throw new Error(`expected a JSON string body, got ${typeof body}`)
  }
  return JSON.parse(body)
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

  it('expands a comma-joined album/label list into repeated params', () => {
    const query = buildPhotoQuery({ album: 'al_1,al_2', label: 'lb_1' })
    // The backend parses repeated params (?album=a&album=b) and ANDs them.
    expect(query.getAll('album')).toEqual(['al_1', 'al_2'])
    expect(query.getAll('label')).toEqual(['lb_1'])
  })

  it('drops empty segments and emits nothing for an empty list', () => {
    const query = buildPhotoQuery({ album: 'al_1,,al_2,', label: '' })
    expect(query.getAll('album')).toEqual(['al_1', 'al_2'])
    expect(query.has('label')).toBe(false)
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

describe('searchPhotos', () => {
  it('requests the search endpoint with the query and mode, and parses the body', async () => {
    const degradedBody: PhotoListResponse = { ...RESPONSE, mode: 'hybrid', degraded: true }
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(degradedBody, 200))
    vi.stubGlobal('fetch', fetchMock)

    const got = await searchPhotos({ q: 'beach', limit: 20 }, 'semantic')
    expect(got.degraded).toBe(true)
    expect(got.mode).toBe('hybrid')

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toContain('/api/v1/search?')
    expect(url).toContain('q=beach')
    expect(url).toContain('mode=semantic')
    expect(init.credentials).toBe('same-origin')
  })

  it('omits the mode parameter when not provided', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(RESPONSE, 200))
    vi.stubGlobal('fetch', fetchMock)

    await searchPhotos({ q: 'sunset' })
    const [url] = fetchMock.mock.calls[0] as [string]
    expect(url).toContain('q=sunset')
    expect(url).not.toContain('mode=')
  })

  it('throws ApiError carrying the status on a non-OK response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ error: 'q is required' }, 400)))
    await expect(searchPhotos({ q: '' })).rejects.toMatchObject({ name: 'ApiError', status: 400 })
  })
})

describe('fetchSimilar', () => {
  const SIMILAR: SimilarResponse = {
    similar: [{ ...RESPONSE.photos[0], distance: 0.12 }],
  }

  it('requests the similar endpoint and returns the similar array', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(SIMILAR, 200))
    vi.stubGlobal('fetch', fetchMock)

    const got = await fetchSimilar('ph1')
    expect(got).toHaveLength(1)
    expect(got[0].uid).toBe('ph1')
    expect(got[0].distance).toBe(0.12)

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/photos/ph1/similar')
    expect(init.credentials).toBe('same-origin')
  })

  it('appends the limit when provided', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(SIMILAR, 200))
    vi.stubGlobal('fetch', fetchMock)

    await fetchSimilar('ph1', 12)
    const [url] = fetchMock.mock.calls[0] as [string]
    expect(url).toBe('/api/v1/photos/ph1/similar?limit=12')
  })

  it('throws ApiError carrying the status on a non-OK response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ error: 'not found' }, 404)))
    await expect(fetchSimilar('missing')).rejects.toMatchObject({ name: 'ApiError', status: 404 })
  })
})

describe('fetchTimeline', () => {
  const TIMELINE: Timeline = {
    buckets: [
      { year: 2026, month: 2, count: 3, cumulative: 0 },
      { year: 2026, month: 1, count: 5, cumulative: 3 },
    ],
    total: 8,
  }

  it('requests the timeline endpoint with the encoded filters and parses the body', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(TIMELINE, 200))
    vi.stubGlobal('fetch', fetchMock)

    await expect(
      fetchTimeline({ sort: 'newest', camera: 'Canon', has_gps: 'true' }),
    ).resolves.toEqual(TIMELINE)

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toContain('/api/v1/photos/timeline?')
    expect(url).toContain('camera=Canon')
    expect(url).toContain('has_gps=true')
    expect(init.credentials).toBe('same-origin')
  })

  it('throws ApiError carrying the status on a non-OK response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ error: 'bad filter' }, 400)))
    await expect(fetchTimeline({ sort: 'newest' })).rejects.toMatchObject({
      name: 'ApiError',
      status: 400,
    })
    await expect(fetchTimeline({ sort: 'newest' })).rejects.toBeInstanceOf(ApiError)
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

describe('saveEdit', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('sends only the edit itself, never the fields the GET adds', async () => {
    // The edit panel hands back what `fetchEdit` returned, which also carries
    // `photo_uid`/`updated_at`. The PUT body is decoded strictly, so echoing
    // those back is rejected as malformed — they must not reach the wire.
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(jsonResponse({ photo_uid: 'ph1', rotation: 90 }, 200))

    await saveEdit('ph1', {
      photo_uid: 'ph1',
      updated_at: '2026-01-01T00:00:00Z',
      rotation: 90,
      brightness: 0.5,
      contrast: 0,
    })

    expect(sentBody(fetchMock.mock.calls[0][1])).toEqual({
      rotation: 90,
      brightness: 0.5,
      contrast: 0,
    })
  })

  it('carries a crop when there is one, and omits it when there is not', async () => {
    // A fresh Response per call: a body can only be read once.
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockImplementation(() => Promise.resolve(jsonResponse({ rotation: 0 }, 200)))

    const crop = { crop_x: 0.1, crop_y: 0.1, crop_w: 0.8, crop_h: 0.8 }
    await saveEdit('ph1', { ...crop, rotation: 0, brightness: 0, contrast: 0 })
    expect(sentBody(fetchMock.mock.calls[0][1])).toMatchObject(crop)

    // No crop = the fields are simply absent, which is how the API is told so.
    await saveEdit('ph1', { rotation: 0, brightness: 0, contrast: 0 })
    expect(sentBody(fetchMock.mock.calls[1][1])).toEqual({
      rotation: 0,
      brightness: 0,
      contrast: 0,
    })
  })

  it('raises an ApiError when the API rejects the edit', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      jsonResponse({ error: 'invalid rotation' }, 400),
    )
    await expect(
      saveEdit('ph1', { rotation: 45, brightness: 0, contrast: 0 }),
    ).rejects.toBeInstanceOf(ApiError)
  })
})
