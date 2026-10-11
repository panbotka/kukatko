import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { NAV_DRAWER_QUERY } from '../hooks/useIsNarrowViewport'
import i18n from '../i18n'
import { declarations, readCss, ruleBody, zIndexOf } from '../test/css'

import { Layout } from './Layout'
import { MobileTabBar } from './MobileTabBar'

// The palette the search tab opens asks for the query schema and the recent
// searches as it mounts; neither may reach for the network here.
vi.mock('../services/search', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/search')>()
  return { ...actual, globalSearch: vi.fn(), fetchQuerySchema: vi.fn() }
})

vi.mock('../services/searchHistory', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/searchHistory')>()
  return {
    ...actual,
    fetchSearchHistory: vi.fn(),
    recordSearch: vi.fn(),
    clearSearchHistory: vi.fn(),
  }
})

/** A minimal signed-in auth context; `canCurate` decides the Upload tab. */
function auth(role: 'viewer' | 'curator' | 'editor'): AuthContextValue {
  return {
    status: 'authenticated',
    user: {
      uid: 'u1',
      username: 'u',
      display_name: 'User One',
      role,
    },
    role,
    downloadToken: null,
    canCurate: role !== 'viewer',
    canWrite: role === 'editor',
    isAdmin: false,
    isMaintainer: false,
    canImport: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

/**
 * Points `window.matchMedia` at a fixed phone/desktop answer. The shared test
 * setup stubs a non-matching (desktop) `matchMedia`; the phone cases override it
 * so `useIsNarrowViewport` takes its narrow branch.
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

const { fetchQuerySchema } = await import('../services/search')
const { fetchSearchHistory } = await import('../services/searchHistory')

/** Renders the bar alone at `path`, so the active tab is the one under test. */
function renderBar(role: 'viewer' | 'curator' | 'editor' = 'editor', path = '/') {
  const onOpenSearch = vi.fn()
  const view = render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(role)}>
        <MemoryRouter initialEntries={[path]}>
          <MobileTabBar onOpenSearch={onOpenSearch} />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
  return { ...view, onOpenSearch }
}

/** Prints the current location, so a test can prove nothing navigated. */
function LocationDisplay() {
  const location = useLocation()
  return <div data-testid="loc">{`${location.pathname}${location.search}`}</div>
}

/** Renders the whole shell, to pin where the bar does (and does not) appear. */
function renderShell(path = '/') {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth('editor')}>
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route element={<Layout />}>
              <Route path="*" element={<LocationDisplay />} />
            </Route>
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/** The tab bar's tabs, links and the search button alike, in slot order. */
function tabsOf(bar: HTMLElement): HTMLElement[] {
  return Array.from(bar.querySelectorAll<HTMLElement>('.kk-tabbar__tab'))
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.mocked(fetchQuerySchema).mockResolvedValue([])
  vi.mocked(fetchSearchHistory).mockResolvedValue([])
})

afterEach(() => {
  // Restore the shared desktop default so later tests never inherit a phone.
  mockViewport(false)
})

