import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useTranslation } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { writeLanguagePreference } from '../i18n/accountLanguage'
import { ApiError, NetworkError, type AuthSession } from '../services/auth'
import { PasskeyError } from '../services/passkeys'

import { useAuth } from './AuthContext'
import { AuthProvider } from './AuthProvider'

const fetchMe = vi.fn<() => Promise<AuthSession | null>>()
const signIn = vi.fn<() => Promise<AuthSession>>()

const signOut = vi.fn<() => Promise<void>>()

vi.mock('../services/auth', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/auth')>()
  return { ...actual, fetchMe: () => fetchMe(), logout: () => signOut() }
})

vi.mock('../services/passkeys', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/passkeys')>()
  return { ...actual, signInWithPasskey: () => signIn() }
})

const SESSION: AuthSession = {
  user: {
    uid: 'u1',
    username: 'alice',
    display_name: 'Alice',
    email: 'alice@example.com',
    role: 'editor',
    disabled: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  },
  download_token: 'tok-123',
}

/** Prints the resolved status, and offers the retry the offline page uses. */
function StatusProbe() {
  const { status, refresh } = useAuth()
  return (
    <div>
      <span data-testid="status">{status}</span>
      <button
        onClick={() => {
          void refresh()
        }}
      >
        retry
      </button>
    </div>
  )
}

function renderProvider() {
  return render(
    <AuthProvider>
      <StatusProbe />
    </AuthProvider>,
  )
}

// No mock reset here on purpose: `restoreMocks: true` (vite.config.ts) already
// restores before each test, and resetting afterwards races RTL cleanup — see
// src/test/setup.ts. Every test below sets its own implementation first thing.
describe('AuthProvider session probe', () => {
  it('reports a signed-in visitor as authenticated', async () => {
    fetchMe.mockResolvedValue(SESSION)

    renderProvider()

    expect(await screen.findByTestId('status')).toHaveTextContent('authenticated')
  })

  it('reports a 401 (no session) as unauthenticated', async () => {
    // fetchMe turns the backend's 401 into a null session — a real answer.
    fetchMe.mockResolvedValue(null)

    renderProvider()

    expect(await screen.findByTestId('status')).toHaveTextContent('unauthenticated')
  })

  it('reports an unreachable backend as unreachable, not as signed out', async () => {
    // The whole defect in one assertion: the installed app cold-launching with
    // no network used to land here and call the reader signed out, which sent
    // them to a login form that then blamed their password for the outage.
    fetchMe.mockRejectedValue(new NetworkError('Failed to fetch'))

    renderProvider()

    expect(await screen.findByTestId('status')).toHaveTextContent('unreachable')
  })

  it('still falls back to signed out when the server itself answered badly', async () => {
    // A 500 came *from* the server, so the difference is not knowable here and
    // the old recover-by-signing-out behaviour stands.
    fetchMe.mockRejectedValue(new ApiError(500, 'boom'))

    renderProvider()

    expect(await screen.findByTestId('status')).toHaveTextContent('unauthenticated')
  })

  it('recovers on retry once the backend answers again', async () => {
    const user = userEvent.setup()
    fetchMe.mockRejectedValueOnce(new NetworkError('Failed to fetch')).mockResolvedValue(SESSION)

    renderProvider()
    expect(await screen.findByTestId('status')).toHaveTextContent('unreachable')

    await user.click(screen.getByRole('button', { name: 'retry' }))

    expect(await screen.findByTestId('status')).toHaveTextContent('authenticated')
  })

  it('keeps refresh() from rejecting when the backend is still unreachable', async () => {
    // The offline page awaits refresh() to clear its spinner; a rejection there
    // would be an unhandled one, since there is nothing useful to catch.
    const user = userEvent.setup()
    fetchMe.mockRejectedValue(new NetworkError('Failed to fetch'))
    const unhandled = vi.fn()
    window.addEventListener('unhandledrejection', unhandled)

    renderProvider()
    expect(await screen.findByTestId('status')).toHaveTextContent('unreachable')
    await user.click(screen.getByRole('button', { name: 'retry' }))

    expect(unhandled).not.toHaveBeenCalled()
    expect(screen.getByTestId('status')).toHaveTextContent('unreachable')
    window.removeEventListener('unhandledrejection', unhandled)
  })
})

/** Signs in with a passkey and prints who ended up in the context. */
function PasskeyProbe() {
  const { status, user, loginWithPasskey } = useAuth()
  return (
    <div>
      <span data-testid="status">{status}</span>
      <span data-testid="user">{user?.username ?? '-'}</span>
      <button
        onClick={() => {
          // Swallowed here as `LoginPage` swallows it: a refused ceremony is a
          // message on the form, not an unhandled rejection.
          void loginWithPasskey().catch(() => undefined)
        }}
      >
        passkey
      </button>
    </div>
  )
}

