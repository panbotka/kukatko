import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'

import { Layout } from './Layout'

/** Builds an auth context value with the given capabilities. */
function auth(
  opts: { canWrite?: boolean; isAdmin?: boolean; isMaintainer?: boolean; role?: string } = {},
): AuthContextValue {
  const { canWrite = false, isMaintainer = false } = opts
  // A maintainer is admin-or-higher, so it satisfies isAdmin too.
  const isAdmin = opts.isAdmin ?? isMaintainer
  const role =
    opts.role ?? (isMaintainer ? 'maintainer' : isAdmin ? 'admin' : canWrite ? 'editor' : 'viewer')
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'User One', role },
    role,
    downloadToken: null,
    canWrite: canWrite || isAdmin,
    isAdmin,
    isMaintainer,
    // Import is an operations capability: maintainer only.
    canImport: isMaintainer,
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

    // The library is the homepage, so its nav entry points at the root route.
    expect(screen.getByRole('link', { name: 'Library' })).toHaveAttribute('href', '/')
    expect(screen.getByRole('link', { name: 'Albums' })).toHaveAttribute('href', '/albums')
    expect(screen.getByRole('link', { name: 'Labels' })).toHaveAttribute('href', '/labels')
  })

  it('leads the bar with a global command-search trigger, not the old search link', () => {
    renderLayout(auth())

    // Search is promoted to a command palette opened from a field-shaped trigger
    // button (named by its action). The old plain "Search" nav link and the
    // saved-searches dropdown are gone.
    expect(screen.getByRole('button', { name: 'Search' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Search' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Saved searches' })).not.toBeInTheDocument()
  })

  it('no longer offers the language switcher', () => {
    renderLayout(auth())

    // The language setting lives on the account page: a Czech-only instance does
    // not spend permanent bar space on it.
    expect(screen.queryByRole('group', { name: 'Switch language' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Čeština' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'English' })).not.toBeInTheDocument()
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

  it('hides the Tools/Operations/Admin groups and Upload from viewers', async () => {
    const user = userEvent.setup()
    renderLayout(auth({ canWrite: false, isAdmin: false }))

    // Browse is available to every role.
    await user.click(screen.getByRole('button', { name: 'Browse' }))
    expect(screen.getByRole('link', { name: 'Favorites' })).toBeInTheDocument()

    // None of the role-gated groups render, and the write-only Upload entry — plus
    // Import, which now lives inside the maintainer Operations group — stay hidden.
    expect(screen.queryByRole('button', { name: 'Tools' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Operations' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Admin' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Upload' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Import' })).not.toBeInTheDocument()
  })

  it('gives an admin governance (Users/Audit) but withholds the maintainer Operations group', async () => {
    const user = userEvent.setup()
    // An admin is not a maintainer, so operations stay out of reach.
    renderLayout(auth({ canWrite: true, isAdmin: true }))

    // Upload is a prominent top-level CTA for any writer.
    expect(screen.getByRole('link', { name: 'Upload' })).toBeInTheDocument()

    // The editor Tools dropdown is present.
    await user.click(screen.getByRole('button', { name: 'Tools' }))
    expect(screen.getByRole('link', { name: 'Trash' })).toBeInTheDocument()

    // The governance Admin dropdown groups Users and Audit — nothing operational.
    await user.click(screen.getByRole('button', { name: 'Admin' }))
    expect(screen.getByRole('link', { name: 'Users' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Audit' })).toBeInTheDocument()

    // The operations dropdown (import, maintenance, system) is maintainer-only.
    expect(screen.queryByRole('button', { name: 'Operations' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Import' })).not.toBeInTheDocument()
  })

  it('shows the maintainer Operations group with import, maintenance and system', async () => {
    const user = userEvent.setup()
    renderLayout(auth({ canWrite: true, isMaintainer: true }))

    // Import is no longer a top-level link; it lives inside Operations.
    expect(screen.queryByRole('link', { name: 'Import' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Operations' }))
    expect(screen.getByRole('link', { name: 'Import' })).toHaveAttribute('href', '/import')
    expect(screen.getByRole('link', { name: 'Maintenance' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'System' })).toBeInTheDocument()

    // A maintainer is admin-or-higher, so governance stays available too.
    expect(screen.getByRole('button', { name: 'Admin' })).toBeInTheDocument()
  })

  it('makes Upload the bar’s single filled call-to-action', () => {
    renderLayout(auth({ canWrite: true }))
    // Adding photos is the everyday loop's payoff, so it reads as a button rather
    // than one more link at the same volume as Import.
    expect(screen.getByRole('link', { name: 'Upload' })).toHaveClass('kukatko-nav-cta')
    // No other top-level entry borrows the call-to-action styling.
    expect(screen.getByRole('link', { name: 'Albums' })).not.toHaveClass('kukatko-nav-cta')
  })

  it('tucks the expand tool inside the Tools group instead of shouting at top level', async () => {
    const user = userEvent.setup()
    renderLayout(auth({ canWrite: true }))

    // Expand is a power-user tool, so it is not one of the always-visible links…
    expect(screen.queryByRole('link', { name: 'Expand' })).not.toBeInTheDocument()
    // …it lives one level down, inside the Tools dropdown.
    await user.click(screen.getByRole('button', { name: 'Tools' }))
    expect(screen.getByRole('link', { name: 'Expand' })).toHaveAttribute('href', '/expand')
  })

  it('omits the tools/admin divider for a viewer who has nothing past it', () => {
    const { container } = renderLayout(auth())
    expect(container.querySelector('.kukatko-nav-divider')).toBeNull()
  })

  it('fences the quieter tools/admin cluster off with a divider when one exists', () => {
    const { container } = renderLayout(auth({ canWrite: true }))
    expect(container.querySelector('.kukatko-nav-divider')).not.toBeNull()
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

  it('offers a Help item in the user menu, linking to the help page', async () => {
    const user = userEvent.setup()
    renderLayout(auth())

    // The user menu is a dropdown labelled by the signed-in display name.
    await user.click(screen.getByRole('button', { name: 'User One' }))

    // Help sits alongside the account item (above the sign-out divider).
    expect(screen.getByRole('link', { name: 'My account' })).toBeInTheDocument()
    const help = screen.getByRole('link', { name: 'Help' })
    expect(help).toHaveAttribute('href', '/help')
    expect(help).toHaveAttribute('title', 'Show help')
  })

  it('renders the global footer below the routed content', () => {
    renderLayout(auth())
    // Every page under the layout shell gets the operator/source-code footer.
    expect(screen.getByRole('contentinfo')).toHaveTextContent('Operated by SDH Veselice')
  })
})
