import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../../auth/AuthContext'
import i18n from '../../i18n'
import { hasAnsweredPushPrompt, recordPushPromptAnswer } from '../../lib/pushPromptAnswer'
import type { User } from '../../services/auth'
import { ToastContext } from '../toast/ToastContext'

import { PushPrompt } from './PushPrompt'

vi.mock('../../pwa/push', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../pwa/push')>()
  return {
    ...actual,
    getPushPermission: vi.fn(),
    getPushState: vi.fn(),
    isIosOutsideInstalledApp: vi.fn(),
    isPushEnabled: vi.fn(),
    isPushSupported: vi.fn(),
    requestPushPermission: vi.fn(),
    subscribeToPush: vi.fn(),
  }
})

const push = await import('../../pwa/push')
const permissionMock = vi.mocked(push.getPushPermission)
const stateMock = vi.mocked(push.getPushState)
const iosMock = vi.mocked(push.isIosOutsideInstalledApp)
const enabledMock = vi.mocked(push.isPushEnabled)
const supportedMock = vi.mocked(push.isPushSupported)
const requestMock = vi.mocked(push.requestPushPermission)
const subscribeMock = vi.mocked(push.subscribeToPush)

/** A signed-in account that has just been welcomed. */
function account(): User {
  return {
    uid: 'u1',
    username: 'anna',
    display_name: 'Anna',
    email: 'anna@example.test',
    role: 'viewer',
    disabled: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    subject_uid: null,
    welcome_seen_at: '2026-09-26T10:00:00Z',
  }
}

/** Mounts the prompt under an authenticated context and a spy toast, as the shell does. */
function renderPrompt() {
  const toast = vi.fn()
  const auth = {
    status: 'authenticated',
    user: account(),
    role: 'viewer',
  } as unknown as AuthContextValue
  render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth}>
        <ToastContext.Provider value={{ show: toast }}>
          <PushPrompt />
        </ToastContext.Provider>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
  return { toast }
}

/** The open prompt. It is a portal, so every assertion is scoped inside it. */
async function dialog() {
  return within(await screen.findByRole('dialog', { name: /notifications/i }))
}

/** Waits until the prompt has decided (and, in these cases, declined) to appear. */
async function expectNoPrompt() {
  // Let every pending promise of the decision settle before looking.
  await new Promise((resolve) => setTimeout(resolve, 0))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
}

