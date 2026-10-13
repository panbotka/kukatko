import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  fetchRekeyRun,
  pauseRekey,
  RekeyApiError,
  resumeRekey,
  retryRekey,
  startRekey,
} from './rekey'

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

/** The method and URL of the stub's only call. */
function sent(mock: ReturnType<typeof stubFetch>) {
  const [url, init] = mock.mock.calls[0] as [string, RequestInit]
  return { url, method: init.method }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('rekey service', () => {
  it('reads the latest run of a project at the instance route', async () => {
    const mock = stubFetch(200, { uid: 'rk1', state: 'running' })
    const run = await fetchRekeyRun('pj v')

    expect(run?.uid).toBe('rk1')
    expect(sent(mock)).toEqual({ url: '/api/v1/projects/pj%20v/rekey', method: 'GET' })
  })

  it('reads a 404 as "no run"', async () => {
    stubFetch(404, { error: 'not_found' })
    expect(await fetchRekeyRun('pjv')).toBeNull()
  })

  it('throws any other refusal of the read', async () => {
    stubFetch(403, { error: 'forbidden' })
    await expect(fetchRekeyRun('pjv')).rejects.toBeInstanceOf(RekeyApiError)
  })

  it('posts the start and the three steering actions', async () => {
    const cases: [(uid: string) => Promise<unknown>, string][] = [
      [startRekey, '/api/v1/projects/pjv/rekey'],
      [pauseRekey, '/api/v1/projects/pjv/rekey/pause'],
      [resumeRekey, '/api/v1/projects/pjv/rekey/resume'],
      [retryRekey, '/api/v1/projects/pjv/rekey/retry'],
    ]
    for (const [action, url] of cases) {
      const mock = stubFetch(200, { uid: 'rk1' })
      await action('pjv')
      expect(sent(mock)).toEqual({ url, method: 'POST' })
    }
  })

  it('carries the refusal code', async () => {
    stubFetch(409, { error: 'already_running' })
    const error: unknown = await startRekey('pjv').catch((e: unknown) => e)

    expect(error).toBeInstanceOf(RekeyApiError)
    expect((error as RekeyApiError).code).toBe('already_running')
    expect((error as RekeyApiError).status).toBe(409)
  })
})
