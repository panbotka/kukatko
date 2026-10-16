import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from './auth'
import {
  addProjectMember,
  createProject,
  fetchAdminProjects,
  fetchAwaitingPassword,
  fetchInstanceAccounts,
  ProjectApiError,
  resendPasswordLink,
  setInstanceRole,
  slugProblem,
  updateProject,
} from './projects'

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

describe('project API client', () => {
  it('talks to the instance-level routes, never a project prefix', async () => {
    let mock = stubFetch(200, [])
    await fetchAdminProjects()
    expect(sent(mock)).toEqual({ url: '/api/v1/projects', method: 'GET', body: undefined })

    mock = stubFetch(201, {})
    await createProject({ slug: 'rodina', name: 'Rodina' })
    expect(sent(mock)).toEqual({
      url: '/api/v1/projects',
      method: 'POST',
      body: { slug: 'rodina', name: 'Rodina' },
    })

    mock = stubFetch(200, {})
    await updateProject('pjr', { active: false, max_total_bytes: null })
    expect(sent(mock)).toEqual({
      url: '/api/v1/projects/pjr',
      method: 'PATCH',
      body: { active: false, max_total_bytes: null },
    })

    mock = stubFetch(201, {})
    await addProjectMember('pjr', 'u1', 'admin')
    expect(sent(mock)).toEqual({
      url: '/api/v1/projects/pjr/members',
      method: 'POST',
      body: { user_uid: 'u1', role: 'admin' },
    })

    mock = stubFetch(200, [])
    await fetchInstanceAccounts()
    expect(sent(mock).url).toBe('/api/v1/users')

    mock = stubFetch(200, {})
    await setInstanceRole('u2', 'maintainer')
    expect(sent(mock)).toEqual({
      url: '/api/v1/users/u2',
      method: 'PATCH',
      body: { instance_role: 'maintainer' },
    })
  })

  it('sets a project up for somebody and manages their set-password link', async () => {
    let mock = stubFetch(201, { slug: 'rodina', first_admin: { username: 'jana' } })
    const created = await createProject({
      slug: 'rodina',
      name: 'Rodina',
      admin: { email: 'jana@example.com', display_name: 'Jana' },
    })
    expect(sent(mock).body).toEqual({
      slug: 'rodina',
      name: 'Rodina',
      admin: { email: 'jana@example.com', display_name: 'Jana' },
    })
    expect(created.first_admin?.username).toBe('jana')

    mock = stubFetch(200, [])
    await fetchAwaitingPassword('pjr')
    expect(sent(mock)).toEqual({
      url: '/api/v1/projects/pjr/awaiting-password',
      method: 'GET',
      body: undefined,
    })

    mock = stubFetch(200, { username: 'jana' })
    await resendPasswordLink('pjr', 'u1')
    expect(sent(mock)).toEqual({
      url: '/api/v1/projects/pjr/members/u1/password-link',
      method: 'POST',
      body: undefined,
    })
  })

  it('throws the refusal code and its rule, still an ApiError', async () => {
    stubFetch(422, { error: 'invalid_slug', message: 'slug must be lowercase' })
    const error = await createProject({ slug: 'X', name: 'x' }).catch((e: unknown) => e)

    expect(error).toBeInstanceOf(ProjectApiError)
    expect(error).toBeInstanceOf(ApiError)
    expect(error).toMatchObject({
      status: 422,
      code: 'invalid_slug',
      detail: 'slug must be lowercase',
    })
  })

  it('falls back to the status when the refusal has no body', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response('', { status: 502, statusText: 'Bad Gateway' })),
    )
    await expect(fetchAdminProjects()).rejects.toMatchObject({ status: 502, code: 'Bad Gateway' })
  })
})

describe('slugProblem', () => {
  it('applies the backend rule', () => {
    expect(slugProblem('rodina-2026')).toBeNull()
    expect(slugProblem('v')).toBeNull()
    expect(slugProblem('a'.repeat(40))).toBeNull()
    expect(slugProblem('')).toBe('empty')
    expect(slugProblem('-rodina')).toBe('format')
    expect(slugProblem('Rodina')).toBe('format')
    expect(slugProblem('rodina nová')).toBe('format')
    expect(slugProblem('a'.repeat(41))).toBe('format')
    expect(slugProblem('system')).toBe('reserved')
    expect(slugProblem('api')).toBe('reserved')
  })
})
