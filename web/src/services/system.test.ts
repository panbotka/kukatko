import { afterEach, describe, expect, it, vi } from 'vitest'

import { fetchLibraryCharts, fetchLibraryStats, fetchSystemStatus } from './system'

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('system service URLs', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("reads the library statistics of the current project, never the instance's", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ photos: 3 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(fetchLibraryStats()).resolves.toEqual({ photos: 3 })

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/p/v/stats')
    expect(init.credentials).toBe('same-origin')
  })

  it('reads the chart series of the current project', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ top_cameras: [] }))
    vi.stubGlobal('fetch', fetchMock)

    await fetchLibraryCharts()

    const [url] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/p/v/stats/charts')
  })

  it('keeps the status snapshot an instance route', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({}))
    vi.stubGlobal('fetch', fetchMock)

    await fetchSystemStatus()

    const [url] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/system/status')
  })

  it('throws an ApiError with the status when the statistics cannot be aggregated', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(jsonResponse({ error: 'collecting library statistics failed' }, 500)),
    )

    await expect(fetchLibraryStats()).rejects.toMatchObject({ status: 500 })
  })
})
