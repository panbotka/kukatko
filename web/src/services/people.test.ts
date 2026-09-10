import { describe, expect, it, vi } from 'vitest'

import { ApiError } from './auth'
import {
  attachPerson,
  type Bbox,
  detachPerson,
  faceCropUrl,
  type PhotoSubject,
  subjectAvatarUrl,
} from './people'

/** Reads the `box` parameter back out of a face-crop URL. */
function boxOf(url: string): string {
  return new URL(url, 'https://example.test').searchParams.get('box') ?? ''
}

describe('faceCropUrl', () => {
  it('addresses the photo and carries the box as four numbers', () => {
    const url = faceCropUrl('ph_1', [0.1, 0.2, 0.3, 0.4])
    expect(url.startsWith('/api/v1/photos/ph_1/face?')).toBe(true)
    expect(boxOf(url)).toBe('0.1000,0.2000,0.3000,0.4000')
  })

  it('rounds to the precision the backend keys its cache by', () => {
    // Two renders of the same face must produce the same URL, or every repaint
    // is a cache miss on the server and in the browser alike.
    const noisy: Bbox = [0.30000000000000004, 0.1999999999, 0.15000001, 0.2]
    expect(boxOf(faceCropUrl('ph_1', noisy))).toBe('0.3000,0.2000,0.1500,0.2000')
  })

  it('keeps a box the detector pushed past the frame', () => {
    // The renderer slides such a box back inside rather than clipping it, so the
    // client must not clamp it away first.
    expect(boxOf(faceCropUrl('ph_1', [-0.02, 0.5, 0.1, 0.1]))).toBe('-0.0200,0.5000,0.1000,0.1000')
  })

  it('escapes a photo uid that would otherwise change the path', () => {
    expect(faceCropUrl('a/b?c', [0, 0, 1, 1]).startsWith('/api/v1/photos/a%2Fb%3Fc/face?')).toBe(
      true,
    )
  })
})

describe('subjectAvatarUrl', () => {
  it('addresses the subject avatar route', () => {
    expect(subjectAvatarUrl('su_1')).toBe('/api/v1/subjects/su_1/avatar')
  })
})

/** One JSON reply, the way the backend sends it. */
function jsonResponse(body: unknown, status: number): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const ALICE: PhotoSubject = {
  subject_uid: 'su_a',
  slug: 'alice',
  name: 'Alice',
  type: 'person',
  marker_uid: 'mk_1',
  attached_at: '2026-02-03T10:00:00Z',
}

describe('attachPerson', () => {
  it('posts the subject and answers the resulting list', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ people: [ALICE] }, 200))
    vi.stubGlobal('fetch', fetchMock)

    // The whole list comes back, so nothing has to re-read the photo.
    await expect(attachPerson('ph_1', 'su_a')).resolves.toEqual([ALICE])

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/photos/ph_1/people')
    expect(init.method).toBe('POST')
    expect(init.body).toBe(JSON.stringify({ subject_uid: 'su_a' }))
  })

  it('reports a refusal from the backend', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(jsonResponse({ error: 'no such subject' }, 404)),
    )
    await expect(attachPerson('ph_1', 'nope')).rejects.toBeInstanceOf(ApiError)
  })
})

describe('detachPerson', () => {
  it('addresses the link by both uids and answers what is left', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ people: [] }, 200))
    vi.stubGlobal('fetch', fetchMock)

    await expect(detachPerson('ph_1', 'su_a')).resolves.toEqual([])

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/photos/ph_1/people/su_a')
    expect(init.method).toBe('DELETE')
  })

  it('escapes uids that would otherwise change the path', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ people: [] }, 200))
    vi.stubGlobal('fetch', fetchMock)

    await detachPerson('a/b', 'c?d')

    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/photos/a%2Fb/people/c%3Fd')
  })
})
