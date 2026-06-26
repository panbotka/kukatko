import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { type Role } from '../services/auth'

import { AuthContext, type AuthContextValue, type AuthStatus } from './AuthContext'
import { RequireAuth, RequireRole } from './ProtectedRoute'

function authValue(status: AuthStatus, role: Role | null = null): AuthContextValue {
  return {
    status,
    user: role ? ({ role } as AuthContextValue['user']) : null,
    role,
    downloadToken: null,
    canWrite: role === 'editor' || role === 'admin',
    isAdmin: role === 'admin',
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  }
}

function renderApp(value: AuthContextValue, guard: 'auth' | 'role', initial = '/secret') {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={value}>
        <MemoryRouter initialEntries={[initial]}>
          <Routes>
            <Route path="/login" element={<div>login page</div>} />
            <Route path="/" element={<div>home page</div>} />
            <Route element={guard === 'auth' ? <RequireAuth /> : <RequireRole role="admin" />}>
              <Route path="/secret" element={<div>secret content</div>} />
            </Route>
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

describe('RequireAuth', () => {
  it('redirects unauthenticated users to the login page', () => {
    renderApp(authValue('unauthenticated'), 'auth')

    expect(screen.getByText('login page')).toBeInTheDocument()
    expect(screen.queryByText('secret content')).not.toBeInTheDocument()
  })

  it('renders the protected content for authenticated users', () => {
    renderApp(authValue('authenticated', 'viewer'), 'auth')

    expect(screen.getByText('secret content')).toBeInTheDocument()
  })

  it('shows a loading spinner while the session is resolving', () => {
    renderApp(authValue('loading'), 'auth')

    expect(screen.getByRole('status')).toBeInTheDocument()
    expect(screen.queryByText('secret content')).not.toBeInTheDocument()
  })
})

describe('RequireRole', () => {
  it('redirects users below the required role to home', () => {
    renderApp(authValue('authenticated', 'viewer'), 'role')

    expect(screen.getByText('home page')).toBeInTheDocument()
    expect(screen.queryByText('secret content')).not.toBeInTheDocument()
  })

  it('renders the content for users meeting the required role', () => {
    renderApp(authValue('authenticated', 'admin'), 'role')

    expect(screen.getByText('secret content')).toBeInTheDocument()
  })
})
