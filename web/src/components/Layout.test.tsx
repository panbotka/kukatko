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

function renderLayout(value: AuthContextValue, path = '/') {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={value}>
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route element={<Layout />}>
              <Route path={path} element={<div>page content</div>} />
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

  it('keeps Library, Albums and Labels as always-visible top-level links', () => {
    renderLayout(auth())

    expect(screen.getByRole('link', { name: 'Library' })).toHaveAttribute('href', '/library')
    expect(screen.getByRole('link', { name: 'Albums' })).toHaveAttribute('href', '/albums')
    expect(screen.getByRole('link', { name: 'Labels' })).toHaveAttribute('href', '/labels')
  })

  it('no longer offers a search link, a search box or a saved-searches menu', () => {
    renderLayout(auth())

    // Searching is reached from the library page and from /search directly.
    expect(screen.queryByRole('link', { name: 'Search' })).not.toBeInTheDocument()
    expect(screen.queryByRole('search')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Saved searches' })).not.toBeInTheDocument()
  })

  it('groups the remaining browse destinations behind one dropdown', async () => {
    const user = userEvent.setup()
    renderLayout(auth())

    await user.click(screen.getByRole('button', { name: 'Browse' }))
    for (const [name, href] of [
      ['Favorites', '/favorites'],
      ['People', '/people'],
      ['Places', '/places'],
      ['Map', '/map'],
    ]) {
      expect(screen.getByRole('link', { name })).toHaveAttribute('href', href)
    }
  })

  it('gives every nav entry an icon and a title describing the action', async () => {
    const user = userEvent.setup()
    const { container } = renderLayout(auth())

    // The title names the action ("Show the albums"), not the noun ("Albums").
    const albums = screen.getByRole('link', { name: 'Albums' })
    expect(albums).toHaveAttribute('title', 'Show the albums')
    // Icons are decorative: hidden from assistive tech, next to a visible label.
    const icon = albums.querySelector('i.bi.bi-collection')
    expect(icon).not.toBeNull()
    expect(icon).toHaveAttribute('aria-hidden', 'true')

    // Dropdown toggles carry the same affordance…
    const browse = screen.getByRole('button', { name: 'Browse' })
    expect(browse).toHaveAttribute('title', 'Show more ways to browse')
    expect(browse.querySelector('i.bi.bi-compass')).not.toBeNull()

    // …as do the entries inside them.
    await user.click(browse)
    const map = screen.getByRole('link', { name: 'Map' })
    expect(map).toHaveAttribute('title', 'Show the photos on a map')
    expect(map.querySelector('i.bi.bi-map')).not.toBeNull()

    // Every icon in the bar comes from the same set, and none of them is exposed.
    const icons = container.querySelectorAll('.kukatko-navbar i.bi')
    expect(icons.length).toBeGreaterThan(0)
    for (const glyph of icons) {
      expect(glyph).toHaveAttribute('aria-hidden', 'true')
    }
  })

  it('hides the Tools/Admin groups and Upload from viewers', async () => {
    const user = userEvent.setup()
    renderLayout(auth({ canWrite: false, isAdmin: false }))

    // Browse is available to every role.
    await user.click(screen.getByRole('button', { name: 'Browse' }))
    expect(screen.getByRole('link', { name: 'Favorites' })).toBeInTheDocument()

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

  it('marks a top-level link active on its detail sub-paths', () => {
    renderLayout(auth(), '/albums/ab12')
    expect(screen.getByRole('link', { name: 'Albums' })).toHaveClass('active')
    expect(screen.getByRole('button', { name: 'Browse' })).not.toHaveClass('active')
  })

  it('marks the parent group active when a child route is current', () => {
    renderLayout(auth(), '/places')
    // /places lives under Browse, so its toggle carries the active state.
    expect(screen.getByRole('button', { name: 'Browse' })).toHaveClass('active')
  })
})
