import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  ApiError,
  canImport,
  canWrite,
  changePassword,
  fetchMe,
  isAdmin,
  isMaintainer,
  login,
  logout,
  roleAtLeast,
  type Role,
} from './auth'

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
  // The four roles in ascending ladder order: viewer < editor < admin < maintainer.
  const ROLES: Role[] = ['viewer', 'editor', 'admin', 'maintainer']

  it('roleAtLeast respects the strict ladder ordering', () => {
    expect(roleAtLeast('maintainer', 'admin')).toBe(true)
    expect(roleAtLeast('admin', 'editor')).toBe(true)
    expect(roleAtLeast('editor', 'editor')).toBe(true)
    expect(roleAtLeast('viewer', 'editor')).toBe(false)
    // A lower role never meets a higher threshold.
    expect(roleAtLeast('admin', 'maintainer')).toBe(false)
    expect(roleAtLeast('editor', 'admin')).toBe(false)
  })

  it('canWrite is true for editor and above', () => {
    const expected: Record<Role, boolean> = {
      viewer: false,
      editor: true,
      admin: true,
      maintainer: true,
    }
    for (const role of ROLES) {
      expect(canWrite(role)).toBe(expected[role])
    }
  })

  it('isAdmin is admin-or-higher (admin and maintainer)', () => {
    const expected: Record<Role, boolean> = {
      viewer: false,
      editor: false,
      admin: true,
      maintainer: true,
    }
    for (const role of ROLES) {
      expect(isAdmin(role)).toBe(expected[role])
    }
  })

  it('the maintainer/import capability is maintainer-only', () => {
    const expected: Record<Role, boolean> = {
      viewer: false,
      editor: false,
      admin: false,
      maintainer: true,
    }
    for (const role of ROLES) {
      // Import is an operations capability, so it tracks isMaintainer exactly.
      expect(isMaintainer(role)).toBe(expected[role])
      expect(canImport(role)).toBe(expected[role])
    }
  })
})
