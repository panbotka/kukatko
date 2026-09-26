import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../../auth/AuthContext'
import i18n from '../../i18n'
import { hasAnsweredPushPrompt } from '../../lib/pushPromptAnswer'
import type { Role } from '../../services/auth'
import type { NotificationPreference, PushSubscriptionRecord } from '../../services/notifications'

import { isKindOffered, NotificationsCard } from './NotificationsCard'

vi.mock('../../pwa/push', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../pwa/push')>()
  return {
    ...actual,
    getPushPermission: vi.fn(),
    getPushState: vi.fn(),
    isIosOutsideInstalledApp: vi.fn(),
    isPushEnabled: vi.fn(),
    requestPushPermission: vi.fn(),
    subscribeToPush: vi.fn(),
    unsubscribeFromPush: vi.fn(),
  }
})

vi.mock('../../services/notifications', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/notifications')>()
  return {
    ...actual,
    fetchPushSubscriptions: vi.fn(),
    deletePushSubscription: vi.fn(),
    fetchNotificationPreferences: vi.fn(),
    updateNotificationPreferences: vi.fn(),
  }
})

const push = await import('../../pwa/push')
const permissionMock = vi.mocked(push.getPushPermission)
const stateMock = vi.mocked(push.getPushState)
const iosMock = vi.mocked(push.isIosOutsideInstalledApp)
const enabledMock = vi.mocked(push.isPushEnabled)
const requestMock = vi.mocked(push.requestPushPermission)
const subscribeMock = vi.mocked(push.subscribeToPush)
const unsubscribeMock = vi.mocked(push.unsubscribeFromPush)

const api = await import('../../services/notifications')
const devicesMock = vi.mocked(api.fetchPushSubscriptions)
const deleteMock = vi.mocked(api.deletePushSubscription)
const prefsMock = vi.mocked(api.fetchNotificationPreferences)
const savePrefsMock = vi.mocked(api.updateNotificationPreferences)

const THIS_ENDPOINT = 'https://push.test/this'
const PHONE_ENDPOINT = 'https://push.test/phone'

/** This browser, as the server lists it. */
const THIS_DEVICE: PushSubscriptionRecord = {
  id: 's1',
  endpoint: THIS_ENDPOINT,
  user_agent: 'Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0',
  created_at: '2026-09-20T10:00:00Z',
  last_used_at: null,
}

/** Another device of the same account. */
const PHONE: PushSubscriptionRecord = {
  id: 's2',
  endpoint: PHONE_ENDPOINT,
  user_agent:
    'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Mobile Safari/537.36',
  created_at: '2026-09-01T10:00:00Z',
  last_used_at: '2026-09-25T18:30:00Z',
}

/** Both kinds on their defaults. */
const DEFAULT_PREFS: NotificationPreference[] = [
  { kind: 'tagged', enabled: true, is_default: true },
  { kind: 'registration_pending', enabled: true, is_default: true },
]

/** Mounts the card for an account of `role`. */
function renderCard(role: Role = 'viewer') {
  const auth = {
    status: 'authenticated',
    user: { uid: 'u1', username: 'anna', role },
    role,
  } as unknown as AuthContextValue
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth}>
        <NotificationsCard />
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/** Waits until the card has read the browser, the choices and the devices. */
async function loaded() {
  await screen.findByRole('heading', { name: 'Devices receiving notifications' })
  await waitFor(() => {
    expect(document.querySelector('.spinner-border')).toBeNull()
  })
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  window.localStorage.clear()
  enabledMock.mockResolvedValue(true)
  iosMock.mockReturnValue(false)
  permissionMock.mockReturnValue('default')
  stateMock.mockResolvedValue({ status: 'prompt', endpoint: null })
  requestMock.mockResolvedValue('granted')
  subscribeMock.mockResolvedValue({ status: 'subscribed', endpoint: THIS_ENDPOINT })
  unsubscribeMock.mockResolvedValue({ status: 'unsubscribed', endpoint: null })
  prefsMock.mockResolvedValue(DEFAULT_PREFS)
  savePrefsMock.mockImplementation((input) =>
    Promise.resolve(
      DEFAULT_PREFS.map((pref) => {
        const stored = input.find((entry) => entry.kind === pref.kind)
        return stored ? { ...stored, is_default: false } : pref
      }),
    ),
  )
  devicesMock.mockResolvedValue([PHONE])
  deleteMock.mockResolvedValue(undefined)
})

afterEach(async () => {
  await i18n.changeLanguage('en')
})

