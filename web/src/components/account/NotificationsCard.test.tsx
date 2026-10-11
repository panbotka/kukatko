import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../../auth/AuthContext'
import i18n from '../../i18n'
import { hasAnsweredPushPrompt } from '../../lib/pushPromptAnswer'
import type { Role } from '../../services/auth'
import type { NotificationPreference, PushSubscriptionRecord } from '../../services/notifications'

import { isKindOffered, isReachableEmail, kindRows } from './notificationKinds'
import { NotificationsCard } from './NotificationsCard'

vi.mock('../../hooks/useMailEnabled', () => ({ useMailEnabled: vi.fn() }))

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

const { useMailEnabled } = await import('../../hooks/useMailEnabled')
const mailEnabledMock = vi.mocked(useMailEnabled)

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

/**
 * A mixed set the card has to render: some pairs on, a kind missing a channel.
 * Not today's server defaults — those are {@link ALL_OFF_PREFS}.
 */
const DEFAULT_PREFS: NotificationPreference[] = [
  { kind: 'tagged', channel: 'push', enabled: true, is_default: true },
  { kind: 'registration_pending', channel: 'push', enabled: true, is_default: true },
  { kind: 'registration_pending', channel: 'email', enabled: false, is_default: true },
]

/** What the server reports for a fresh account: every kind on every channel, all off. */
const ALL_OFF_PREFS: NotificationPreference[] = ['tagged', 'registration_pending'].flatMap((kind) =>
  ['in_app', 'push', 'email'].map((channel) => ({
    kind,
    channel,
    enabled: false,
    is_default: true,
  })),
)

/** The accessible name of one kind's switch on one channel. */
const TAGGED_PUSH = 'When somebody tags me in a photo — Push'
const REG_PUSH = 'When a new registration is waiting for approval — Push'
const REG_EMAIL = 'When a new registration is waiting for approval — E-mail'

/** Mounts the card for an account of `role` with address `email`. */
function renderCard(role: Role = 'viewer', email = 'anna@example.com') {
  const auth = {
    status: 'authenticated',
    user: { uid: 'u1', username: 'anna', role, email },
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
  await screen.findByRole('heading', { name: 'What to notify me about' })
  await waitFor(() => {
    expect(document.querySelector('.spinner-border')).toBeNull()
  })
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  window.localStorage.clear()
  enabledMock.mockResolvedValue(true)
  mailEnabledMock.mockReturnValue(true)
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
        const stored = input.find(
          (entry) => entry.kind === pref.kind && entry.channel === pref.channel,
        )
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
    expect(screen.getByRole('checkbox', { name: TAGGED_PUSH })).toBeVisible()
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
      screen.getByText(/belong to your account: in-app ones show up under the bell/),
    ).toBeInTheDocument()
  })
})

describe('NotificationsCard — push off instance-wide', () => {
  it('says so in the device part and disables the push column, keeping e-mail', async () => {
    enabledMock.mockResolvedValue(false)
    renderCard('admin')
    await loaded()

    expect(
      screen.getByText(/Push notifications are not switched on for this instance, so/),
    ).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Turn on/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Devices receiving notifications' })).toBeNull()
    expect(devicesMock).not.toHaveBeenCalled()

    const taggedPush = screen.getByRole('checkbox', { name: TAGGED_PUSH })
    expect(taggedPush).toBeDisabled()
    expect(taggedPush.closest('[title]')).toHaveAttribute(
      'title',
      'Push notifications are not switched on for this instance.',
    )
    expect(screen.getByRole('checkbox', { name: REG_EMAIL })).toBeEnabled()
  })
})

