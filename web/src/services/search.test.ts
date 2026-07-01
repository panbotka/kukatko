import { afterEach, describe, expect, it, vi } from 'vitest'

import { type GlobalSearchResult, globalSearch, hasEntityMatches, isEmptyResult } from './search'

function jsonResponse(body: unknown, status: number): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const RESULT: GlobalSearchResult = {
  query: 'beach',
  albums: [{ uid: 'al1', title: 'Beach trip', cover: 'ph9', photo_count: 12 }],
  labels: [{ uid: 'lb1', name: 'beach', photo_count: 40 }],
  people: [{ uid: 'su1', name: 'Beatrice', cover: 'ph3' }],
  photos: [],
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('globalSearch', () => {
  it('requests the grouped endpoint with the query and parses the body', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(RESULT, 200))
    vi.stubGlobal('fetch', fetchMock)

    await expect(globalSearch('beach')).resolves.toEqual(RESULT)

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toContain('/api/v1/search/global?')
    expect(url).toContain('q=beach')
    expect(init.credentials).toBe('same-origin')
  })

  it('URL-encodes the query', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(RESULT, 200))
    vi.stubGlobal('fetch', fetchMock)

    await globalSearch('a & b')
    const [url] = fetchMock.mock.calls[0] as [string]
    expect(url).toContain('q=a+%26+b')
  })

  it('throws ApiError carrying the status on a non-OK response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ error: 'q is required' }, 400)))
    await expect(globalSearch('')).rejects.toMatchObject({ status: 400, message: 'q is required' })
  })
})

describe('hasEntityMatches', () => {
  it('is true when any album/label/person is present', () => {
    expect(hasEntityMatches(RESULT)).toBe(true)
  })

  it('is false when only photos (or nothing) match', () => {
    expect(hasEntityMatches({ query: 'x', albums: [], labels: [], people: [], photos: [] })).toBe(
      false,
    )
  })
})

describe('isEmptyResult', () => {
  it('is true only when every group is empty', () => {
    expect(isEmptyResult({ query: 'x', albums: [], labels: [], people: [], photos: [] })).toBe(true)
    expect(isEmptyResult(RESULT)).toBe(false)
  })
})
