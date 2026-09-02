import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'

import { OfflinePage } from './OfflinePage'

function authValue(overrides: Partial<AuthContextValue> = {}): AuthContextValue {
  return {
    status: 'unreachable',
    user: null,
    role: null,
    downloadToken: null,
    canWrite: false,
    isAdmin: false,
    isMaintainer: false,
    canImport: false,
    login: vi.fn(),
    loginWithPasskey: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  }
}

function renderOffline(value: AuthContextValue) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={value}>
        <OfflinePage />
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

afterEach(async () => {
  // Czech is the instance default; restore it for whoever runs next.
  await i18n.changeLanguage('cs')
})

describe('OfflinePage', () => {
  it('names the connection as the problem, not the session', () => {
    renderOffline(authValue())

    expect(screen.getByRole('heading')).toHaveTextContent(/cannot reach the server/i)
    // Careful wording: not "you are still signed in" (nobody checked, so nobody
    // knows) but "nothing has ended your session", which holds either way.
    expect(screen.getByTestId('offline-page')).toHaveTextContent(/nothing has ended it/i)
  })

  it('says the library needs a connection, matching what the cache actually holds', () => {
    // The service worker caches the app shell and nothing under /api/, so the
    // page must not imply the photos are here.
    renderOffline(authValue())

    expect(screen.getByTestId('offline-page')).toHaveTextContent(
      /photos and library data have to come from the server/i,
    )
  })

  it('re-reads the session when the reader tries again', async () => {
    const user = userEvent.setup()
    const refresh = vi.fn().mockResolvedValue(undefined)
    renderOffline(authValue({ refresh }))

    await user.click(screen.getByRole('button', { name: 'Try again' }))

    expect(refresh).toHaveBeenCalledTimes(1)
  })

  it('frees the button again when the retry finds the backend still unreachable', async () => {
    const user = userEvent.setup()
    // A retry that resolves without changing the status: the guard keeps this
    // page mounted, so the button has to come back.
    renderOffline(authValue())

    await user.click(screen.getByRole('button', { name: 'Try again' }))

    const button = await screen.findByRole('button', { name: 'Try again' })
    expect(button).toBeEnabled()
  })
})
