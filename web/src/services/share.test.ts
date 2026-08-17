import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { type ShareManifestFile } from '../lib/photoShare'

import { ApiError } from './auth'
import { fetchShareFile, fetchShareManifest } from './share'

/** A JSON response, as `fetch` would resolve it. */
function jsonResponse(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: '',
    json: () => Promise.resolve(body),
  } as unknown as Response
}

/** A binary response carrying `bytes` under `type`. */
function blobResponse(bytes: number[], type: string): Response {
  return {
    ok: true,
    status: 200,
    statusText: '',
    blob: () => Promise.resolve(new Blob([new Uint8Array(bytes)], { type })),
  } as unknown as Response
}

/** A failing response with the standard error envelope. */
function errorResponse(status: number, message = 'nope'): Response {
  return jsonResponse({ error: message }, status)
}

const fetchMock = vi.fn<typeof fetch>()

beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
  fetchMock.mockReset()
})

/** The manifest entry of an ordinary photo. */
const photo: ShareManifestFile = {
  uid: 'ph1',
  name: 'beach.jpg',
  mime: 'image/jpeg',
  size: 2048,
  preview: false,
}

/** The manifest entry of a RAW original, to be shared as a JPEG preview. */
const rawPreview: ShareManifestFile = {
  uid: 'ph2',
  name: 'IMG_0007.jpg',
  mime: 'image/jpeg',
  size: 26_000_000,
  preview: true,
}

describe('fetchShareManifest', () => {
  it('posts the selection and returns the files it is told about', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ files: [photo] }))

    const files = await fetchShareManifest(['ph1'])

    expect(files).toEqual([photo])
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/v1/photos/share-manifest')
    expect(init?.method).toBe('POST')
    expect(init?.body).toBe(JSON.stringify({ photo_uids: ['ph1'] }))
  })

  it('surfaces the over-cap refusal as a 413 the caller can recognise', async () => {
    fetchMock.mockResolvedValue(errorResponse(413, 'too many photos'))

    await expect(fetchShareManifest(['ph1'])).rejects.toMatchObject({
      status: 413,
      message: 'too many photos',
    })
  })
})

describe('fetchShareFile', () => {
  it('fetches an original through the app, never as a cross-origin redirect', async () => {
    fetchMock.mockResolvedValue(blobResponse([0xff, 0xd8], 'image/jpeg'))

    const file = await fetchShareFile(photo)

    expect(file).toBeInstanceOf(File)
    expect(file.name).toBe('beach.jpg')
    expect(file.type).toBe('image/jpeg')
    // proxy=true is what keeps the bytes readable by the page: without it the route
    // may answer with a redirect to the object store, which fetch cannot read.
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/photos/ph1/download?original=true&proxy=true')
  })

  it('fetches a RAW as its largest cached preview', async () => {
    fetchMock.mockResolvedValue(blobResponse([0xff, 0xd8], 'image/jpeg'))

    const file = await fetchShareFile(rawPreview)

    expect(file.name).toBe('IMG_0007.jpg')
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/photos/ph2/thumb/fit_3840?proxy=true')
  })

  it('falls back down the preview sizes when the largest is not there', async () => {
    fetchMock
      .mockResolvedValueOnce(errorResponse(404))
      .mockResolvedValueOnce(errorResponse(500))
      .mockResolvedValueOnce(blobResponse([0xff, 0xd8], 'image/jpeg'))

    const file = await fetchShareFile(rawPreview)

    expect(file.name).toBe('IMG_0007.jpg')
    expect(fetchMock.mock.calls.map((call) => call[0])).toEqual([
      '/api/v1/photos/ph2/thumb/fit_3840?proxy=true',
      '/api/v1/photos/ph2/thumb/fit_2560?proxy=true',
      '/api/v1/photos/ph2/thumb/fit_1920?proxy=true',
    ])
  })

  it('fails with the last status when no address answers', async () => {
    fetchMock.mockResolvedValue(errorResponse(404, 'thumbnail unavailable'))

    await expect(fetchShareFile(rawPreview)).rejects.toBeInstanceOf(ApiError)
    // Every preview size was tried before giving up on the photo.
    expect(fetchMock).toHaveBeenCalledTimes(4)
  })

  it('falls back to the response type when the manifest states none', async () => {
    fetchMock.mockResolvedValue(blobResponse([1, 2], 'video/mp4'))

    const file = await fetchShareFile({ ...photo, name: 'clip.mp4', mime: '' })

    expect(file.type).toBe('video/mp4')
  })
})