describe('PushPrompt', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
    window.localStorage.clear()
    supportedMock.mockReturnValue(true)
    iosMock.mockReturnValue(false)
    permissionMock.mockReturnValue('default')
    stateMock.mockResolvedValue({ status: 'prompt', endpoint: null })
    enabledMock.mockResolvedValue(true)
    requestMock.mockResolvedValue('granted')
    subscribeMock.mockResolvedValue({ status: 'subscribed', endpoint: 'https://push.test/e' })
  })

  it('asks a freshly signed-in account, without requesting permission on its own', async () => {
    renderPrompt()

    const modal = await dialog()
    expect(modal.getByText(/when somebody tags you in a photo/)).toBeInTheDocument()
    expect(modal.getByRole('button', { name: 'Not now' })).toBeInTheDocument()
    expect(modal.getByRole('button', { name: 'Turn on notifications' })).toBeInTheDocument()
    expect(requestMock).not.toHaveBeenCalled()
    expect(subscribeMock).not.toHaveBeenCalled()
  })

  it('asks in Czech by default', async () => {
    await i18n.changeLanguage('cs')
    renderPrompt()

    const modal = within(await screen.findByRole('dialog', { name: /oznámení/i }))
    expect(modal.getByRole('button', { name: 'Zapnout oznámení' })).toBeInTheDocument()
    expect(modal.getByRole('button', { name: 'Teď ne' })).toBeInTheDocument()
  })

  it('also asks a browser that already granted permission but holds no subscription', async () => {
    permissionMock.mockReturnValue('granted')
    stateMock.mockResolvedValue({ status: 'unsubscribed', endpoint: null })
    renderPrompt()

    await dialog()
  })

  it('does not ask an account that already answered in this browser', async () => {
    recordPushPromptAnswer('u1')
    renderPrompt()

    await expectNoPrompt()
    expect(stateMock).not.toHaveBeenCalled()
    expect(enabledMock).not.toHaveBeenCalled()
  })

  it('does not ask when push is off instance-wide', async () => {
    enabledMock.mockResolvedValue(false)
    renderPrompt()

    await waitFor(() => {
      expect(enabledMock).toHaveBeenCalled()
    })
    await expectNoPrompt()
    expect(hasAnsweredPushPrompt('u1')).toBe(false)
  })

  it('does not ask a browser that cannot push', async () => {
    supportedMock.mockReturnValue(false)
    renderPrompt()

    await expectNoPrompt()
    expect(enabledMock).not.toHaveBeenCalled()
  })

  it('does not ask on iOS outside an installed home-screen app', async () => {
    iosMock.mockReturnValue(true)
    renderPrompt()

    await expectNoPrompt()
    expect(enabledMock).not.toHaveBeenCalled()
  })

  it('does not ask a browser that already blocked the site', async () => {
    permissionMock.mockReturnValue('denied')
    renderPrompt()

    await expectNoPrompt()
    expect(stateMock).not.toHaveBeenCalled()
  })

  it.each(['subscribed', 'no-worker', 'failed'] as const)(
    'does not ask a browser whose push state is %s',
    async (status) => {
      stateMock.mockResolvedValue({ status, endpoint: null })
      renderPrompt()

      await waitFor(() => {
        expect(stateMock).toHaveBeenCalled()
      })
      await expectNoPrompt()
      expect(enabledMock).not.toHaveBeenCalled()
    },
  )

  it('"not now" records the answer and closes', async () => {
    const user = userEvent.setup()
    renderPrompt()

    await user.click((await dialog()).getByRole('button', { name: 'Not now' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(hasAnsweredPushPrompt('u1')).toBe(true)
    expect(requestMock).not.toHaveBeenCalled()
  })

  it('closing with the ✕ records the answer too', async () => {
    const user = userEvent.setup()
    renderPrompt()

    await user.click(
      (await dialog()).getByRole('button', { name: 'Close the notification question' }),
    )

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(hasAnsweredPushPrompt('u1')).toBe(true)
  })

  it('"turn on" requests permission, subscribes, confirms and records', async () => {
    const user = userEvent.setup()
    const { toast } = renderPrompt()

    await user.click((await dialog()).getByRole('button', { name: 'Turn on notifications' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(requestMock).toHaveBeenCalledTimes(1)
    expect(subscribeMock).toHaveBeenCalledTimes(1)
    expect(requestMock.mock.invocationCallOrder[0]).toBeLessThan(
      subscribeMock.mock.invocationCallOrder[0],
    )
    expect(toast).toHaveBeenCalledWith({ message: 'Notifications are on.', variant: 'success' })
    expect(hasAnsweredPushPrompt('u1')).toBe(true)
  })

  it('a browser-level denial says it is blocked and where to re-allow it, and records', async () => {
    requestMock.mockResolvedValue('denied')
    const user = userEvent.setup()
    const { toast } = renderPrompt()

    const modal = await dialog()
    await user.click(modal.getByRole('button', { name: 'Turn on notifications' }))

    expect(await modal.findByRole('alert')).toHaveTextContent(
      /browser has blocked notifications.*cannot ask again.*site settings/,
    )
    expect(subscribeMock).not.toHaveBeenCalled()
    expect(toast).not.toHaveBeenCalled()
    expect(hasAnsweredPushPrompt('u1')).toBe(true)
    // No button that would only fail again: the one way on is to close.
    expect(modal.queryByRole('button', { name: 'Turn on notifications' })).not.toBeInTheDocument()

    await user.click(modal.getByRole('button', { name: 'Got it' }))
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })

  it('a dismissed browser prompt counts as "not now"', async () => {
    requestMock.mockResolvedValue('default')
    const user = userEvent.setup()
    renderPrompt()

    await user.click((await dialog()).getByRole('button', { name: 'Turn on notifications' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(subscribeMock).not.toHaveBeenCalled()
    expect(hasAnsweredPushPrompt('u1')).toBe(true)
  })

  it('a failed subscription says so and records', async () => {
    subscribeMock.mockResolvedValue({ status: 'failed', endpoint: null })
    const user = userEvent.setup()
    const { toast } = renderPrompt()

    const modal = await dialog()
    await user.click(modal.getByRole('button', { name: 'Turn on notifications' }))

    expect(await modal.findByRole('alert')).toHaveTextContent(/could not be turned on/)
    expect(toast).not.toHaveBeenCalled()
    expect(hasAnsweredPushPrompt('u1')).toBe(true)
  })
})
