import { afterEach, describe, expect, it, vi } from 'vitest'

import { resolveEntityProject } from './resolve'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('resolveEntityProject', () => {
  it('asks the instance-level resolver and returns the slug', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify({ slug: 'rodina' }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(resolveEntityProject('photo', 'ph 1')).resolves.toBe('rodina')
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/resolve/photo/ph%201', {
      credentials: 'same-origin',
      signal: undefined,
    })
  })

  it('reads the one 404 as "nowhere you may go"', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{}', { status: 404 })))

    await expect(resolveEntityProject('task', 'tk1')).resolves.toBeNull()
  })

  it('throws for any other failure', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{}', { status: 429 })))

    await expect(resolveEntityProject('album', 'al1')).rejects.toThrow(/429/)
  })
})
