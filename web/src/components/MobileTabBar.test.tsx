import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { declarations, readCss, ruleBody, zIndexOf } from '../test/css'

import { Layout } from './Layout'
import { MobileTabBar } from './MobileTabBar'

/** A minimal signed-in auth context; `canWrite` decides the Upload tab. */
function auth(canWrite: boolean): AuthContextValue {
  return {
    status: 'authenticated',
    user: {
      uid: 'u1',
      username: 'u',
      display_name: 'User One',
      role: canWrite ? 'editor' : 'viewer',
    },
    role: canWrite ? 'editor' : 'viewer',
    downloadToken: null,
    canWrite,
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

/** Renders the bar alone at `path`, so the active tab is the one under test. */
function renderBar(canWrite = true, path = '/') {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <MemoryRouter initialEntries={[path]}>
          <MobileTabBar />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/** Renders the whole shell, to pin where the bar does (and does not) appear. */
function renderShell(path = '/') {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(true)}>
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route element={<Layout />}>
              <Route path="*" element={<div>page content</div>} />
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
    const tabs = ['Library', '/', 'Albums', '/albums', 'Search', '/search', 'Upload', '/upload']
    for (let i = 0; i < tabs.length; i += 2) {
      expect(screen.getByRole('link', { name: tabs[i] })).toHaveAttribute('href', tabs[i + 1])
    }
    // Four tabs at most: the bar is the everyday loop, not a second navbar, so
    // browse / review / tools / admin stay in the hamburger menu.
    expect(bar.querySelectorAll('a')).toHaveLength(4)
    expect(screen.queryByRole('link', { name: 'People' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Review' })).not.toBeInTheDocument()
  })

  it('spends the third slot on search rather than labels', () => {
    renderBar()

    // Searching is what the top bar could only offer through an unlabelled
    // magnifier a thumb never hovers; browsing by label is the rarer errand and
    // keeps its row in the drawer.
    const search = screen.getByRole('link', { name: 'Search' })
    expect(search).toHaveAttribute('title', 'Search the photos')
    expect(search.querySelector('i.bi.bi-search')).not.toBeNull()
    expect(screen.queryByRole('link', { name: 'Labels' })).not.toBeInTheDocument()
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
    renderBar(true, '/albums/ab12')

    // react-router marks the matching NavLink; the album detail page still reads
    // as "you are in Albums".
    const albums = screen.getByRole('link', { name: 'Albums' })
    expect(albums).toHaveClass('active')
    expect(albums).toHaveAttribute('aria-current', 'page')
    // The library tab is `end`-matched, so a deep route never lights it up.
    expect(screen.getByRole('link', { name: 'Library' })).not.toHaveClass('active')
  })

  it('marks the library tab active on the site root only', () => {
    renderBar(true, '/')
    expect(screen.getByRole('link', { name: 'Library' })).toHaveClass('active')
    expect(screen.getByRole('link', { name: 'Albums' })).not.toHaveClass('active')
  })

  it('hides the write-gated Upload tab from a viewer', () => {
    renderBar(false)

    expect(screen.queryByRole('link', { name: 'Upload' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Library' })).toBeInTheDocument()
    expect(
      screen.getByRole('navigation', { name: 'Primary navigation' }).querySelectorAll('a'),
    ).toHaveLength(3)
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
    // The top bar is untouched — the tab bar adds a second navigation, it does
    // not replace the hamburger menu.
    expect(container.querySelector('.navbar.kukatko-navbar')).not.toBeNull()
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
    // The document reserves the room, so no page can forget to…
    expect(rule(/\bbody\s*(?=\{)/, /overflow-x/).get('padding-bottom')).toBe(
      'var(--kk-tabbar-height)',
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
    // unreachable behind the tabs.
    expect(rule(/\.kukatko-timeline\s*(?=\{)/, /position/).get('bottom')).toContain(
      'var(--kk-tabbar-height)',
    )
  })
})
