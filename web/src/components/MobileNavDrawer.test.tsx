import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { CAPABILITIES_DEFAULT, CapabilitiesContext } from '../capabilities/CapabilitiesContext'
import i18n from '../i18n'
import { declarations, readCss, ruleBody } from '../test/css'

import { Layout } from './Layout'

/** Builds an auth context value with the given capabilities. */
function auth(
  opts: {
    canWrite?: boolean
    isAdmin?: boolean
    isMaintainer?: boolean
    logout?: () => void
    /** The person of the library this account says it is; omitted means none. */
    subjectUid?: string
  } = {},
): AuthContextValue {
  const { canWrite = false, isMaintainer = false } = opts
  // A maintainer is admin-or-higher, so it satisfies isAdmin too.
  const isAdmin = opts.isAdmin ?? isMaintainer
  const role = isMaintainer ? 'maintainer' : isAdmin ? 'admin' : canWrite ? 'editor' : 'viewer'
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
    canImport: isMaintainer,
    login: vi.fn(),
    logout: opts.logout ?? vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

/**
 * Points `window.matchMedia` at a fixed phone/desktop answer, as the tab bar's
 * tests do: the shared setup stubs a non-matching (desktop) `matchMedia`, so the
 * phone cases have to override it for `useIsNarrowViewport`.
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

/** Renders the shell with a few real destinations, starting at `path`. */
function renderShell(value: AuthContextValue, path = '/', caps = CAPABILITIES_DEFAULT) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={value}>
        <CapabilitiesContext.Provider value={caps}>
          <MemoryRouter initialEntries={[path]}>
            <Routes>
              <Route element={<Layout />}>
                <Route path="/" element={<div>home page</div>} />
                <Route path="/albums" element={<div>albums page</div>} />
                <Route path="/albums/:uid" element={<div>album detail</div>} />
                <Route path="/people" element={<div>people page</div>} />
                <Route path="/login" element={<div>login page</div>} />
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

/** Opens the hamburger and returns the drawer dialog. */
async function openDrawer(user: ReturnType<typeof userEvent.setup>): Promise<HTMLElement> {
  await user.click(screen.getByRole('button', { name: /toggle navigation/i }))
  return screen.findByRole('dialog', { name: 'Menu' })
}

/** The `href` of every link inside the drawer, in document order. */
function drawerHrefs(drawer: HTMLElement): string[] {
  return [...drawer.querySelectorAll('a')].map((a) => a.getAttribute('href') ?? '')
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  mockViewport(true)
})

afterEach(() => {
  // Restore the shared desktop default so later tests never inherit a phone.
  mockViewport(false)
})

