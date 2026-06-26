import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError, canWrite, changePassword, fetchMe, login, logout, roleAtLeast } from './auth'

const SESSION = {
  user: {
    uid: 'u1',
    username: 'alice',
    display_name: 'Alice',
    email: 'alice@example.com',
    role: 'editor' as const,
    disabled: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  },
  download_token: 'tok-123',
}

function jsonResponse(body: unknown, status: number): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('login', () => {
  it('posts credentials and returns the session on 200', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(SESSION, 200))
    vi.stubGlobal('fetch', fetchMock)

    await expect(login('alice', 'secret')).resolves.toEqual(SESSION)

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/auth/login')
    expect(init.method).toBe('POST')
    expect(init.body).toBe(JSON.stringify({ username: 'alice', password: 'secret' }))
  })

  it('throws ApiError with status 401 on bad credentials', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(jsonResponse({ error: 'invalid username or password' }, 401)),
    )

    await expect(login('alice', 'nope')).rejects.toMatchObject({
      name: 'ApiError',
      status: 401,
    })
  })

  it('throws ApiError with status 429 when rate limited', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ error: 'too many' }, 429)))

    const error = await login('alice', 'secret').catch((e: unknown) => e)
    expect(error).toBeInstanceOf(ApiError)
    expect((error as ApiError).status).toBe(429)
  })
})

describe('fetchMe', () => {
  it('returns the session when authenticated', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse(SESSION, 200)))
    await expect(fetchMe()).resolves.toEqual(SESSION)
  })

  it('returns null on 401 (no session)', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 401 })))
    await expect(fetchMe()).resolves.toBeNull()
  })
})

describe('logout', () => {
  it('posts to the logout endpoint and resolves on 204', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(logout()).resolves.toBeUndefined()
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/auth/logout',
      expect.objectContaining({ method: 'POST' }),
    )
  })
})

describe('changePassword', () => {
  it('posts current and new passwords', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(changePassword('old', 'newpassword')).resolves.toBeUndefined()

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(init.body).toBe(JSON.stringify({ current_password: 'old', new_password: 'newpassword' }))
  })

  it('throws ApiError on 401 (wrong current password)', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ error: 'wrong' }, 401)))
    await expect(changePassword('bad', 'newpassword')).rejects.toMatchObject({ status: 401 })
  })
})

describe('role helpers', () => {
  it('roleAtLeast respects the privilege ordering', () => {
    expect(roleAtLeast('admin', 'editor')).toBe(true)
    expect(roleAtLeast('editor', 'editor')).toBe(true)
    expect(roleAtLeast('viewer', 'editor')).toBe(false)
  })

  it('canWrite is true for editor and admin only', () => {
    expect(canWrite('admin')).toBe(true)
    expect(canWrite('editor')).toBe(true)
    expect(canWrite('viewer')).toBe(false)
  })
})
