import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'

import { Layout } from './Layout'

/** Builds an auth context value with the given capabilities. */
function auth(opts: { canWrite?: boolean; isAdmin?: boolean } = {}): AuthContextValue {
  const { canWrite = false, isAdmin = false } = opts
  const role = isAdmin ? 'admin' : canWrite ? 'editor' : 'viewer'
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'User One', role },
    role,
    downloadToken: null,
    canWrite,
    isAdmin,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

function renderLayout(value: AuthContextValue) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={value}>
        <MemoryRouter initialEntries={['/']}>
          <Routes>
            <Route element={<Layout />}>
              <Route path="/" element={<div>home content</div>} />
            </Route>
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('Layout navbar', () => {
  it('renders a collapsible mobile menu toggle wired to the nav collapse', () => {
    renderLayout(auth())
    // The hamburger toggle (visible below the `md` breakpoint) controls the
    // collapsible nav region, so touch users can open the menu.
    const toggle = screen.getByRole('button', { name: /toggle navigation/i })
    expect(toggle).toHaveAttribute('aria-controls', 'main-navbar')
  })

  it('applies safe-area padding class to the navbar', () => {
    const { container } = renderLayout(auth())
    expect(container.querySelector('.navbar.kukatko-navbar')).not.toBeNull()
    expect(container.querySelector('main.kukatko-main')).not.toBeNull()
  })

  it('hides write- and admin-only links from viewers', () => {
    renderLayout(auth({ canWrite: false, isAdmin: false }))
    expect(screen.getByRole('link', { name: 'Library' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Upload' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Import' })).not.toBeInTheDocument()
  })

  it('shows write links to editors and admin links to admins', () => {
    renderLayout(auth({ canWrite: true, isAdmin: true }))
    expect(screen.getByRole('link', { name: 'Upload' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Trash' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Import' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'System' })).toBeInTheDocument()
  })
})