describe('NotificationsCard — kinds × channels', () => {
  it('renders one row per kind with a switch per channel it is delivered through', async () => {
    renderCard('admin')
    await loaded()

    expect(screen.getByRole('checkbox', { name: TAGGED_PUSH })).toBeChecked()
    // Tagged is push only: no e-mail switch, an empty cell instead.
    expect(
      screen.queryByRole('checkbox', { name: 'When somebody tags me in a photo — E-mail' }),
    ).not.toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: REG_PUSH })).toBeChecked()
    expect(screen.getByRole('checkbox', { name: REG_EMAIL })).not.toBeChecked()
    expect(screen.getByRole('checkbox', { name: REG_EMAIL })).toBeEnabled()
    expect(
      screen.getByText('Everything is off by default — turn on only what you want.'),
    ).toBeInTheDocument()
  })

  it('saves a push toggle at once, keeping the other kinds on their defaults', async () => {
    const user = userEvent.setup()
    renderCard()
    await loaded()

    const tagged = screen.getByRole('checkbox', { name: TAGGED_PUSH })
    expect(tagged).toBeChecked()

    await user.click(tagged)

    expect(savePrefsMock).toHaveBeenCalledWith([
      { kind: 'tagged', channel: 'push', enabled: false },
    ])
    await waitFor(() => {
      expect(tagged).not.toBeChecked()
    })
  })

  it('saves an e-mail toggle with its channel, sending the stored pairs along', async () => {
    const user = userEvent.setup()
    prefsMock.mockResolvedValue([
      { kind: 'tagged', channel: 'push', enabled: false, is_default: false },
      DEFAULT_PREFS[1],
      DEFAULT_PREFS[2],
    ])
    renderCard('admin')
    await loaded()

    const email = screen.getByRole('checkbox', { name: REG_EMAIL })
    await user.click(email)

    expect(savePrefsMock).toHaveBeenCalledWith([
      { kind: 'tagged', channel: 'push', enabled: false },
      { kind: 'registration_pending', channel: 'email', enabled: true },
    ])
    await waitFor(() => {
      expect(email).toBeChecked()
    })
    expect(screen.getByRole('checkbox', { name: REG_PUSH })).toBeChecked()
  })

  it('puts the choice back and says so when the save fails', async () => {
    const user = userEvent.setup()
    savePrefsMock.mockRejectedValue(new Error('offline'))
    renderCard('admin')
    await loaded()

    const email = screen.getByRole('checkbox', { name: REG_EMAIL })
    await user.click(email)

    expect(await screen.findByText(/could not be saved/)).toBeInTheDocument()
    await waitFor(() => {
      expect(email).not.toBeChecked()
    })
    expect(email).toBeEnabled()
  })

  it('disables the e-mail column with the reason when the instance sends no mail', async () => {
    mailEnabledMock.mockReturnValue(false)
    renderCard('admin')
    await loaded()

    const email = screen.getByRole('checkbox', { name: REG_EMAIL })
    expect(email).toBeDisabled()
    expect(email.closest('[title]')).toHaveAttribute(
      'title',
      'E-mails are not switched on for this instance.',
    )
    expect(email).toHaveAccessibleDescription('E-mails are not switched on for this instance.')
    expect(screen.getByRole('checkbox', { name: REG_PUSH })).toBeEnabled()
  })

  it('disables the e-mail column for an account with a placeholder address', async () => {
    renderCard('admin', 'admin@kukatko.invalid')
    await loaded()

    const email = screen.getByRole('checkbox', { name: REG_EMAIL })
    expect(email).toBeDisabled()
    expect(email.closest('[title]')).toHaveAttribute(
      'title',
      'Add an e-mail address to your account.',
    )
  })

  it('hides the registration kind from a viewer', async () => {
    mailEnabledMock.mockReturnValue(false)
    renderCard('viewer')
    await loaded()

    expect(screen.getByRole('checkbox', { name: TAGGED_PUSH })).toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: /registration/ })).not.toBeInTheDocument()
    // No e-mail switch on the page at all, so no e-mail reason under the grid either.
    expect(screen.queryByText(/E-mails are not switched on/)).not.toBeInTheDocument()
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

  it('groups the preferences into rows, skipping unknown kinds', () => {
    const rows = kindRows(
      [
        ...DEFAULT_PREFS,
        { kind: 'something_new', channel: 'push', enabled: true, is_default: true },
      ],
      'admin',
    )
    expect(rows.map((row) => row.kind)).toEqual(['tagged', 'registration_pending'])
    expect([...rows[0].byChannel.keys()]).toEqual(['push'])
    expect([...rows[1].byChannel.keys()]).toEqual(['push', 'email'])
  })

  it.each<[string | undefined, boolean]>([
    ['anna@example.com', true],
    ['', false],
    ['  ', false],
    [undefined, false],
    ['admin@kukatko.invalid', false],
  ])('treats %j as a reachable address: %s', (email, reachable) => {
    expect(isReachableEmail(email)).toBe(reachable)
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

    expect(await screen.findByRole('heading', { name: 'Notifikace' })).toBeInTheDocument()
    expect(
      await screen.findByRole('checkbox', { name: 'Když mě někdo označí na fotce — Push' }),
    ).toBeInTheDocument()
    expect(
      await screen.findByRole('button', { name: 'Zapnout v tomhle prohlížeči' }),
    ).toBeInTheDocument()
    expect(await screen.findByText('Chrome na Androidu')).toBeInTheDocument()
  })
})

describe('NotificationsCard — the in-app column', () => {
  const TAGGED_IN_APP = 'When somebody tags me in a photo — In-app'

  it('offers in-app first, off by default, for every kind the role gets', async () => {
    prefsMock.mockResolvedValue(ALL_OFF_PREFS)
    renderCard('admin')
    await loaded()

    const heads = [...document.querySelectorAll('.kk-notify-grid__channel-head')].map(
      (head) => head.textContent,
    )
    expect(heads).toEqual(['In-app', 'Push', 'E-mail'])
    for (const name of [
      TAGGED_IN_APP,
      'When a new registration is waiting for approval — In-app',
    ]) {
      expect(screen.getByRole('checkbox', { name })).not.toBeChecked()
    }
    expect(
      screen.getByText('Everything is off by default — turn on only what you want.'),
    ).toBeInTheDocument()
  })

  it('stays usable with push and mail both off for the instance, and saves its channel', async () => {
    const user = userEvent.setup()
    enabledMock.mockResolvedValue(false)
    mailEnabledMock.mockReturnValue(false)
    prefsMock.mockResolvedValue(ALL_OFF_PREFS)
    renderCard()
    await loaded()

    const inApp = screen.getByRole('checkbox', { name: TAGGED_IN_APP })
    expect(inApp).toBeEnabled()
    expect(screen.getByRole('checkbox', { name: TAGGED_PUSH })).toBeDisabled()
    expect(inApp).not.toHaveAttribute('aria-describedby')

    await user.click(inApp)
    expect(savePrefsMock).toHaveBeenCalledWith([
      { kind: 'tagged', channel: 'in_app', enabled: true },
    ])
  })
})