describe('MobileNavDrawer', () => {
  it('opens the hamburger as a dismissible drawer, not an inline collapse', async () => {
    const user = userEvent.setup()
    const { container } = renderShell(auth())

    // Closed at rest: nothing of the menu is in the DOM…
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    const drawer = await openDrawer(user)

    // …and open it is a modal panel with a close affordance, not the navbar's
    // collapse expanded inline (which is not rendered on a phone at all).
    expect(drawer).toHaveClass('offcanvas')
    expect(drawer).toHaveAttribute('aria-modal', 'true')
    expect(container.querySelector('.navbar-collapse')).toBeNull()
    expect(within(drawer).getByRole('button', { name: 'Close the menu' })).toBeInTheDocument()
  })

  it('lays the menu out as labelled sections instead of one flat list', async () => {
    const user = userEvent.setup()
    renderShell(auth({ isMaintainer: true }))
    const drawer = await openDrawer(user)

    // Every block names itself, and the name is the accessible name of the
    // region its rows live in — that is what makes the menu scannable.
    for (const name of ['Main', 'Browse', 'Tools', 'Operations', 'Admin', 'Account']) {
      expect(within(drawer).getByRole('heading', { level: 2, name })).toBeInTheDocument()
      expect(within(drawer).getByRole('region', { name })).toBeInTheDocument()
    }
    // Each destination sits inside its own section, not in one undifferentiated
    // stack: Trash is a tool, Import is operations.
    const tools = within(drawer).getByRole('region', { name: 'Tools' })
    expect(within(tools).getByRole('link', { name: 'Trash' })).toBeInTheDocument()
    expect(within(tools).queryByRole('link', { name: 'Import' })).not.toBeInTheDocument()
    expect(
      within(within(drawer).getByRole('region', { name: 'Operations' })).getByRole('link', {
        name: 'Import',
      }),
    ).toHaveAttribute('href', '/import')
  })

  it('gives search a labelled row and files saved searches under Browse', async () => {
    const user = userEvent.setup()
    renderShell(auth())
    const drawer = await openDrawer(user)

    // On a phone the search page used to have no menu entry at all — its only
    // door was the top bar's unlabelled magnifier, whose shortcut tooltip a
    // touch screen never shows. Here it is a row of the everyday block.
    const main = within(drawer).getByRole('region', { name: 'Main' })
    const search = within(main).getByRole('link', { name: 'Search' })
    expect(search).toHaveAttribute('href', '/search')
    expect(search).toHaveAttribute('title', 'Search the photos')

    // Saved searches are smart albums: they belong beside the favorites, not a
    // dropdown deeper still inside the search page.
    const browse = within(drawer).getByRole('region', { name: 'Browse' })
    expect(within(browse).getByRole('link', { name: 'Saved searches' })).toHaveAttribute(
      'href',
      '/saved',
    )
    // And the leaderboard is demoted out of the everyday block into Browse.
    expect(within(main).queryByRole('link', { name: 'Leaderboard' })).not.toBeInTheDocument()
    expect(within(browse).getByRole('link', { name: 'Leaderboard' })).toBeInTheDocument()
  })

  it('carries the maintainer’s complete set of destinations', async () => {
    const user = userEvent.setup()
    renderShell(auth({ isMaintainer: true }))
    const drawer = await openDrawer(user)

    // The whole navbar, drawer-shaped: the everyday block, browse, the three
    // role-gated clusters and the account block that replaces the user menu.
    expect(drawerHrefs(drawer)).toEqual([
      '/',
      '/albums',
      '/labels',
      '/search',
      '/review',
      '/upload',
      '/favorites',
      '/saved',
      '/people',
      '/places',
      '/map',
      '/leaderboard',
      '/expand',
      '/faces',
      '/recognition',
      '/outliers',
      '/duplicate-markers',
      '/duplicates',
      '/trash',
      '/import',
      '/maintenance',
      '/system',
      '/users',
      '/audit',
      '/account',
      '/stats',
      '/help',
    ])
    // The keyboard-shortcuts overlay and sign-out ride along as rows, so nothing
    // the bar offers is lost on a phone.
    expect(within(drawer).getByRole('button', { name: 'Keyboard shortcuts' })).toBeInTheDocument()
    expect(within(drawer).getByRole('button', { name: 'Sign out' })).toBeInTheDocument()
  })

  it('prints the build version in the account block, above sign-out', async () => {
    const user = userEvent.setup()
    renderShell(auth(), '/', withVersion)
    const drawer = await openDrawer(user)

    const account = within(drawer).getByRole('region', { name: 'Account' })
    const version = within(account).getByText('v0.5.1')
    expect(version).toHaveAttribute('title', 'App version')

    // Above the one destructive action in the drawer, as on the desktop menu.
    const logout = within(account).getByRole('button', { name: 'Sign out' })
    expect(
      version.compareDocumentPosition(logout) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeGreaterThan(0)
  })

  it('shows the version as text, not a tappable row', async () => {
    const user = userEvent.setup()
    renderShell(auth(), '/', withVersion)
    const drawer = await openDrawer(user)

    // Not a link, not a button, not even a row: a paragraph the thumb and the
    // keyboard both pass over. It also stays out of the drawer's link list.
    const version = within(drawer).getByText('v0.5.1')
    expect(version.tagName).toBe('P')
    expect(version).not.toHaveClass('kk-navdrawer__link')
    expect(version.closest('a, button')).toBeNull()
    expect(drawerHrefs(drawer)).not.toContain('v0.5.1')
  })

  it('shows no version when the capabilities call has not answered', async () => {
    const user = userEvent.setup()
    renderShell(auth())
    const drawer = await openDrawer(user)

    // The drawer keeps working; there is simply nothing to print.
    expect(within(drawer).queryByTitle('App version')).not.toBeInTheDocument()
    expect(within(drawer).getByRole('button', { name: 'Sign out' })).toBeInTheDocument()
  })

  it('keeps the role gating of the bar: a viewer sees neither tools nor admin', async () => {
    const user = userEvent.setup()
    renderShell(auth())
    const drawer = await openDrawer(user)

    for (const name of ['Tools', 'Operations', 'Admin']) {
      expect(within(drawer).queryByRole('region', { name })).not.toBeInTheDocument()
    }
    // Browse and the account block stay — they are available to every role.
    expect(within(drawer).getByRole('region', { name: 'Browse' })).toBeInTheDocument()
    expect(drawerHrefs(drawer)).toEqual([
      '/',
      '/albums',
      '/labels',
      '/search',
      '/favorites',
      '/saved',
      '/people',
      '/places',
      '/map',
      '/leaderboard',
      '/account',
      '/stats',
      '/help',
    ])
  })

  it('gives an admin governance but withholds the maintainer operations', async () => {
    const user = userEvent.setup()
    renderShell(auth({ canWrite: true, isAdmin: true }))
    const drawer = await openDrawer(user)

    expect(within(drawer).getByRole('region', { name: 'Admin' })).toBeInTheDocument()
    expect(within(drawer).getByRole('link', { name: 'Users' })).toHaveAttribute('href', '/users')
    expect(within(drawer).queryByRole('region', { name: 'Operations' })).not.toBeInTheDocument()
    expect(within(drawer).queryByRole('link', { name: 'Import' })).not.toBeInTheDocument()
  })

  it('closes itself when the user taps a destination', async () => {
    const user = userEvent.setup()
    renderShell(auth())
    const drawer = await openDrawer(user)

    await user.click(within(drawer).getByRole('link', { name: 'People' }))

    expect(await screen.findByText('people page')).toBeInTheDocument()
    // Navigate = dismiss: the drawer must not stay open over the page it opened.
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })

  it('closes on the close button without navigating anywhere', async () => {
    const user = userEvent.setup()
    renderShell(auth())
    const drawer = await openDrawer(user)

    await user.click(within(drawer).getByRole('button', { name: 'Close the menu' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(screen.getByText('home page')).toBeInTheDocument()
  })

  it('signs out from the account section and leaves for the login page', async () => {
    const logout = vi.fn().mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderShell(auth({ logout }))
    const drawer = await openDrawer(user)

    await user.click(within(drawer).getByRole('button', { name: 'Sign out' }))

    expect(logout).toHaveBeenCalledTimes(1)
    expect(await screen.findByText('login page')).toBeInTheDocument()
  })

  it('highlights the row for the current route, sub-paths included', async () => {
    const user = userEvent.setup()
    renderShell(auth(), '/albums/ab12')
    const drawer = await openDrawer(user)

    const albums = within(drawer).getByRole('link', { name: 'Albums' })
    expect(albums).toHaveClass('active')
    expect(albums).toHaveAttribute('aria-current', 'page')
    // The library row is `end`-matched, so a deep route never lights it up.
    expect(within(drawer).getByRole('link', { name: 'Library' })).not.toHaveClass('active')
  })

  it('gives every row an icon and the action tooltip the bar uses', async () => {
    const user = userEvent.setup()
    renderShell(auth())
    const drawer = await openDrawer(user)

    const map = within(drawer).getByRole('link', { name: 'Map' })
    expect(map).toHaveAttribute('title', 'Show the photos on a map')
    const icon = map.querySelector('i.bi.bi-map')
    expect(icon).not.toBeNull()
    // Icons stay decorative: the visible label is the accessible name.
    expect(icon).toHaveAttribute('aria-hidden', 'true')
  })
})

describe('MobileNavDrawer on desktop', () => {
  beforeEach(() => {
    mockViewport(false)
  })

  it('is not rendered at all — the inline navbar keeps the navigation', async () => {
    const user = userEvent.setup()
    const { container } = renderShell(auth({ isMaintainer: true }))

    await user.click(screen.getByRole('button', { name: /toggle navigation/i }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(container.querySelector('.kk-navdrawer')).toBeNull()
    // The `md`+ bar is untouched: the collapse, its dropdowns and the user menu.
    expect(container.querySelector('.navbar-collapse')).not.toBeNull()
    expect(screen.getByRole('button', { name: 'Tools' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'User One' })).toBeInTheDocument()
    // …and exactly one copy of a nav link, not one per navigation.
    expect(screen.getAllByRole('link', { name: 'Albums' })).toHaveLength(1)
  })
})

/**
 * jsdom evaluates neither `env()` nor the offcanvas geometry, so the parts that
 * keep the drawer off the notch and its rows finger-sized are asserted against
 * the stylesheet itself — the same guard the tab bar gets.
 */
describe('mobile nav drawer stylesheet', () => {
  const css = readCss('src/styles/app.css')

  /** The declarations of the rule matching `prelude`; throws when there is none. */
  function rule(prelude: RegExp, contains?: RegExp): Map<string, string> {
    const body = ruleBody(css, prelude, contains)
    if (body === undefined) {
      throw new Error(`rule not found: ${prelude.source}`)
    }
    return declarations(body)
  }

  it('keeps the panel clear of the notch and the home indicator', () => {
    const drawer = rule(/\.kk-navdrawer\s*(?=\{)/, /--bs-offcanvas-width/)

    // The panel is fixed to the right edge and runs the full height, so it meets
    // three of the four insets; the fourth edge faces the middle of the screen.
    expect(drawer.get('padding-top')).toMatch(/env\(safe-area-inset-top,\s*0px\)/)
    expect(drawer.get('padding-right')).toMatch(/env\(safe-area-inset-right,\s*0px\)/)
    expect(drawer.get('padding-bottom')).toMatch(/env\(safe-area-inset-bottom,\s*0px\)/)
    // Overriding Bootstrap's own width variable keeps the panel narrower than the
    // viewport, so a strip of backdrop is always there to dismiss by.
    expect(drawer.get('--bs-offcanvas-width')).toBe('min(20rem, 86vw)')
  })

  it('gives every row a comfortable finger target', () => {
    const row = rule(/\.kk-navdrawer__link\s*(?=\{)/, /min-height/)
    // 3rem = 48px, past the app's 44px touch floor: these rows are the whole
    // point of the drawer, and the coarse-pointer floor does not reach a plain
    // `<a>`/`<button>` that is not a `.nav-link`.
    expect(row.get('min-height')).toBe('3rem')
    expect(row.get('display')).toBe('flex')
  })

  it('separates the sections with a rule as well as a heading', () => {
    const between = rule(/\.kk-navdrawer__section \+ \.kk-navdrawer__section\s*(?=\{)/)
    expect(between.get('border-top')).toContain('1px solid')
    expect(between.get('padding-top')).toBe('var(--kk-space-4)')
    // The body is the scroller when the menu outgrows the screen, and it keeps
    // that scroll to itself instead of chaining it to the page behind.
    expect(rule(/\.kk-navdrawer__body\s*(?=\{)/).get('overscroll-behavior')).toBe('contain')
  })

  it('lights the current row with the same accent pill as the other navigations', () => {
    const active = rule(/\.kk-navdrawer__link\.active\s*(?=\{)/)
    expect(active.get('background-color')).toBe('var(--kk-accent-subtle)')
    expect(active.get('color')).toBe('var(--kk-accent)')
  })
})

describe('MobileNavDrawer — my photos', () => {
  it('offers the entry only once the account names a person', async () => {
    const user = userEvent.setup()
    renderShell(auth())
    const drawer = await openDrawer(user)

    expect(within(drawer).queryByRole('link', { name: 'My photos' })).not.toBeInTheDocument()
  })

  it('points the entry at the linked person, in the account section', async () => {
    const user = userEvent.setup()
    renderShell(auth({ subjectUid: 'sub123' }))
    const drawer = await openDrawer(user)

    const account = within(drawer).getByRole('region', { name: 'Account' })
    expect(within(account).getByRole('link', { name: 'My photos' })).toHaveAttribute(
      'href',
      '/?person=sub123',
    )
  })
})
