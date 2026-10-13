import { describe, expect, it, vi } from 'vitest'

import {
  acceptInvite,
  createInvite,
  fetchInvites,
  fetchPublicInvite,
  InviteApiError,
  inviteAddress,
  revokeInvite,
} from './invites'

function jsonResponse(body: unknown, status: number): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

/** Stubs `fetch` with one answer and returns the mock, to read the request back. */
function answer(response: Response) {
  const fetchMock = vi.fn().mockResolvedValue(response)
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('invites service', () => {
  it('manages the current project’s invites under its prefix', async () => {
    let fetchMock = answer(jsonResponse({ invites: [], default_days: 7, max_days: 90 }, 200))
    await expect(fetchInvites()).resolves.toEqual({ invites: [], default_days: 7, max_days: 90 })
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/p/v/invites')

    fetchMock = answer(jsonResponse({ uid: 'iv1' }, 201))
    await createInvite({ role: 'editor', days: 14, max_uses: 3 })
    let [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/p/v/invites')
    expect(init.method).toBe('POST')
    expect(init.body).toBe(JSON.stringify({ role: 'editor', days: 14, max_uses: 3 }))

    fetchMock = answer(new Response(null, { status: 204 }))
    await revokeInvite('iv1')
    ;[url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/p/v/invites/iv1')
    expect(init.method).toBe('DELETE')
  })

  it('reads and accepts a public invite outside any project', async () => {
    let fetchMock = answer(
      jsonResponse(
        {
          project_name: 'Veselice',
          role: 'curator',
          registration_role: 'curator',
          signed_in: true,
        },
        200,
      ),
    )
    await expect(fetchPublicInvite('AB3DEF7H')).resolves.toMatchObject({
      project_name: 'Veselice',
    })
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/invite/AB3DEF7H')

    fetchMock = answer(jsonResponse({ slug: 'v', role: 'curator', changed: true }, 200))
    await expect(acceptInvite('AB3DEF7H')).resolves.toEqual({
      slug: 'v',
      role: 'curator',
      changed: true,
    })
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/invite/AB3DEF7H/accept')
    expect(init.method).toBe('POST')
  })

  it('carries the backend’s refusal code', async () => {
    answer(jsonResponse({ error: 'not_found' }, 404))
    const failure = await fetchPublicInvite('nope').catch((error: unknown) => error)
    expect(failure).toBeInstanceOf(InviteApiError)
    expect((failure as InviteApiError).status).toBe(404)
    expect((failure as InviteApiError).code).toBe('not_found')
  })

  it('makes a host-relative invite URL absolute', () => {
    expect(inviteAddress({ url: '/invite/AB3DEF7H' })).toBe(
      `${window.location.origin}/invite/AB3DEF7H`,
    )
    expect(inviteAddress({ url: 'https://fotky.example.cz/invite/AB3DEF7H' })).toBe(
      'https://fotky.example.cz/invite/AB3DEF7H',
    )
  })
})
