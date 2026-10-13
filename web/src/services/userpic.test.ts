import { afterEach, describe, expect, it, vi } from 'vitest'

import { setCurrentProjectSlug } from './api'
import { fetchMyPicture, userAvatarUrl } from './userpic'

afterEach(() => {
  setCurrentProjectSlug(null)
  vi.unstubAllGlobals()
})

describe('userAvatarUrl', () => {
  it('asks the current project, whose chain falls back to the person there', () => {
    setCurrentProjectSlug('rodina')
    expect(userAvatarUrl('usr_1')).toBe('/api/v1/p/rodina/users/usr_1/avatar')
    expect(userAvatarUrl('usr_1', 2)).toBe('/api/v1/p/rodina/users/usr_1/avatar?v=2')
  })

  it('asks the instance route when no project is current', () => {
    setCurrentProjectSlug(null)
    expect(userAvatarUrl('usr_1')).toBe('/api/v1/users/usr_1/avatar')
  })
})

describe('fetchMyPicture', () => {
  it('reads the picture state in the current project', async () => {
    setCurrentProjectSlug('v')
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ origin: 'subject', subject_uid: 'sub1' }), { status: 200 }),
      )
    vi.stubGlobal('fetch', fetchMock)

    await expect(fetchMyPicture()).resolves.toEqual({ origin: 'subject', subject_uid: 'sub1' })
    const [url] = fetchMock.mock.calls[0] as [string]
    expect(url).toBe('/api/v1/p/v/auth/picture')
  })
})
