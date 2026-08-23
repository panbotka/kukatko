import { describe, expect, it, vi } from 'vitest'

import { fetchPublicSettings } from './settings'

describe('fetchPublicSettings', () => {
  it('reads the registration flag from the anonymous endpoint', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ registration_enabled: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await expect(fetchPublicSettings()).resolves.toEqual({ registration_enabled: true })

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/settings/public')
    // The endpoint has no guard, so the call carries no credentials.
    expect(init.credentials).toBeUndefined()
  })

  it('throws when the endpoint answers with an error status', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('nope', { status: 500 })))

    await expect(fetchPublicSettings()).rejects.toThrow(/500/)
  })
})
