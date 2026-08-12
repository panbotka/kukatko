import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

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

/** Surfaces the resolved path, so a test can prove the URL did not move. */
function LocationProbe() {
  return <span data-testid="pathname">{useLocation().pathname}</span>
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
          <LocationProbe />
        </MemoryRouter>
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

  it('explains an unreachable backend instead of sending anyone to the login form', () => {
    // Redirecting here was the bug: the visitor was never signed out, and the
    // form they landed on could not reach a server, so it answered every attempt
    // with "invalid username or password".
    renderApp(authValue('unreachable'), 'auth')

    expect(screen.getByTestId('offline-page')).toHaveTextContent(/cannot reach the server/i)
    expect(screen.queryByText('login page')).not.toBeInTheDocument()
    expect(screen.queryByText('secret content')).not.toBeInTheDocument()
  })

  it('keeps the URL on the route asked for, so a retry lands on the right page', () => {
    renderApp(authValue('unreachable'), 'auth')

    expect(screen.getByTestId('pathname')).toHaveTextContent('/secret')
  })
})

describe('RequireRole', () => {
  it('explains the refusal instead of redirecting users below the required role', () => {
    renderApp(authValue('authenticated', 'viewer'), 'role')

    // A 403 page in place of the route, naming the role that is missing.
    expect(screen.getByTestId('forbidden-page')).toHaveTextContent(/administrator role/i)
    expect(screen.queryByText('secret content')).not.toBeInTheDocument()
    expect(screen.queryByText('home page')).not.toBeInTheDocument()
  })

  it('keeps the URL on the protected route, so a reload repeats the explanation', () => {
    renderApp(authValue('authenticated', 'viewer'), 'role')

    expect(screen.getByTestId('pathname')).toHaveTextContent('/secret')
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

  it('shows admins, editors and viewers the 403 page on the route itself', () => {
    // Import is now an operations capability: only a maintainer holds it, so even
    // an admin (governance, not operations) is refused — and told so, on the URL
    // they asked for, rather than being dropped on the library.
    for (const role of ['admin', 'editor', 'viewer'] as const) {
      const { unmount } = renderApp(authValue('authenticated', role), 'import')
      expect(screen.getByTestId('forbidden-page')).toHaveTextContent(/maintainer role/i)
      expect(screen.getByTestId('pathname')).toHaveTextContent('/secret')
      expect(screen.queryByText('secret content')).not.toBeInTheDocument()
      expect(screen.queryByText('home page')).not.toBeInTheDocument()
      unmount()
    }
  })
})
