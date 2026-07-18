import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { type Role } from '../services/auth'

import { AuthContext, type AuthContextValue, type AuthStatus } from './AuthContext'
import { RequireAuth, RequireImport, RequireRole } from './ProtectedRoute'

function authValue(status: AuthStatus, role: Role | null = null): AuthContextValue {
  const isAdmin = role === 'admin' || role === 'maintainer'
  const isMaintainer = role === 'maintainer'
  return {
    status,
    user: role ? ({ role } as AuthContextValue['user']) : null,
    role,
    downloadToken: null,
    canWrite: role === 'editor' || isAdmin,
    isAdmin,
    isMaintainer,
    // Import is an operations capability: maintainer only.
    canImport: isMaintainer,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  }
}

function renderApp(
  value: AuthContextValue,
  guard: 'auth' | 'role' | 'import',
  initial = '/secret',
) {
  const guardElement = {
    auth: <RequireAuth />,
    role: <RequireRole role="admin" />,
    import: <RequireImport />,
  }[guard]
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={value}>
        <MemoryRouter initialEntries={[initial]}>
          <Routes>
            <Route path="/login" element={<div>login page</div>} />
            <Route path="/" element={<div>home page</div>} />
            <Route element={guardElement}>
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

describe('RequireImport', () => {
  it('renders the content for maintainers', () => {
    renderApp(authValue('authenticated', 'maintainer'), 'import')

    expect(screen.getByText('secret content')).toBeInTheDocument()
  })

  it('redirects admins, editors and viewers to home', () => {
    // Import is now an operations capability: only a maintainer holds it, so even
    // an admin (governance, not operations) is sent home.
    for (const role of ['admin', 'editor', 'viewer'] as const) {
      const { unmount } = renderApp(authValue('authenticated', role), 'import')
      expect(screen.getByText('home page')).toBeInTheDocument()
      expect(screen.queryByText('secret content')).not.toBeInTheDocument()
      unmount()
    }
  })
})
