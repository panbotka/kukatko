import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { CAPABILITIES_DEFAULT, CapabilitiesContext } from '../capabilities/CapabilitiesContext'
import i18n from '../i18n'

import { Layout } from './Layout'

/** Builds an auth context value with the given capabilities. */
function auth(
  opts: {
    canWrite?: boolean
    isAdmin?: boolean
    isMaintainer?: boolean
    role?: string
    /** The person of the library this account says it is; omitted means none. */
    subjectUid?: string
  } = {},
): AuthContextValue {
  const { canWrite = false, isMaintainer = false } = opts
  // A maintainer is admin-or-higher, so it satisfies isAdmin too.
  const isAdmin = opts.isAdmin ?? isMaintainer
  const role =
    opts.role ?? (isMaintainer ? 'maintainer' : isAdmin ? 'admin' : canWrite ? 'editor' : 'viewer')
  return {
    status: 'authenticated',
    user: {
      uid: 'u1',
      username: 'u',
      display_name: 'User One',
      role,
      subject_uid: opts.subjectUid ?? null,
    },
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

function renderLayout(value: AuthContextValue, path = '/', caps = CAPABILITIES_DEFAULT) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={value}>
        <CapabilitiesContext.Provider value={caps}>
          <MemoryRouter initialEntries={[path]}>
            <Routes>
              <Route element={<Layout />}>
                <Route path={path} element={<div>page content</div>} />
              </Route>
            </Routes>
          </MemoryRouter>
        </CapabilitiesContext.Provider>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/** Capabilities as they arrive from a stamped (released) build. */
const withVersion = {
  ...CAPABILITIES_DEFAULT,
  version: { version: '0.5.1', commit: '77fba72' },
}

/**
 * Renders the Layout with several destination routes registered under it, so a
 * navigation triggered from the menu lands on a real route and keeps the shell
 * (and thus the navbar under test) mounted. Starts at the library root.
 */
function renderLayoutWithRoutes(value: AuthContextValue) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={value}>
        <MemoryRouter initialEntries={['/']}>
          <Routes>
            <Route element={<Layout />}>
              <Route path="/" element={<div>home page</div>} />
              <Route path="/albums" element={<div>albums page</div>} />
              <Route path="/favorites" element={<div>favorites page</div>} />
            </Route>
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/**
 * Points `window.matchMedia` at a fixed phone/desktop answer. The shared test
 * setup stubs a non-matching (desktop) `matchMedia`; the phone case overrides it
 * so `useIsNarrowViewport` takes its narrow branch and the bar renders the way a
 * phone sees it (no inline collapse, the drawer instead).
 */
function mockViewport(narrow: boolean): void {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: narrow,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
}

/** True when `first` precedes `second` in document order. */
function precedes(first: Element, second: Element): boolean {
  return (first.compareDocumentPosition(second) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0
}

/**
 * True when the collapsible mobile menu is closed. react-bootstrap tags the
 * hamburger toggle with `collapsed` exactly when the navbar's `expanded` state
 * is false, so this reads that controlled state without depending on the CSS
 * collapse transition (which does not run under jsdom).
 */
function menuIsCollapsed(): boolean {
  return screen.getByRole('button', { name: /toggle navigation/i }).classList.contains('collapsed')
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  // Reassigning `window.matchMedia` outlives `restoreMocks`, so re-pin the
  // desktop answer here rather than leaking a phone viewport into the next test.
  mockViewport(false)
})

describe('Layout navbar', () => {
  it('holds every shortcut back while the command palette has the focus', async () => {
    renderLayout(auth())

    // `/` opens the palette; with its field focused the app's own shortcuts —
    // `?` for this very help among them — must stay out of the way.
    fireEvent.keyDown(document, { key: '/' })
    expect(await screen.findByRole('dialog')).toBeInTheDocument()

    fireEvent.keyDown(document, { key: '?' })
    await waitFor(() => {
      expect(screen.getAllByRole('dialog')).toHaveLength(1)
    })
    expect(screen.queryByText('Keyboard shortcuts', { selector: '.modal-title' })).toBeNull()
  })

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

  it('puts everything in flow inside the page column the timeline lane is cut from', () => {
    // The fixed timeline rail is given a lane out of this element's width
    // (`.kukatko-page:has(.kukatko-timeline)`), which only keeps the rail off the
    // content while the content is actually inside it — the footer included, since
    // the rail runs to the bottom of the viewport. The navbar stays outside: the
    // rail comes to rest below it.
    const { container } = renderLayout(auth())
    const page = container.querySelector('.kukatko-page')
    expect(page).not.toBeNull()
    expect(page?.querySelector('main.kukatko-main')).not.toBeNull()
    expect(page?.querySelector('footer.kukatko-footer')).not.toBeNull()
    expect(page?.querySelector('.kukatko-navbar')).toBeNull()
  })

  it('carries no logo and no wordmark, on either viewport', () => {
    // Width is the bar's scarce resource: the brand is gone entirely, mark and
    // wordmark alike, rather than being juggled by display utilities.
    const { container, unmount } = renderLayout(auth(), '/albums')
    expect(screen.queryByRole('link', { name: /Kukátko/ })).not.toBeInTheDocument()
    const bar = container.querySelector('.kukatko-navbar')
    expect(bar).not.toBeNull()
    expect(bar?.textContent).not.toContain('Kukátko')
    expect(container.querySelector('.kukatko-navbar i.bi-binoculars-fill')).toBeNull()

    unmount()
    mockViewport(true)
    const phone = renderLayout(auth(), '/albums')
    expect(phone.container.querySelector('.kukatko-navbar i.bi-binoculars-fill')).toBeNull()
    expect(screen.queryByRole('link', { name: /Kukátko/ })).not.toBeInTheDocument()
  })

  it('keeps a labelled one-tap way home on the desktop bar', () => {
    // Start away from home so the way back is a real link, not a self-link. With
    // the brand gone this is what replaces it: the bar's first item, labelled.
    renderLayout(auth(), '/albums')

    const home = screen.getByRole('link', { name: 'Library' })
    expect(home).toHaveAttribute('href', '/')
    expect(home).toHaveAttribute('title', 'Show the photo library')
    expect(home.closest('.navbar.kukatko-navbar')).not.toBeNull()
  })

  it('keeps a one-tap way home on a phone, in the bottom tab bar', () => {
    mockViewport(true)
    renderLayout(auth(), '/albums')

    // The phone bar has no nav at all (it folds into the drawer), so the way home
    // is the tab bar's leading tab — permanently under the thumb, no menu first.
    const home = screen.getByRole('link', { name: 'Library' })
    expect(home).toHaveAttribute('href', '/')
    expect(home.closest('.kk-tabbar')).not.toBeNull()
  })

  it('pairs search with the hamburger on the phone row', () => {
    mockViewport(true)
    renderLayout(auth())

    // On a phone the nav folds into the drawer entirely, so the top row is just
    // `[search] [hamburger]` — search first (the hamburger belongs on the
    // trailing edge), and outside the collapse so it never folds away.
    const search = screen.getByRole('button', { name: 'Search' })
    const toggle = screen.getByRole('button', { name: /toggle navigation/i })
    expect(precedes(search, toggle)).toBe(true)
    expect(search.closest('.navbar.kukatko-navbar')).not.toBeNull()

    // The bar did not bring the inline nav back with it: the only Albums link on
    // a phone is the bottom tab bar's.
    const albums = screen.getByRole('link', { name: 'Albums' })
    expect(albums.closest('.navbar.kukatko-navbar')).toBeNull()
  })

  it('keeps Library, Albums, Labels and Search as always-visible top-level links', () => {
    renderLayout(auth())

    // The library is the homepage, so its nav entry points at the root route.
    expect(screen.getByRole('link', { name: 'Library' })).toHaveAttribute('href', '/')
    expect(screen.getByRole('link', { name: 'Albums' })).toHaveAttribute('href', '/albums')
    expect(screen.getByRole('link', { name: 'Labels' })).toHaveAttribute('href', '/labels')
    // The search page is the only place with the query-language help and the
    // mode selector, so it is a labelled destination like the rest — not just
    // the magnifier circle's hover tooltip.
    const search = screen.getByRole('link', { name: 'Search' })
    expect(search).toHaveAttribute('href', '/search')
    expect(search).toHaveAttribute('title', 'Search the photos')
    expect(search.querySelector('i.bi.bi-search')).not.toBeNull()
  })

  it('keeps the command-palette trigger beside the labelled search entry', () => {
    renderLayout(auth())

    // Two doors to the same feature, deliberately: the icon button opens the
    // palette (a button, named by its action), the nav link goes to the full
    // page. The old saved-searches dropdown is still gone from the bar.
    const trigger = screen.getByRole('button', { name: 'Search' })
    expect(trigger).toHaveAttribute('title', 'Search the whole library (press / or Ctrl+K)')
    expect(precedes(trigger, screen.getByRole('link', { name: 'Search' }))).toBe(true)
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
      // Saved searches are smart albums, so they sit next to the favorites
      // instead of hiding in a dropdown on the search page.
      ['Saved searches', '/saved'],
      ['People', '/people'],
      ['Places', '/places'],
      ['Map', '/map'],
      ['Leaderboard', '/leaderboard'],
    ]) {
      expect(screen.getByRole('link', { name })).toHaveAttribute('href', href)
    }
  })

  it('demotes the leaderboard out of the top level into Browse', async () => {
    const user = userEvent.setup()
    renderLayout(auth())

    // A scoreboard for a game a handful of people play does not earn a slot
    // beside Knihovna and Alba; it is one level down with the other ways of
    // looking at the library.
    expect(screen.queryByRole('link', { name: 'Leaderboard' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Browse' }))
    expect(screen.getByRole('link', { name: 'Leaderboard' })).toHaveAttribute(
      'href',
      '/leaderboard',
    )
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

  it('hides the Tools group, the admin section and Upload from viewers', async () => {
    const user = userEvent.setup()
    renderLayout(auth({ canWrite: false, isAdmin: false }))

    // Browse is available to every role.
    await user.click(screen.getByRole('button', { name: 'Browse' }))
    expect(screen.getByRole('link', { name: 'Favorites' })).toBeInTheDocument()

    // The editor group does not render, nor the write-only Upload entry.
    expect(screen.queryByRole('button', { name: 'Tools' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Upload' })).not.toBeInTheDocument()

    // And the user menu carries no administration: no heading, no divider left
    // hanging over nothing, none of the five destinations.
    await user.click(screen.getByRole('button', { name: 'User One' }))
    expect(screen.queryByText('Admin')).not.toBeInTheDocument()
    for (const name of ['Import', 'Maintenance', 'System', 'Users', 'Audit']) {
      expect(screen.queryByRole('link', { name })).not.toBeInTheDocument()
    }
  })

  it('keeps the administration out of the bar entirely', async () => {
    const user = userEvent.setup()
    renderLayout(auth({ canWrite: true, isMaintainer: true }))

    // The two dropdowns that used to eat this row's width are gone; what is left
    // behind the divider is the editor's Tools.
    expect(screen.queryByRole('button', { name: 'Operations' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Admin' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Tools' })).toBeInTheDocument()

    // None of the administration destinations is reachable from the bar itself —
    // they only appear once the user menu is opened.
    expect(screen.queryByRole('link', { name: 'Import' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Users' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'User One' }))
    expect(screen.getByRole('link', { name: 'Import' })).toBeInTheDocument()
  })

  it('gives a maintainer the whole admin section, above sign-out', async () => {
    const user = userEvent.setup()
    renderLayout(auth({ canWrite: true, isMaintainer: true }))

    await user.click(screen.getByRole('button', { name: 'User One' }))

    // One labelled section, in ladder order: operations first, governance after.
    const heading = screen.getByText('Admin')
    expect(heading).toHaveClass('dropdown-header')
    const links = ['Import', 'Maintenance', 'System', 'Users', 'Audit', 'Settings'].map((name) =>
      screen.getByRole('link', { name }),
    )
    expect(links.map((link) => link.getAttribute('href'))).toEqual([
      '/import',
      '/maintenance',
      '/system',
      '/users',
      '/audit',
      '/settings',
    ])
    expect(links[0]).toHaveAttribute('title', 'Run a photo import')
    expect(links[0].querySelector('i.bi.bi-box-arrow-in-down')).not.toBeNull()

    // It sits between the account block and the one destructive action.
    expect(precedes(screen.getByRole('link', { name: 'Help' }), heading)).toBe(true)
    for (const link of links) {
      expect(precedes(heading, link)).toBe(true)
      expect(precedes(link, screen.getByRole('button', { name: 'Sign out' }))).toBe(true)
    }
  })

  it('gates the admin section per item: an admin gets governance, not operations', async () => {
    const user = userEvent.setup()
    // An admin is not a maintainer, so operations stay out of reach — but the
    // section itself is not withheld along with them.
    renderLayout(auth({ canWrite: true, isAdmin: true }))

    await user.click(screen.getByRole('button', { name: 'User One' }))

    expect(screen.getByText('Admin')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Users' })).toHaveAttribute('href', '/users')
    expect(screen.getByRole('link', { name: 'Audit' })).toHaveAttribute('href', '/audit')
    expect(screen.getByRole('link', { name: 'Settings' })).toHaveAttribute('href', '/settings')
    for (const name of ['Import', 'Maintenance', 'System']) {
      expect(screen.queryByRole('link', { name })).not.toBeInTheDocument()
    }
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

  it('offers the library statistics in the user menu, for a plain viewer too', async () => {
    const user = userEvent.setup()
    // auth() is a viewer: the counts carry no role gate, so they must be here.
    renderLayout(auth())

    await user.click(screen.getByRole('button', { name: 'User One' }))

    const stats = screen.getByRole('link', { name: 'Statistics' })
    expect(stats).toHaveAttribute('href', '/stats')
    expect(stats).toHaveAttribute('title', 'Show the library statistics')
  })

  it('offers "my photos" in the user menu only once the account names a person', async () => {
    const user = userEvent.setup()
    // An unlinked account: the entry would lead to a person-scoped grid of
    // nobody, so it is not offered at all.
    renderLayout(auth())
    await user.click(screen.getByRole('button', { name: 'User One' }))
    expect(screen.queryByRole('link', { name: 'My photos' })).not.toBeInTheDocument()
  })

  it('points "my photos" at the linked person\'s scoped grid', async () => {
    const user = userEvent.setup()
    renderLayout(auth({ subjectUid: 'sub123' }))

    await user.click(screen.getByRole('button', { name: 'User One' }))

    const mine = screen.getByRole('link', { name: 'My photos' })
    expect(mine).toHaveAttribute('href', '/?person=sub123')
    expect(mine).toHaveAttribute('title', 'Show the photos I am on')
  })

  it('prints the build version in the user menu, above sign-out', async () => {
    const user = userEvent.setup()
    renderLayout(auth(), '/', withVersion)

    await user.click(screen.getByRole('button', { name: 'User One' }))

    // A semantic version is shown the customary way, and the commit is left for
    // the help page — the menu is a narrow place.
    const version = screen.getByText('v0.5.1')
    expect(version).toHaveAttribute('title', 'App version')
    expect(version.textContent).not.toContain('77fba72')

    // Order is what the user asked for: the version sits above sign-out.
    const logout = screen.getByRole('button', { name: 'Sign out' })
    expect(precedes(version, logout)).toBe(true)
  })

  it('shows the version as inert text, not a menu item', async () => {
    const user = userEvent.setup()
    renderLayout(auth(), '/', withVersion)

    await user.click(screen.getByRole('button', { name: 'User One' }))

    // It is a `dropdown-item-text`: no role, no href, nothing to activate, and
    // no tab stop — arrowing or tabbing through the menu passes it by.
    const version = screen.getByText('v0.5.1')
    expect(version.tagName).toBe('SPAN')
    expect(version).toHaveClass('dropdown-item-text')
    expect(version.closest('a, button')).toBeNull()
    expect(version).not.toHaveAttribute('href')
    expect(version).not.toHaveAttribute('tabindex')

    // Tab from the account item lands on the next real item, never on the text.
    screen.getByRole('link', { name: 'My account' }).focus()
    await user.tab()
    await user.tab()
    await user.tab()
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Sign out' }))
  })

  it('shows no version when the capabilities call has not answered', async () => {
    const user = userEvent.setup()
    // The default context is what a failed (or not yet resolved) call leaves
    // behind: the menu must still open and work, just without a version.
    renderLayout(auth())

    await user.click(screen.getByRole('button', { name: 'User One' }))

    expect(screen.queryByTitle('App version')).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'My account' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Sign out' })).toBeInTheDocument()
  })

  it('closes the open mobile menu after tapping a top-level link', async () => {
    const user = userEvent.setup()
    renderLayoutWithRoutes(auth())

    // The menu starts collapsed; opening the hamburger expands it.
    expect(menuIsCollapsed()).toBe(true)
    await user.click(screen.getByRole('button', { name: /toggle navigation/i }))
    expect(menuIsCollapsed()).toBe(false)

    // Tapping a destination navigates and must dismiss the menu, rather than
    // leaving it expanded over the page it just opened.
    await user.click(screen.getByRole('link', { name: 'Albums' }))
    expect(await screen.findByText('albums page')).toBeInTheDocument()
    await waitFor(() => {
      expect(menuIsCollapsed()).toBe(true)
    })
  })

  it('closes the open mobile menu after tapping an item inside a group dropdown', async () => {
    const user = userEvent.setup()
    renderLayoutWithRoutes(auth())

    // Open the mobile menu, then the Browse group inside it.
    await user.click(screen.getByRole('button', { name: /toggle navigation/i }))
    expect(menuIsCollapsed()).toBe(false)
    await user.click(screen.getByRole('button', { name: 'Browse' }))

    // A grouped item is a raw Dropdown.Item, which never fired the select event
    // the old collapseOnSelect relied on. It must still close the menu.
    await user.click(screen.getByRole('link', { name: 'Favorites' }))
    expect(await screen.findByText('favorites page')).toBeInTheDocument()
    await waitFor(() => {
      expect(menuIsCollapsed()).toBe(true)
    })
  })

  it('renders the global footer below the routed content', () => {
    renderLayout(auth())
    // Every page under the layout shell gets the operator/source-code footer.
    expect(screen.getByRole('contentinfo')).toHaveTextContent('Operated by SDH Veselice')
  })
})
