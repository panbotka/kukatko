import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  fetchNotifications,
  fetchUnreadByProject,
  fetchUnreadNotificationCount,
  markAllNotificationsRead,
  type NotificationPreference,
  notificationTarget,
  updateNotificationPreferences,
  withPreference,
} from './notifications'

/** A fresh admin's effective set: every pair on its default. */
const DEFAULTS: NotificationPreference[] = [
  { kind: 'tagged', channel: 'push', enabled: true, is_default: true },
  { kind: 'registration_pending', channel: 'push', enabled: true, is_default: true },
  { kind: 'registration_pending', channel: 'email', enabled: false, is_default: true },
]

describe('withPreference', () => {
  it('sends only the changed pair while everything is on its default', () => {
    expect(withPreference(DEFAULTS, 'tagged', 'push', false)).toEqual([
      { kind: 'tagged', channel: 'push', enabled: false },
    ])
  })

  it('keeps every stored choice, on any channel, and replaces only the same pair', () => {
    const current: NotificationPreference[] = [
      { kind: 'tagged', channel: 'push', enabled: false, is_default: false },
      { kind: 'registration_pending', channel: 'push', enabled: true, is_default: true },
      { kind: 'registration_pending', channel: 'email', enabled: true, is_default: false },
    ]
    expect(withPreference(current, 'registration_pending', 'push', false)).toEqual([
      { kind: 'tagged', channel: 'push', enabled: false },
      { kind: 'registration_pending', channel: 'email', enabled: true },
      { kind: 'registration_pending', channel: 'push', enabled: false },
    ])
  })

  it('does not drop the other channel of the same kind', () => {
    const current: NotificationPreference[] = [
      { kind: 'registration_pending', channel: 'push', enabled: false, is_default: false },
      { kind: 'registration_pending', channel: 'email', enabled: false, is_default: true },
    ]
    expect(withPreference(current, 'registration_pending', 'email', true)).toEqual([
      { kind: 'registration_pending', channel: 'push', enabled: false },
      { kind: 'registration_pending', channel: 'email', enabled: true },
    ])
  })
})

describe('updateNotificationPreferences', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('puts the choices with their channel and returns the effective set', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ preferences: DEFAULTS }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    const input = [{ kind: 'registration_pending', channel: 'email', enabled: true }]
    await expect(updateNotificationPreferences(input)).resolves.toEqual(DEFAULTS)
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/p/v/notifications/preferences', {
      method: 'PUT',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ preferences: input }),
    })
  })
})

/** A JSON 200 response with `body`. */
function ok(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('inbox requests', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('lists the inbox with only the query parameters that were asked for', async () => {
    const page = { notifications: [], total: 0, limit: 30, offset: 0 }
    const fetchMock = vi.fn().mockImplementation(() => Promise.resolve(ok(page)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(fetchNotifications()).resolves.toEqual(page)
    await fetchNotifications({ unread: true, limit: 8, offset: 30 })
    await fetchNotifications({ unread: false, offset: 0 })

    expect(fetchMock.mock.calls.map((call) => call[0] as string)).toEqual([
      '/api/v1/p/v/notifications',
      '/api/v1/p/v/notifications?unread=true&limit=8&offset=30',
      '/api/v1/p/v/notifications',
    ])
  })

  it('reads the unread count and marks everything read', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(ok({ unread: 4 }))
      .mockResolvedValueOnce(ok({ marked: 4 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(fetchUnreadNotificationCount()).resolves.toBe(4)
    await expect(markAllNotificationsRead()).resolves.toBe(4)
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/p/v/notifications/unread-count')
    expect(fetchMock.mock.calls[1]).toEqual([
      '/api/v1/p/v/notifications/read-all',
      { method: 'POST', credentials: 'same-origin' },
    ])
  })

  it("reads every project's unread count from the instance route, keyed by project uid", async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(
      ok({
        projects: [
          { project: 'pj-a', unread: 2 },
          { project: 'pj-b', unread: 120 },
        ],
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await expect(fetchUnreadByProject()).resolves.toEqual({ 'pj-a': 2, 'pj-b': 120 })
    // Instance-level: never under the current project's prefix.
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/notifications/unread-by-project')
  })

  it('opens a notification at its link, or its own page when it has none', () => {
    expect(notificationTarget({ uid: 'nt1', link: '/users' })).toBe('/users')
    expect(notificationTarget({ uid: 'nt1', link: '' })).toBe('/n/nt1')
  })
})
