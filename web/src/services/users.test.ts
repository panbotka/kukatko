import { afterEach, describe, expect, it, vi } from 'vitest'

import { setUserDisabled, updateUser } from './users'

/** Stubs fetch with one JSON answer and returns the mock. */
function stubFetch(status: number, body: unknown) {
  const mock = vi.fn().mockResolvedValue(
    new Response(JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' },
    }),
  )
  vi.stubGlobal('fetch', mock)
  return mock
}

/** The method, URL and decoded body of the stub's only call. */
function sent(mock: ReturnType<typeof stubFetch>) {
  const [url, init] = mock.mock.calls[0] as [string, RequestInit]
  return {
    url,
    method: init.method,
    body: init.body === undefined ? undefined : (JSON.parse(init.body as string) as unknown),
  }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('account-record client', () => {
  it('patches only the fields it is given', async () => {
    const mock = stubFetch(200, {})
    await updateUser('u2', { display_name: 'Jana' })
    expect(sent(mock)).toEqual({
      url: '/api/v1/admin/users/u2',
      method: 'PATCH',
      body: { display_name: 'Jana' },
    })
  })

  it('unblocks with the flag alone, never echoing a stale row back', async () => {
    const mock = stubFetch(200, {})
    await setUserDisabled(
      {
        uid: 'u2',
        display_name: 'Stale',
        email: 'stale@example.com',
        instance_role: 'user',
        note: 'stale',
      } as Parameters<typeof setUserDisabled>[0],
      false,
    )
    expect(sent(mock)).toEqual({
      url: '/api/v1/admin/users/u2',
      method: 'PATCH',
      body: { disabled: false },
    })
  })

  it('blocks through the dedicated endpoint with no body', async () => {
    const mock = stubFetch(200, {})
    await setUserDisabled({ uid: 'u2' }, true)
    expect(sent(mock)).toEqual({
      url: '/api/v1/admin/users/u2/disable',
      method: 'POST',
      body: undefined,
    })
  })
})