describe('NotificationsCard — this browser', () => {
  it('turns an off browser on from the click: asks, subscribes, lists it as this browser', async () => {
    const user = userEvent.setup()
    renderCard()
    await loaded()

    expect(screen.getByText('Notifications are off in this browser.')).toBeInTheDocument()
    expect(requestMock).not.toHaveBeenCalled()

    devicesMock.mockResolvedValue([PHONE, THIS_DEVICE])
    await user.click(screen.getByRole('button', { name: 'Turn on in this browser' }))

    expect(await screen.findByText('Notifications are on in this browser.')).toBeInTheDocument()
    expect(requestMock).toHaveBeenCalledTimes(1)
    expect(subscribeMock).toHaveBeenCalledTimes(1)
    // Answering here counts as the one-time prompt's answer too.
    expect(hasAnsweredPushPrompt('u1')).toBe(true)
  })

  it('does not ask again when the permission is already granted', async () => {
    const user = userEvent.setup()
    permissionMock.mockReturnValue('granted')
    stateMock.mockResolvedValue({ status: 'unsubscribed', endpoint: null })
    renderCard()
    await loaded()

    await user.click(screen.getByRole('button', { name: 'Turn on in this browser' }))

    await waitFor(() => {
      expect(subscribeMock).toHaveBeenCalledTimes(1)
    })
    expect(requestMock).not.toHaveBeenCalled()
  })

  it('turns an on browser off, unsubscribing in both places', async () => {
    const user = userEvent.setup()
    stateMock.mockResolvedValue({ status: 'subscribed', endpoint: THIS_ENDPOINT })
    devicesMock.mockResolvedValue([PHONE, THIS_DEVICE])
    renderCard()
    await loaded()

    expect(screen.getByText('Notifications are on in this browser.')).toBeInTheDocument()

    devicesMock.mockResolvedValue([PHONE])
    await user.click(screen.getByRole('button', { name: 'Turn off in this browser' }))

    expect(await screen.findByText('Notifications are off in this browser.')).toBeInTheDocument()
    expect(unsubscribeMock).toHaveBeenCalledTimes(1)
  })

  it('reads as off when the server no longer lists this browser', async () => {
    stateMock.mockResolvedValue({ status: 'subscribed', endpoint: THIS_ENDPOINT })
    devicesMock.mockResolvedValue([PHONE])
    renderCard()
    await loaded()

    expect(screen.getByText('Notifications are off in this browser.')).toBeInTheDocument()
  })

  it('explains a browser-level block instead of offering a button', async () => {
    stateMock.mockResolvedValue({ status: 'denied', endpoint: null })
    renderCard()
    await loaded()

    expect(screen.getByRole('alert')).toHaveTextContent(/site settings/)
    expect(screen.queryByRole('button', { name: /Turn on/ })).not.toBeInTheDocument()
    // The account-wide choices still matter for the other devices.
    expect(screen.getByRole('checkbox', { name: 'When somebody tags me in a photo' })).toBeVisible()
  })

  it('shows the blocked explanation when the browser prompt is answered with a block', async () => {
    const user = userEvent.setup()
    requestMock.mockResolvedValue('denied')
    renderCard()
    await loaded()

    await user.click(screen.getByRole('button', { name: 'Turn on in this browser' }))

    expect(await screen.findByText(/site settings/)).toBeInTheDocument()
    expect(subscribeMock).not.toHaveBeenCalled()
    expect(screen.queryByRole('button', { name: /Turn on/ })).not.toBeInTheDocument()
  })

  it('tells iOS outside the installed app to install it', async () => {
    stateMock.mockResolvedValue({ status: 'unsupported', endpoint: null })
    iosMock.mockReturnValue(true)
    renderCard()
    await loaded()

    expect(screen.getByText(/Add to Home Screen/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Turn on/ })).not.toBeInTheDocument()
  })

  it('says the two scopes out loud', async () => {
    renderCard()
    await loaded()

    expect(screen.getByText(/applies only to the browser you are using/)).toBeInTheDocument()
    expect(
      screen.getByText(/belong to your account: they apply to every device/),
    ).toBeInTheDocument()
  })
})

describe('NotificationsCard — push off instance-wide', () => {
  it('says so instead of rendering controls', async () => {
    enabledMock.mockResolvedValue(false)
    renderCard('admin')

    expect(await screen.findByText(/switched off on this Kukátko instance/)).toBeInTheDocument()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
    expect(prefsMock).not.toHaveBeenCalled()
    expect(devicesMock).not.toHaveBeenCalled()
  })
})