describe('MobileTabBar on a phone', () => {
  beforeEach(() => {
    mockViewport(true)
  })

  it('puts the everyday destinations one thumb-reach away', () => {
    renderBar()

    const bar = screen.getByRole('navigation', { name: 'Primary navigation' })
    // The library is the homepage, so its tab points at the site root.
    const links = ['Library', '/', 'Albums', '/albums', 'Upload', '/upload']
    for (let i = 0; i < links.length; i += 2) {
      expect(screen.getByRole('link', { name: links[i] })).toHaveAttribute('href', links[i + 1])
    }
    // Four tabs at most: the bar is the everyday loop, not a second navbar, so
    // browse / review / tools / admin stay in the hamburger menu.
    expect(tabsOf(bar).map((tab) => tab.textContent)).toEqual([
      'Library',
      'Albums',
      'Search',
      'Upload',
    ])
    expect(screen.queryByRole('link', { name: 'People' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Review' })).not.toBeInTheDocument()
  })

  it('spends the third slot on search rather than labels', () => {
    renderBar()

    // Browsing by label is the rarer errand and keeps its row in the drawer.
    const bar = screen.getByRole('navigation', { name: 'Primary navigation' })
    const search = screen.getByRole('button', { name: 'Search' })
    expect(tabsOf(bar)[2]).toBe(search)
    expect(search.querySelector('i.bi.bi-search')).not.toBeNull()
    expect(screen.queryByRole('link', { name: 'Labels' })).not.toBeInTheDocument()
  })

  it('opens the global search palette from the search tab, not the /search page', async () => {
    const user = userEvent.setup()
    const { onOpenSearch } = renderBar()

    // One search on a phone: the tab is the palette's door (the top bar drops
    // its magnifier), and the /search page stays a drawer row. So the tab is a
    // dialog-opening button, not a link anywhere.
    expect(screen.queryByRole('link', { name: 'Search' })).not.toBeInTheDocument()
    const search = screen.getByRole('button', { name: 'Search' })
    expect(search).toHaveAttribute('aria-haspopup', 'dialog')
    expect(search).toHaveAttribute(
      'title',
      'Search the whole library: photos, people, albums, labels',
    )
    await user.click(search)
    expect(onOpenSearch).toHaveBeenCalledTimes(1)
  })

  it('gives every tab an icon, a label and an action tooltip', () => {
    renderBar()

    const albums = screen.getByRole('link', { name: 'Albums' })
    expect(albums).toHaveAttribute('title', 'Show the albums')
    // Icons are decorative bootstrap-icons glyphs beside the visible label.
    const icon = albums.querySelector('i.bi.bi-collection')
    expect(icon).not.toBeNull()
    expect(icon).toHaveAttribute('aria-hidden', 'true')
    expect(albums.querySelector('.kk-tabbar__label')).toHaveTextContent('Albums')
  })

  it('highlights the tab for the current route, sub-paths included', () => {
    renderBar('editor', '/albums/ab12')

    // react-router marks the matching NavLink; the album detail page still reads
    // as "you are in Albums".
    const albums = screen.getByRole('link', { name: 'Albums' })
    expect(albums).toHaveClass('active')
    expect(albums).toHaveAttribute('aria-current', 'page')
    // The library tab is `end`-matched, so a deep route never lights it up.
    expect(screen.getByRole('link', { name: 'Library' })).not.toHaveClass('active')
  })

  it('marks the library tab active on the site root only', () => {
    renderBar('editor', '/')
    expect(screen.getByRole('link', { name: 'Library' })).toHaveClass('active')
    expect(screen.getByRole('link', { name: 'Albums' })).not.toHaveClass('active')
  })

  it('offers a curator the Upload tab, which is curation', () => {
    renderBar('curator')

    expect(screen.getByRole('link', { name: 'Upload' })).toHaveAttribute('href', '/upload')
  })

  it('gives a viewer the Upload tab too: all four tabs for every role', () => {
    renderBar('viewer')

    expect(screen.getByRole('link', { name: 'Upload' })).toHaveAttribute('href', '/upload')
    expect(screen.getByRole('link', { name: 'Library' })).toBeInTheDocument()
    expect(tabsOf(screen.getByRole('navigation', { name: 'Primary navigation' }))).toHaveLength(4)
  })

  it('publishes its height so the rest of the shell can keep clear of it', () => {
    const { unmount } = renderBar()

    // The value itself is 0px under jsdom (no layout); what matters is that the
    // variable exists while the bar does, and is torn down with it — that is what
    // makes the page clearance and the batch bar's offset collapse to nothing.
    expect(document.documentElement.style.getPropertyValue('--kk-tabbar-height')).not.toBe('')
    unmount()
    expect(document.documentElement.style.getPropertyValue('--kk-tabbar-height')).toBe('')
  })

  it('appears in the layout shell alongside the top navbar', () => {
    const { container } = renderShell()

    expect(container.querySelector('.kk-tabbar')).not.toBeNull()
    // The top bar stays — the tab bar adds a second navigation, it does not
    // replace the hamburger menu.
    expect(container.querySelector('.navbar.kukatko-navbar')).not.toBeNull()
  })

  it('leaves the top bar without a magnifier, so the phone has one search', () => {
    const { container } = renderShell()

    expect(container.querySelector('.kukatko-search-trigger')).toBeNull()
    const navbar = container.querySelector<HTMLElement>('.navbar.kukatko-navbar')
    expect(navbar).not.toBeNull()
    expect(within(navbar ?? document.body).queryByRole('button', { name: 'Search' })).toBeNull()
    // The lone hamburger holds the trailing edge, under the thumb.
    expect(container.querySelector('.navbar-toggler')).toHaveClass('ms-auto')
  })

  it('opens the palette from the search tab without navigating anywhere', async () => {
    const user = userEvent.setup()
    renderShell('/albums')

    const bar = screen.getByRole('navigation', { name: 'Primary navigation' })
    await user.click(within(bar).getByRole('button', { name: 'Search' }))
    const input = await screen.findByRole('combobox')
    // The palette is state, not a route: the page underneath keeps its URL, so
    // closing it leaves nothing on the history stack for Back to walk through.
    expect(screen.getByTestId('loc')).toHaveTextContent('/albums')

    fireEvent.keyDown(input, { key: 'Escape' })
    await waitFor(() => {
      expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
    })
    expect(screen.getByTestId('loc')).toHaveTextContent('/albums')
  })
})

describe('MobileTabBar on desktop', () => {
  it('renders nothing at all', () => {
    // The shared setup already reports a wide viewport.
    const { container } = renderBar()

    expect(container).toBeEmptyDOMElement()
    expect(screen.queryByRole('navigation', { name: 'Primary navigation' })).not.toBeInTheDocument()
    // Nothing reserves bottom clearance for a bar that is not there.
    expect(document.documentElement.style.getPropertyValue('--kk-tabbar-height')).toBe('')
  })

  it('leaves the layout shell with the top navbar as its sole navigation', () => {
    const { container } = renderShell()

    expect(container.querySelector('.kk-tabbar')).toBeNull()
    // …and therefore exactly one "Albums" link in the DOM, not two.
    expect(screen.getAllByRole('link', { name: 'Albums' })).toHaveLength(1)
    // Desktop is unchanged: the magnifier still leads the bar.
    expect(container.querySelector('.kukatko-search-trigger')).not.toBeNull()
  })
})

/**
 * Which viewports get the bar at all. It follows the navbar's expand breakpoint
 * rather than the app's phone one: from `lg` down the navigation is folded into
 * the hamburger (a portrait tablet included, where the inline bar used to run off
 * the screen), and wherever that is true the everyday destinations owe the reader
 * a thumb-level strip.
 */
describe('MobileTabBar breakpoint', () => {
  it('asks the navigation breakpoint, not the page one', () => {
    mockViewport(true)
    const { container } = renderBar()

    // The JS decision and its CSS twin have to name the same width, or a resize
    // shows the bar on a desktop for a frame (or hides it on a tablet).
    expect(window.matchMedia).toHaveBeenCalledWith(NAV_DRAWER_QUERY)
    expect(NAV_DRAWER_QUERY).toContain('991.98px')
    expect(container.querySelector('.kk-tabbar')).toHaveClass('d-lg-none')
  })
})

/**
 * jsdom evaluates neither `env()` nor `@supports`, so the geometry that keeps the
 * bar off the home indicator — and keeps everything else off the bar — is asserted
 * against the stylesheet itself.
 */
describe('mobile tab bar stylesheet', () => {
  const css = readCss('src/styles/app.css')

  /** The declarations of the rule matching `prelude`; throws when there is none. */
  function rule(prelude: RegExp, contains?: RegExp): Map<string, string> {
    const body = ruleBody(css, prelude, contains)
    if (body === undefined) {
      throw new Error(`rule not found: ${prelude.source}`)
    }
    return declarations(body)
  }

  it('pins the bar to the bottom edge and clears the home indicator', () => {
    const bar = rule(/\.kk-tabbar\s*(?=\{)/, /position/)

    expect(bar.get('position')).toBe('fixed')
    expect(bar.get('bottom')).toBe('0')
    // The bar carries the home-indicator inset itself, so the tabs never sit
    // under it — mirroring how `.kk-batch-dock` does it.
    expect(bar.get('padding-bottom')).toMatch(/env\(safe-area-inset-bottom,\s*0px\)/)
    // A landscape notch takes a side column, so the row inset on both sides too.
    expect(bar.get('padding-left')).toMatch(/env\(safe-area-inset-left,\s*0px\)/)
    expect(bar.get('padding-right')).toMatch(/env\(safe-area-inset-right,\s*0px\)/)
  })

  it('gives every tab a finger-sized target', () => {
    const tab = rule(/\.kk-tabbar__tab\s*(?=\{)/, /min-height/)
    // 2.75rem = the app's 44px touch floor. Declared on the tab itself: the
    // app-wide `pointer: coarse` floor enumerates controls by class and does not
    // reach a plain `<a>`.
    expect(tab.get('min-height')).toBe('2.75rem')
  })

  it('reserves page scroll clearance for the bar, and none without it', () => {
    // The document reserves the room, so no page can forget to — through the
    // bottom-edge token, which is the bar's height wherever the bar is mounted…
    expect(rule(/\bbody\s*(?=\{)/, /overflow-x/).get('padding-bottom')).toBe(
      'var(--kk-bottom-edge)',
    )
    // …and the variable is `0px` until a bar publishes its real height, which is
    // what makes the reservation collapse to nothing on desktop.
    expect(rule(/:root\s*(?=\{)/, /--kk-tabbar-height/).get('--kk-tabbar-height')).toBe('0px')
  })

  it('stacks the floating batch bar above the tabs instead of over them', () => {
    // `--kk-bottom-edge` is the taken bottom edge: the tab bar's height where
    // there is one, the bare safe-area inset where there is not.
    const root = rule(/:root\s*(?=\{)/, /--kk-bottom-edge/)
    expect(root.get('--kk-bottom-edge')).toBe(
      'max(env(safe-area-inset-bottom, 0px), var(--kk-tabbar-height))',
    )
    // The dock rises above it…
    expect(rule(/\.kk-batch-dock\s*(?=\{)/, /position/).get('bottom')).toContain(
      'var(--kk-bottom-edge)',
    )
    // …and the scroll clearance a selecting page reserves grows with it, so the
    // last photo row still scrolls clear of both bars.
    expect(root.get('--kk-batch-clearance')).toContain('var(--kk-bottom-edge)')
    // The dock also has to win the paint order where the two ever meet. Both
    // read their layer from the stylesheet's `--kk-*-z` scale, so compare the
    // resolved values rather than the `var()` references.
    const barZ = zIndexOf(css, /\.kk-tabbar\s*(?=\{)/, /position/)
    const dockZ = zIndexOf(css, /\.kk-batch-dock\s*(?=\{)/, /position/)
    expect(barZ).toBeLessThan(dockZ)
  })

  it('stops the timeline rail above the tabs', () => {
    // The rail runs to the bottom edge; without this its oldest ticks would be
    // unreachable behind the tabs. The bottom-edge token is the bar's height
    // wherever the bar is mounted.
    expect(rule(/\.kukatko-timeline\s*(?=\{)/, /position/).get('bottom')).toContain(
      'var(--kk-bottom-edge)',
    )
  })
})
