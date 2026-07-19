import { describe, expect, it, vi } from 'vitest'

import { fetchCapabilities } from './capabilities'

describe('fetchCapabilities', () => {
  it('returns the parsed capabilities payload on a 200 response', async () => {
    const payload = { semantic_search: true }
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(payload), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await expect(fetchCapabilities()).resolves.toEqual(payload)
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/capabilities', {
      credentials: 'same-origin',
      signal: undefined,
    })
  })

  it('throws when the response status is not ok', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('nope', { status: 401 })))

    await expect(fetchCapabilities()).rejects.toThrow(/capabilities request failed: 401/)
  })
})
