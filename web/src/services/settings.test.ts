import { describe, expect, it, vi } from 'vitest'

import { ApiError } from './auth'
import {
  fetchProjectPublicSettings,
  fetchProjectSettings,
  fetchProjectUsage,
  fetchPublicSettings,
  updateProjectSettings,
} from './settings'

describe('fetchPublicSettings', () => {
  it('reads the instance flags from the anonymous endpoint', async () => {
    const flags = { passkeys_enabled: true, mail_enabled: false }
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(flags), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await expect(fetchPublicSettings()).resolves.toEqual(flags)

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

/** The admin wire shape as `GET /settings` returns it. */
const adminRecord = {
  registration_enabled: true,
  registration_secret: 'veselice',
  welcome_markdown: '# Ahoj',
  updated_at: '2026-08-20T09:00:00Z',
  updated_by_uid: 'u1',
}

/** A JSON Response with the given status and body. */
function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('fetchProjectPublicSettings', () => {
  it("reads the named project's anonymous endpoint without credentials", async () => {
    const body = { project_name: 'Rodina Nováků', registration_enabled: true }
    const fetchMock = vi.fn().mockResolvedValue(json(body))
    vi.stubGlobal('fetch', fetchMock)

    await expect(fetchProjectPublicSettings('rodina nováků')).resolves.toEqual(body)

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/p/rodina%20nov%C3%A1k%C5%AF/settings/public')
    expect(init.credentials).toBeUndefined()
  })

  it('rejects an unknown or deactivated project with its 404', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json({ error: 'project not found' }, 404)))

    const failure: unknown = await fetchProjectPublicSettings('nic').catch(
      (error: unknown) => error,
    )
    expect(failure).toBeInstanceOf(ApiError)
    expect((failure as ApiError).status).toBe(404)
  })
})

describe('fetchProjectUsage', () => {
  it("reads the current project's usage with the session cookie", async () => {
    const usage = { photos: 3, originals_bytes: 2048, max_file_bytes: null, max_total_bytes: 4096 }
    const fetchMock = vi.fn().mockResolvedValue(json(usage))
    vi.stubGlobal('fetch', fetchMock)

    await expect(fetchProjectUsage()).resolves.toEqual(usage)

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/p/v/settings/usage')
    expect(init.credentials).toBe('same-origin')
  })
})

describe('fetchProjectSettings', () => {
  it('reads the full record, secret included, with the session cookie', async () => {
    const fetchMock = vi.fn().mockResolvedValue(json(adminRecord))
    vi.stubGlobal('fetch', fetchMock)

    await expect(fetchProjectSettings()).resolves.toEqual(adminRecord)

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/p/v/settings')
    expect(init.method).toBe('GET')
    // Admin-only server-side, so the session has to travel with the request.
    expect(init.credentials).toBe('same-origin')
  })

  it('throws an ApiError carrying the status when the guard refuses', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json({ error: 'forbidden' }, 403)))

    await expect(fetchProjectSettings()).rejects.toMatchObject({
      name: 'ApiError',
      status: 403,
      message: 'forbidden',
    })
  })
})

describe('updateProjectSettings', () => {
  it('PUTs all three values together and returns the persisted record', async () => {
    const fetchMock = vi.fn().mockResolvedValue(json(adminRecord))
    vi.stubGlobal('fetch', fetchMock)

    await expect(
      updateProjectSettings({
        registration_enabled: true,
        registration_secret: 'veselice',
        welcome_markdown: '# Ahoj',
      }),
    ).resolves.toEqual(adminRecord)

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/p/v/settings')
    expect(init.method).toBe('PUT')
    expect(JSON.parse(init.body as string)).toEqual({
      registration_enabled: true,
      registration_secret: 'veselice',
      welcome_markdown: '# Ahoj',
    })
  })

  it("passes the server's rejection through as the ApiError message", async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          json(
            { error: 'registration secret must not be empty when registration is enabled' },
            400,
          ),
        ),
    )

    const failure = await updateProjectSettings({
      registration_enabled: true,
      registration_secret: '',
      welcome_markdown: '',
    }).catch((error: unknown) => error)

    expect(failure).toBeInstanceOf(ApiError)
    expect((failure as ApiError).status).toBe(400)
    expect((failure as ApiError).message).toBe(
      'registration secret must not be empty when registration is enabled',
    )
  })
})
