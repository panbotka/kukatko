import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { ApiError, NetworkError } from '../services/auth'

import { LoginPage } from './LoginPage'

function authValue(overrides: Partial<AuthContextValue> = {}): AuthContextValue {
  return {
    status: 'unauthenticated',
    user: null,
    role: null,
    downloadToken: null,
    canWrite: false,
    isAdmin: false,
    isMaintainer: false,
    canImport: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
    ...overrides,
  }
}

/** Surfaces where login sent the user, query string included. */
function LocationProbe() {
  const { pathname, search } = useLocation()
  return <span data-testid="location">{`${pathname}${search}`}</span>
}

/**
 * Mounts the login page. `state` is what the route guard stashes when it bounces
 * a visitor here — the location they actually asked for.
 */
function renderLogin(
  value: AuthContextValue,
  state?: { from: { pathname: string; search: string } },
) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={value}>
        <MemoryRouter initialEntries={[{ pathname: '/login', state }]}>
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route path="/" element={<div>home page</div>} />
            <Route path="/share-target" element={<div>share page</div>} />
          </Routes>
          <LocationProbe />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

describe('LoginPage', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
  })

  it('does not call login when fields are empty (client-side validation)', async () => {
    const user = userEvent.setup()
    const value = authValue()
    renderLogin(value)

    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(value.login).not.toHaveBeenCalled()
  })

  it('submits trimmed credentials when fields are filled', async () => {
    const user = userEvent.setup()
    const login = vi.fn().mockResolvedValue(undefined)
    renderLogin(authValue({ login }))

    await user.type(screen.getByLabelText('Username'), '  alice  ')
    await user.type(screen.getByLabelText('Password'), 'secret')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(login).toHaveBeenCalledWith('alice', 'secret')
  })

  it('renders the invalid-credentials message on a 401', async () => {
    const user = userEvent.setup()
    const login = vi.fn().mockRejectedValue(new ApiError(401, 'invalid username or password'))
    renderLogin(authValue({ login }))

    await user.type(screen.getByLabelText('Username'), 'alice')
    await user.type(screen.getByLabelText('Password'), 'wrong')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Invalid username or password.')
  })

  it('returns to the requested address with its query string intact', async () => {
    // The query is not decoration: a share from the phone names the files
    // waiting in the cache with it, so dropping it loses the photos.
    const user = userEvent.setup()
    const login = vi.fn().mockResolvedValue(undefined)
    renderLogin(authValue({ login }), {
      from: { pathname: '/share-target', search: '?share=1700000000000-1' },
    })

    await user.type(screen.getByLabelText('Username'), 'alice')
    await user.type(screen.getByLabelText('Password'), 'secret')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByText('share page')).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('/share-target?share=1700000000000-1')
  })

  it('sends an already-signed-in visitor onward, query string and all', async () => {
    renderLogin(authValue({ status: 'authenticated' }), {
      from: { pathname: '/share-target', search: '?share=abc' },
    })

    expect(await screen.findByText('share page')).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('/share-target?share=abc')
  })

  it('falls back to the library when the guard stashed nothing', async () => {
    const user = userEvent.setup()
    const login = vi.fn().mockResolvedValue(undefined)
    renderLogin(authValue({ login }))

    await user.type(screen.getByLabelText('Username'), 'alice')
    await user.type(screen.getByLabelText('Password'), 'secret')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByText('home page')).toBeInTheDocument()
  })

  it('renders the rate-limited message on a 429', async () => {
    const user = userEvent.setup()
    const login = vi.fn().mockRejectedValue(new ApiError(429, 'too many'))
    renderLogin(authValue({ login }))

    await user.type(screen.getByLabelText('Username'), 'alice')
    await user.type(screen.getByLabelText('Password'), 'secret')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/too many login attempts/i)
  })

  it('blames the connection, not the password, when the request never left the device', async () => {
    // A phone with no signal: fetch rejects, nothing judged the credentials. The
    // generic "sign in failed, please try again" used to land here and send the
    // reader off to retype a password they had not forgotten.
    const user = userEvent.setup()
    const login = vi.fn().mockRejectedValue(new NetworkError('Failed to fetch'))
    renderLogin(authValue({ login }))

    await user.type(screen.getByLabelText('Username'), 'alice')
    await user.type(screen.getByLabelText('Password'), 'secret')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/could not reach the server/i)
    expect(alert).toHaveTextContent(/your password is probably fine/i)
    // Emphatically not the credentials message, and not the generic one.
    expect(alert).not.toHaveTextContent(/invalid username or password/i)
    expect(alert).not.toHaveTextContent(/sign in failed/i)
  })

  it('warns up front when the session probe already found the backend unreachable', async () => {
    renderLogin(authValue({ status: 'unreachable' }))

    expect(await screen.findByTestId('login-offline-notice')).toHaveTextContent(
      /cannot reach the server right now/i,
    )
  })

  it('keeps its hands off the keyboard while the backend is unreachable', () => {
    // autoFocus raises the phone's keyboard. Doing that for a form that cannot
    // succeed is an invitation to type a password nothing will check.
    const { unmount } = renderLogin(authValue({ status: 'unreachable' }))
    expect(screen.getByLabelText('Username')).not.toHaveFocus()
    unmount()

    renderLogin(authValue())
    expect(screen.getByLabelText('Username')).toHaveFocus()
  })

  it('replaces the standing warning with the message for the attempt just made', async () => {
    const user = userEvent.setup()
    const login = vi.fn().mockRejectedValue(new NetworkError('Failed to fetch'))
    renderLogin(authValue({ status: 'unreachable', login }))

    await user.type(screen.getByLabelText('Username'), 'alice')
    await user.type(screen.getByLabelText('Password'), 'secret')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/could not reach the server/i)
    expect(screen.queryByTestId('login-offline-notice')).not.toBeInTheDocument()
  })
})
