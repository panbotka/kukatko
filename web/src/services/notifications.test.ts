import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  type NotificationPreference,
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
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/notifications/preferences', {
      method: 'PUT',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ preferences: input }),
    })
  })
})