describe('AuthProvider passkey sign-in', () => {
  it('publishes the ceremony session exactly as a password login does', async () => {
    // Nothing downstream may be able to tell the two ways in apart: the passkey
    // endpoint returns the same AuthSession, so it is applied the same way.
    const user = userEvent.setup()
    fetchMe.mockResolvedValue(null)
    signIn.mockResolvedValue(SESSION)
    render(
      <AuthProvider>
        <PasskeyProbe />
      </AuthProvider>,
    )
    expect(await screen.findByTestId('status')).toHaveTextContent('unauthenticated')

    await user.click(screen.getByRole('button', { name: 'passkey' }))

    expect(await screen.findByTestId('status')).toHaveTextContent('authenticated')
    expect(screen.getByTestId('user')).toHaveTextContent('alice')
  })

  it('leaves the session alone when the ceremony is refused', async () => {
    const user = userEvent.setup()
    fetchMe.mockResolvedValue(null)
    signIn.mockRejectedValue(new PasskeyError('cancelled'))
    render(
      <AuthProvider>
        <PasskeyProbe />
      </AuthProvider>,
    )
    expect(await screen.findByTestId('status')).toHaveTextContent('unauthenticated')

    await user.click(screen.getByRole('button', { name: 'passkey' }))

    expect(screen.getByTestId('status')).toHaveTextContent('unauthenticated')
    expect(screen.getByTestId('user')).toHaveTextContent('-')
  })
})

/** Signs out, and prints the status and the language the UI ended up in. */
function SignOutProbe() {
  const { status, logout } = useAuth()
  // Through react-i18next, so a language switch re-renders the probe.
  const { i18n: active } = useTranslation()
  return (
    <div>
      <span data-testid="status">{status}</span>
      <span data-testid="lang">{active.language}</span>
      <button
        onClick={() => {
          void logout().catch(() => undefined)
        }}
      >
        sign out
      </button>
    </div>
  )
}

describe('AuthProvider browser state', () => {
  beforeEach(async () => {
    window.localStorage.clear()
    window.sessionStorage.clear()
    await i18n.changeLanguage('cs')
  })

  it('clears what the account left in storage on sign-out, and keeps the device preferences', async () => {
    const user = userEvent.setup()
    fetchMe.mockResolvedValue(SESSION)
    signOut.mockResolvedValue(undefined)
    window.localStorage.setItem('kukatko.announcement.dismissedAt.u1', 't1')
    window.localStorage.setItem('kukatko.review.daily.u1', '2026-09-27')
    window.sessionStorage.setItem('kukatko.gridScroll.u1', '{}')
    window.localStorage.setItem('kukatko.grid.density', '"large"')
    render(
      <AuthProvider>
        <SignOutProbe />
      </AuthProvider>,
    )
    expect(await screen.findByTestId('status')).toHaveTextContent('authenticated')

    await user.click(screen.getByRole('button', { name: 'sign out' }))

    expect(await screen.findByTestId('status')).toHaveTextContent('unauthenticated')
    expect(window.localStorage.getItem('kukatko.announcement.dismissedAt.u1')).toBeNull()
    expect(window.localStorage.getItem('kukatko.review.daily.u1')).toBeNull()
    expect(window.sessionStorage.getItem('kukatko.gridScroll.u1')).toBeNull()
    expect(window.localStorage.getItem('kukatko.grid.density')).toBe('"large"')
  })

  it('clears it even when the server never heard the sign-out', async () => {
    const user = userEvent.setup()
    fetchMe.mockResolvedValue(SESSION)
    signOut.mockRejectedValue(new NetworkError('Failed to fetch'))
    window.localStorage.setItem('kukatko.announcement.dismissedAt.u1', 't1')
    render(
      <AuthProvider>
        <SignOutProbe />
      </AuthProvider>,
    )
    expect(await screen.findByTestId('status')).toHaveTextContent('authenticated')

    await user.click(screen.getByRole('button', { name: 'sign out' }))

    expect(await screen.findByTestId('status')).toHaveTextContent('unauthenticated')
    expect(window.localStorage.getItem('kukatko.announcement.dismissedAt.u1')).toBeNull()
  })

  it('opens in the signed-in account’s language and returns to Czech on sign-out', async () => {
    const user = userEvent.setup()
    fetchMe.mockResolvedValue(SESSION)
    signOut.mockResolvedValue(undefined)
    writeLanguagePreference('u1', 'en')
    render(
      <AuthProvider>
        <SignOutProbe />
      </AuthProvider>,
    )

    await waitFor(() => {
      expect(screen.getByTestId('lang')).toHaveTextContent('en')
    })

    await user.click(screen.getByRole('button', { name: 'sign out' }))

    await waitFor(() => {
      expect(screen.getByTestId('lang')).toHaveTextContent('cs')
    })
    // The choice is the account's and outlives the session.
    expect(window.localStorage.getItem('kukatko.language.u1')).toBe('en')
  })

  it('does not hand one account’s language to the next', async () => {
    fetchMe.mockResolvedValue(SESSION)
    // Another account chose English in this browser, and the UI is still in it.
    writeLanguagePreference('u2', 'en')
    await i18n.changeLanguage('en')
    render(
      <AuthProvider>
        <SignOutProbe />
      </AuthProvider>,
    )

    expect(await screen.findByTestId('status')).toHaveTextContent('authenticated')
    await waitFor(() => {
      expect(screen.getByTestId('lang')).toHaveTextContent('cs')
    })
  })
})
