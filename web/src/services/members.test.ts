import { describe, expect, it, vi } from 'vitest'

import {
  addMember,
  approveMember,
  fetchMembers,
  MemberApiError,
  removeMember,
  setMemberRole,
  withdrawMember,
} from './members'

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

describe('members service', () => {
  it('lists the members of the current project', async () => {
    const fetchMock = answer(jsonResponse([], 200))

    await expect(fetchMembers()).resolves.toEqual([])

    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/p/v/members')
  })

  it('sends a role change, an approval and a removal to the member', async () => {
    let fetchMock = answer(jsonResponse({ user_uid: 'u2' }, 200))
    await setMemberRole('u2', 'editor')
    let [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/p/v/members/u2')
    expect(init.method).toBe('PATCH')
    expect(init.body).toBe(JSON.stringify({ role: 'editor' }))

    fetchMock = answer(jsonResponse({ user_uid: 'u2' }, 200))
    await approveMember('u2')
    ;[url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/p/v/members/u2/approve')
    expect(init.method).toBe('POST')

    fetchMock = answer(new Response(null, { status: 204 }))
    await removeMember('u2')
    ;[url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/p/v/members/u2')
    expect(init.method).toBe('DELETE')
  })

  it('carries the backend’s refusal code', async () => {
    answer(jsonResponse({ error: 'last_admin' }, 409))

    const failure = await setMemberRole('u2', 'viewer').catch((error: unknown) => error)

    expect(failure).toBeInstanceOf(MemberApiError)
    expect((failure as MemberApiError).code).toBe('last_admin')
    expect((failure as MemberApiError).status).toBe(409)
  })

  it.each([202, 404, 409, 422, 500])(
    'resolves an add the same way whatever the backend answers (%i)',
    async (status) => {
      const fetchMock = answer(jsonResponse({ status: 'accepted', error: 'whatever' }, status))

      await expect(addMember('jana@example.org', 'viewer')).resolves.toBeUndefined()

      const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
      expect(url).toBe('/api/v1/p/v/members')
      expect(init.method).toBe('POST')
      expect(init.body).toBe(JSON.stringify({ identifier: 'jana@example.org', role: 'viewer' }))
    },
  )

  it.each([202, 404, 409, 422, 500])(
    'resolves a withdrawal the same way whatever the backend answers (%i)',
    async (status) => {
      const fetchMock = answer(jsonResponse({ status: 'accepted', error: 'whatever' }, status))

      await expect(withdrawMember('jana@example.org')).resolves.toBeUndefined()

      const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
      expect(url).toBe('/api/v1/p/v/members/withdraw')
      expect(init.method).toBe('POST')
      expect(init.body).toBe(JSON.stringify({ identifier: 'jana@example.org' }))
    },
  )

  it('rejects an add only when no answer came at all', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))

    await expect(addMember('jana', 'viewer')).rejects.toThrow('Failed to fetch')
  })
})
