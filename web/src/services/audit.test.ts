import { afterEach, describe, expect, it, vi } from 'vitest'

import { type AuditListResponse, fetchAuditLog } from './audit'

function jsonResponse(body: unknown, status: number): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const RESPONSE: AuditListResponse = {
  entries: [
    {
      id: 7,
      actor_uid: 'us1',
      action: 'photo.update',
      target_type: 'photos',
      target_uid: 'ph9',
      details: { field: 'title' },
      ip: '10.0.0.1',
      user_agent: 'curl/8',
      created_at: '2026-07-11T10:00:00Z',
    },
  ],
  total: 1,
  limit: 100,
  offset: 0,
  next_offset: null,
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('fetchAuditLog', () => {
  it('requests /audit and parses the body', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(RESPONSE, 200))
    vi.stubGlobal('fetch', fetchMock)

    await expect(fetchAuditLog()).resolves.toEqual(RESPONSE)

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/audit')
    expect(init.credentials).toBe('same-origin')
  })

  it('serializes the filters and pagination into query params', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(RESPONSE, 200))
    vi.stubGlobal('fetch', fetchMock)

    await fetchAuditLog({
      user: 'us1',
      action: 'photo.update',
      entity_type: 'photos',
      entity_uid: 'ph9',
      since: '2026-07-01T00:00:00Z',
      until: '2026-07-31T23:59:59Z',
      limit: 100,
      offset: 100,
    })

    const [url] = fetchMock.mock.calls[0] as [string]
    expect(url).toContain('/api/v1/audit?')
    expect(url).toContain('user=us1')
    expect(url).toContain('action=photo.update')
    expect(url).toContain('entity_type=photos')
    expect(url).toContain('entity_uid=ph9')
    expect(url).toContain('since=2026-07-01T00%3A00%3A00Z')
    expect(url).toContain('until=2026-07-31T23%3A59%3A59Z')
    expect(url).toContain('limit=100')
    expect(url).toContain('offset=100')
  })

  it('omits empty filters and a zero offset from the query', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(RESPONSE, 200))
    vi.stubGlobal('fetch', fetchMock)

    await fetchAuditLog({ user: '', action: 'photo.delete', limit: 100, offset: 0 })

    const [url] = fetchMock.mock.calls[0] as [string]
    expect(url).not.toContain('user=')
    expect(url).not.toContain('offset=')
    expect(url).toContain('action=photo.delete')
    expect(url).toContain('limit=100')
  })

  it('throws ApiError carrying the status on a non-OK response', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(jsonResponse({ error: 'since must be an RFC 3339 timestamp' }, 400)),
    )
    await expect(fetchAuditLog({ since: 'bad' })).rejects.toMatchObject({
      status: 400,
      message: 'since must be an RFC 3339 timestamp',
    })
  })
})
