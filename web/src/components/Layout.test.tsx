import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
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

  it('hides the Tools/Admin groups and Upload from viewers', async () => {
    const user = userEvent.setup()
    renderLayout(auth({ canWrite: false, isAdmin: false }))

    // Browse is available to every role: opening it reveals the library links.
    const browse = screen.getByRole('button', { name: 'Browse' })
    await user.click(browse)
    expect(screen.getByRole('link', { name: 'Library' })).toBeInTheDocument()

    // Neither the editor Tools group nor the admin Admin group is rendered,
    // and the write-only Upload entry stays hidden.
    expect(screen.queryByRole('button', { name: 'Tools' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Admin' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Upload' })).not.toBeInTheDocument()
  })

  it('shows the Tools group to editors and the Admin group to admins', async () => {
    const user = userEvent.setup()
    renderLayout(auth({ canWrite: true, isAdmin: true }))

    // Upload is prominent as a top-level link.
    expect(screen.getByRole('link', { name: 'Upload' })).toBeInTheDocument()

    // The editor-only Tools dropdown groups Duplicates and Trash.
    await user.click(screen.getByRole('button', { name: 'Tools' }))
    expect(screen.getByRole('link', { name: 'Trash' })).toBeInTheDocument()

    // The admin-only Admin dropdown groups Import, Maintenance and System.
    await user.click(screen.getByRole('button', { name: 'Admin' }))
    expect(screen.getByRole('link', { name: 'Import' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'System' })).toBeInTheDocument()
  })

  it('marks the parent group active when a child route is current', () => {
    render(
      <I18nextProvider i18n={i18n}>
        <AuthContext.Provider value={auth({ canWrite: true, isAdmin: true })}>
          <MemoryRouter initialEntries={['/albums']}>
            <Routes>
              <Route element={<Layout />}>
                <Route path="/albums" element={<div>albums content</div>} />
              </Route>
            </Routes>
          </MemoryRouter>
        </AuthContext.Provider>
      </I18nextProvider>,
    )
    // /albums lives under Browse, so its toggle carries the active state.
    expect(screen.getByRole('button', { name: 'Browse' })).toHaveClass('active')
  })
})
