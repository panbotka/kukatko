import { describe, expect, it, vi } from 'vitest'

import { fetchHealth } from './health'

describe('fetchHealth', () => {
  it('returns the parsed health payload on a 200 response', async () => {
    const payload = { status: 'ok', version: { version: 'dev', commit: 'abc1234' } }
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(payload), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await expect(fetchHealth()).resolves.toEqual(payload)
    expect(fetchMock).toHaveBeenCalledWith('/healthz', { signal: undefined })
  })

  it('throws when the response status is not ok', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('boom', { status: 503 })))

    await expect(fetchHealth()).rejects.toThrow(/health request failed: 503/)
  })
})
