import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { ApiError } from '../services/auth'

import { LoginPage } from './LoginPage'

function authValue(overrides: Partial<AuthContextValue> = {}): AuthContextValue {
  return {
    status: 'unauthenticated',
    user: null,
    role: null,
    downloadToken: null,
    canWrite: false,
    isAdmin: false,
    canImport: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
    ...overrides,
  }
}

function renderLogin(value: AuthContextValue) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={value}>
        <MemoryRouter initialEntries={['/login']}>
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route path="/" element={<div>home page</div>} />
          </Routes>
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

  it('renders the rate-limited message on a 429', async () => {
    const user = userEvent.setup()
    const login = vi.fn().mockRejectedValue(new ApiError(429, 'too many'))
    renderLogin(authValue({ login }))

    await user.type(screen.getByLabelText('Username'), 'alice')
    await user.type(screen.getByLabelText('Password'), 'secret')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/too many login attempts/i)
  })
})