describe('NotificationsCard — which messages', () => {
  it('saves a toggle at once, keeping the other kinds on their defaults', async () => {
    const user = userEvent.setup()
    renderCard()
    await loaded()

    const tagged = screen.getByRole('checkbox', { name: 'When somebody tags me in a photo' })
    expect(tagged).toBeChecked()

    await user.click(tagged)

    expect(savePrefsMock).toHaveBeenCalledWith([{ kind: 'tagged', enabled: false }])
    await waitFor(() => {
      expect(tagged).not.toBeChecked()
    })
  })

  it('puts the choice back and says so when the save fails', async () => {
    const user = userEvent.setup()
    savePrefsMock.mockRejectedValue(new Error('offline'))
    renderCard()
    await loaded()

    const tagged = screen.getByRole('checkbox', { name: 'When somebody tags me in a photo' })
    await user.click(tagged)

    expect(await screen.findByText(/could not be saved/)).toBeInTheDocument()
    expect(tagged).toBeChecked()
  })

  it('hides the registration kind from a viewer', async () => {
    renderCard('viewer')
    await loaded()

    expect(screen.getByRole('checkbox', { name: /tags me/ })).toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: /registration/ })).not.toBeInTheDocument()
  })

  it('shows the registration kind to an admin', async () => {
    renderCard('admin')
    await loaded()

    expect(
      screen.getByRole('checkbox', { name: 'When a new registration is waiting for approval' }),
    ).toBeInTheDocument()
  })

  it.each<[Role, boolean]>([
    ['viewer', false],
    ['curator', false],
    ['editor', false],
    ['admin', true],
    ['maintainer', true],
  ])('offers the registration kind to a %s: %s', (role, offered) => {
    expect(isKindOffered('registration_pending', role)).toBe(offered)
    expect(isKindOffered('tagged', role)).toBe(true)
    expect(isKindOffered('something_new', role)).toBe(false)
  })
})

describe('NotificationsCard — devices', () => {
  it('names each device readably and marks the current browser', async () => {
    stateMock.mockResolvedValue({ status: 'subscribed', endpoint: THIS_ENDPOINT })
    devicesMock.mockResolvedValue([PHONE, THIS_DEVICE])
    renderCard()
    await loaded()

    const items = screen.getAllByRole('listitem')
    expect(items).toHaveLength(2)
    expect(within(items[0]).getByText('Chrome on Android')).toBeInTheDocument()
    expect(within(items[0]).queryByText('This browser')).not.toBeInTheDocument()
    expect(within(items[0]).getByText(/Last notification/)).toBeInTheDocument()
    expect(within(items[1]).getByText('Firefox on Linux')).toBeInTheDocument()
    expect(within(items[1]).getByText('This browser')).toBeInTheDocument()
    expect(within(items[1]).getByText(/No notification yet/)).toBeInTheDocument()
    // Never the raw string.
    expect(screen.queryByText(/Mozilla/)).not.toBeInTheDocument()
  })

  it('removes another device after a confirmation', async () => {
    const user = userEvent.setup()
    renderCard()
    await loaded()

    await user.click(screen.getByRole('button', { name: 'Remove Chrome on Android' }))
    const dialog = within(await screen.findByRole('dialog'))
    await user.click(dialog.getByRole('button', { name: 'Remove' }))

    expect(deleteMock).toHaveBeenCalledWith(PHONE_ENDPOINT)
    expect(await screen.findByText('No device receives notifications yet.')).toBeInTheDocument()
    expect(unsubscribeMock).not.toHaveBeenCalled()
  })

  it('removing this browser turns it off in the browser too', async () => {
    const user = userEvent.setup()
    stateMock.mockResolvedValue({ status: 'subscribed', endpoint: THIS_ENDPOINT })
    devicesMock.mockResolvedValue([THIS_DEVICE])
    renderCard()
    await loaded()

    devicesMock.mockResolvedValue([])
    await user.click(screen.getByRole('button', { name: 'Remove Firefox on Linux' }))
    const dialog = within(await screen.findByRole('dialog'))
    await user.click(dialog.getByRole('button', { name: 'Remove' }))

    await waitFor(() => {
      expect(unsubscribeMock).toHaveBeenCalledTimes(1)
    })
    expect(deleteMock).not.toHaveBeenCalled()
    expect(await screen.findByText('Notifications are off in this browser.')).toBeInTheDocument()
  })
})

describe('NotificationsCard — Czech', () => {
  it('speaks Czech by default', async () => {
    await i18n.changeLanguage('cs')
    renderCard()

    expect(await screen.findByRole('heading', { name: 'Oznámení' })).toBeInTheDocument()
    expect(
      await screen.findByRole('button', { name: 'Zapnout v tomhle prohlížeči' }),
    ).toBeInTheDocument()
    expect(await screen.findByText('Chrome na Androidu')).toBeInTheDocument()
  })
})
