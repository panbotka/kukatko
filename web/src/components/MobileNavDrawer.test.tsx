import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { CAPABILITIES_DEFAULT, CapabilitiesContext } from '../capabilities/CapabilitiesContext'
import i18n from '../i18n'
import { ProjectContext, type ProjectContextValue } from '../project/ProjectContext'
import type { ProjectMembership } from '../services/auth'
import { TaskSummaryContext } from '../tasks/TaskSummaryContext'
import { declarations, readCss, ruleBody } from '../test/css'

import { Layout } from './Layout'

/** Builds an auth context value with the given capabilities. */
function auth(
  opts: {
    /** A curator: curation without the wider write access. */
    canCurate?: boolean
    canWrite?: boolean
    isAdmin?: boolean
    isMaintainer?: boolean
    logout?: () => void
    /** The person of the library this account says it is; omitted means none. */
    subjectUid?: string
  } = {},
): AuthContextValue {
  const { canCurate = false, canWrite = false, isMaintainer = false } = opts
  // A maintainer is admin-or-higher, so it satisfies isAdmin too.
  const isAdmin = opts.isAdmin ?? isMaintainer
  const role = isMaintainer
    ? 'maintainer'
    : isAdmin
      ? 'admin'
      : canWrite
        ? 'editor'
        : canCurate
          ? 'curator'
          : 'viewer'
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
    // What the project overlay of `useAuth()` reads the maintainer from, once
    // a test runs the shell inside a project.
    instanceRole: isMaintainer ? 'maintainer' : 'user',
    downloadToken: null,
    canCurate: canCurate || canWrite || isAdmin,
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

/** An approved membership of the active project `slug`. */
function member(slug: string, name: string): ProjectMembership {
  return { uid: `p-${slug}`, slug, name, active: true, role: 'editor', approved: true }
}

/** The account's two projects: it works in Veselice, and may switch to Rodina. */
const TWO_PROJECTS = [member('v', 'Veselice'), member('rodina', 'Rodina')]

/**
 * Renders the shell with a few real destinations, starting at `path`. With
 * `projects` the shell runs inside a project (the first one), as the app does;
 * without, there is no project context and the switcher is absent.
 */
function renderShell(
  value: AuthContextValue,
  path = '/p/v',
  caps = CAPABILITIES_DEFAULT,
  projects?: ProjectMembership[],
) {
  const routes = (
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route element={<Layout />}>
          <Route path="/p/v" element={<div>home page</div>} />
          <Route path="/p/v/albums" element={<div>albums page</div>} />
          <Route path="/p/v/albums/:uid" element={<div>album detail</div>} />
          <Route path="/p/v/people" element={<div>people page</div>} />
          <Route path="/p/rodina" element={<div>rodina home</div>} />
          <Route path="/p/rodina/albums" element={<div>rodina albums</div>} />
          <Route path="/login" element={<div>login page</div>} />
        </Route>
      </Routes>
    </MemoryRouter>
  )
  const project: ProjectContextValue | null =
    projects === undefined
      ? null
      : {
          project: projects[0],
          projects,
          status: 'ready',
          inAddress: true,
          switchProject: vi.fn(),
        }
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={value}>
        <CapabilitiesContext.Provider value={caps}>
          {project === null ? (
            routes
          ) : (
            <ProjectContext.Provider value={project}>{routes}</ProjectContext.Provider>
          )}
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
  await user.click(screen.getByRole('button', { name: /open the menu/i }))
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
  it('leads with the project: its name, the other projects and the chooser', async () => {
    const user = userEvent.setup()
    renderShell(auth(), '/p/v/albums', CAPABILITIES_DEFAULT, TWO_PROJECTS)
    const drawer = await openDrawer(user)

    const project = within(drawer).getByRole('region', { name: 'Project' })
    // The first block of the drawer, before the everyday one.
    const main = within(drawer).getByRole('region', { name: 'Main' })
    expect(
      project.compareDocumentPosition(main) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeGreaterThan(0)
    expect(within(project).getByText('Veselice')).toBeInTheDocument()
    // Albums is a list every project has, so switching keeps it.
    expect(within(project).getByRole('link', { name: /Rodina/ })).toHaveAttribute(
      'href',
      '/p/rodina/albums',
    )
    expect(within(project).getByRole('link', { name: 'All projects…' })).toHaveAttribute(
      'href',
      '/projects',
    )
  })

  it('closes when a project row is followed', async () => {
    const user = userEvent.setup()
    renderShell(auth(), '/p/v/albums', CAPABILITIES_DEFAULT, TWO_PROJECTS)
    const drawer = await openDrawer(user)

    await user.click(within(drawer).getByRole('link', { name: /Rodina/ }))

    expect(await screen.findByText('rodina albums')).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })

  it('names a lone project without offering a switch', async () => {
    const user = userEvent.setup()
    renderShell(auth(), '/p/v', CAPABILITIES_DEFAULT, [member('v', 'Veselice')])
    const drawer = await openDrawer(user)

    const project = within(drawer).getByRole('region', { name: 'Project' })
    expect(within(project).getByText('Veselice')).toBeInTheDocument()
    expect(within(project).queryByRole('link')).not.toBeInTheDocument()
  })

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
    for (const name of ['Main', 'Browse', 'Tools', 'Account']) {
      expect(within(drawer).getByRole('heading', { level: 2, name })).toBeInTheDocument()
      expect(within(drawer).getByRole('region', { name })).toBeInTheDocument()
    }
    // Administration is one group *inside* the account block, as it is inside the
    // desktop user menu — a labelled sub-region, not a section of its own.
    const account = within(drawer).getByRole('region', { name: 'Account' })
    expect(within(account).getByRole('heading', { level: 3, name: 'Admin' })).toBeInTheDocument()
    const admin = within(account).getByRole('region', { name: 'Admin' })
    // Each destination sits inside its own block, not in one undifferentiated
    // stack: Trash is a tool, Import is administration.
    const tools = within(drawer).getByRole('region', { name: 'Tools' })
    expect(within(tools).getByRole('link', { name: 'Trash' })).toBeInTheDocument()
    expect(within(tools).queryByRole('link', { name: 'Import' })).not.toBeInTheDocument()
    expect(within(admin).getByRole('link', { name: 'Import' })).toHaveAttribute(
      'href',
      '/p/v/import',
    )
    // And there is no separate Operations block left to teach a second menu.
    expect(within(drawer).queryByRole('region', { name: 'Operations' })).not.toBeInTheDocument()
  })

  it('keeps the admin group between the account rows and sign-out', async () => {
    const user = userEvent.setup()
    renderShell(auth({ isMaintainer: true }))
    const drawer = await openDrawer(user)

    const admin = within(drawer).getByRole('region', { name: 'Admin' })
    const help = within(drawer).getByRole('link', { name: 'Help' })
    const logout = within(drawer).getByRole('button', { name: 'Sign out' })

    expect(help.compareDocumentPosition(admin) & Node.DOCUMENT_POSITION_FOLLOWING).toBeGreaterThan(
      0,
    )
    expect(
      admin.compareDocumentPosition(logout) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeGreaterThan(0)
  })

  it('gives search a labelled row and files saved searches under Browse', async () => {
    const user = userEvent.setup()
    renderShell(auth())
    const drawer = await openDrawer(user)

    // On a phone the drawer is the /search page's only door: the tab bar's
    // search tab opens the global palette instead, and the top bar carries no
    // magnifier. Here it is a row of the everyday block, a real navigation.
    const main = within(drawer).getByRole('region', { name: 'Main' })
    const search = within(main).getByRole('link', { name: 'Search' })
    expect(search).toHaveAttribute('href', '/p/v/search')
    expect(search).toHaveAttribute('title', 'Search the photos')
    // …and the only link to it anywhere on the phone shell.
    expect(
      Array.from(document.querySelectorAll('a[href="/p/v/search"]')).filter(
        (link) => !drawer.contains(link),
      ),
    ).toHaveLength(0)

    // Saved searches are smart albums: they belong beside the favorites, not a
    // dropdown deeper still inside the search page.
    const browse = within(drawer).getByRole('region', { name: 'Browse' })
    expect(within(browse).getByRole('link', { name: 'Saved searches' })).toHaveAttribute(
      'href',
      '/p/v/saved',
    )
    // And the leaderboard is demoted out of the everyday block into Browse.
    expect(within(main).queryByRole('link', { name: 'Leaderboard' })).not.toBeInTheDocument()
    expect(within(browse).getByRole('link', { name: 'Leaderboard' })).toBeInTheDocument()
  })

  it('carries the maintainer’s complete set of destinations', async () => {
    const user = userEvent.setup()
    renderShell(auth({ isMaintainer: true }), '/p/v', CAPABILITIES_DEFAULT, TWO_PROJECTS)
    const drawer = await openDrawer(user)

    // The whole navbar, drawer-shaped: the project switcher first, then the
    // everyday block, browse, the editor's tools and the account block that
    // replaces the user menu — administration included, at the end of it
    // exactly as the desktop menu carries it.
    expect(drawerHrefs(drawer)).toEqual([
      '/p/rodina',
      '/projects',
      '/p/v',
      '/p/v/albums',
      '/p/v/labels',
      '/p/v/search',
      '/p/v/review',
      '/p/v/upload',
      '/p/v/favorites',
      '/p/v/saved',
      '/p/v/tasks',
      '/p/v/people',
      '/p/v/places',
      '/p/v/map',
      '/p/v/leaderboard',
      '/p/v/expand',
      '/p/v/faces',
      '/p/v/recognition',
      '/p/v/outliers',
      '/p/v/duplicate-markers',
      '/p/v/upload-links',
      '/p/v/duplicates',
      '/p/v/trash',
      '/account',
      '/p/v/stats',
      '/p/v/help',
      '/p/v/import',
      '/maintenance',
      '/system',
      '/p/v/users',
      '/p/v/audit',
      '/p/v/settings',
    ])
    // The keyboard-shortcuts overlay and sign-out ride along as rows, so nothing
    // the bar offers is lost on a phone.
    expect(within(drawer).getByRole('button', { name: 'Keyboard shortcuts' })).toBeInTheDocument()
    expect(within(drawer).getByRole('button', { name: 'Sign out' })).toBeInTheDocument()
  })

  it('prints the labelled build version in the account block, under sign-out', async () => {
    const user = userEvent.setup()
    renderShell(auth(), '/p/v', withVersion)
    const drawer = await openDrawer(user)

    const account = within(drawer).getByRole('region', { name: 'Account' })
    const version = within(account).getByText('Version 0.5.1')
    expect(version).toHaveAttribute('title', 'App version')

    // A footnote under the last action, as on the desktop menu.
    const logout = within(account).getByRole('button', { name: 'Sign out' })
    expect(
      logout.compareDocumentPosition(version) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeGreaterThan(0)
  })

  it('shows the version as text, not a tappable row', async () => {
    const user = userEvent.setup()
    renderShell(auth(), '/p/v', withVersion)
    const drawer = await openDrawer(user)

    // Not a link, not a button, not even a row: a paragraph the thumb and the
    // keyboard both pass over. It also stays out of the drawer's link list.
    const version = within(drawer).getByText('Version 0.5.1')
    expect(version.tagName).toBe('P')
    expect(version).not.toHaveClass('kk-navdrawer__link')
    expect(version.closest('a, button')).toBeNull()
    expect(drawerHrefs(drawer)).not.toContain('Version 0.5.1')
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
    // No orphan heading either: a viewer's account block ends where it always did.
    expect(within(drawer).queryByRole('heading', { name: 'Admin' })).not.toBeInTheDocument()
    // Browse, Upload and the account block stay — they are available to every
    // role. The review game beside Upload is curation and is not there.
    expect(within(drawer).getByRole('region', { name: 'Browse' })).toBeInTheDocument()
    expect(drawerHrefs(drawer)).toEqual([
      '/p/v',
      '/p/v/albums',
      '/p/v/labels',
      '/p/v/search',
      '/p/v/upload',
      '/p/v/favorites',
      '/p/v/saved',
      '/p/v/tasks',
      '/p/v/people',
      '/p/v/places',
      '/p/v/map',
      '/p/v/leaderboard',
      '/account',
      '/p/v/stats',
      '/p/v/help',
    ])
  })

  it('gives a curator review, upload and the curation tools, not duplicates or trash', async () => {
    const user = userEvent.setup()
    renderShell(auth({ canCurate: true }))
    const drawer = await openDrawer(user)

    const hrefs = drawerHrefs(drawer)
    for (const href of [
      '/p/v/review',
      '/p/v/upload',
      '/p/v/expand',
      '/p/v/faces',
      '/p/v/duplicate-markers',
    ]) {
      expect(hrefs).toContain(href)
    }
    for (const href of ['/p/v/duplicates', '/p/v/trash']) {
      expect(hrefs).not.toContain(href)
    }
    expect(within(drawer).getByRole('region', { name: 'Tools' })).toBeInTheDocument()
    expect(within(drawer).queryByRole('region', { name: 'Admin' })).not.toBeInTheDocument()
  })

  it('gives an admin governance but withholds the maintainer operations', async () => {
    const user = userEvent.setup()
    renderShell(auth({ canWrite: true, isAdmin: true }))
    const drawer = await openDrawer(user)

    // The group is gated row by row, so an admin gets it — with two rows in it.
    const admin = within(drawer).getByRole('region', { name: 'Admin' })
    expect(within(admin).getByRole('link', { name: 'Users' })).toHaveAttribute('href', '/p/v/users')
    expect(within(admin).getByRole('link', { name: 'Audit' })).toHaveAttribute('href', '/p/v/audit')
    expect(within(admin).getByRole('link', { name: 'Settings' })).toHaveAttribute(
      'href',
      '/p/v/settings',
    )
    for (const name of ['Import', 'Maintenance', 'System']) {
      expect(within(drawer).queryByRole('link', { name })).not.toBeInTheDocument()
    }
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
    renderShell(auth(), '/p/v/albums/ab12')
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

    await user.click(screen.getByRole('button', { name: /open the menu/i }))

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
    // The nested admin group is fenced on both sides instead: the rule above ends
    // the account rows, the one below keeps sign-out out of the group.
    const sub = rule(/\.kk-navdrawer__subsection\s*(?=\{)/)
    expect(sub.get('border-top')).toContain('1px solid')
    expect(sub.get('border-bottom')).toContain('1px solid')
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
      '/p/v?person=sub123',
    )
  })
})

describe('MobileNavDrawer — the tasks-waiting badge', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
    mockViewport(true)
  })

  it('wears the count on the Tasks row, the same one the hamburger showed', async () => {
    const user = userEvent.setup()
    render(
      <I18nextProvider i18n={i18n}>
        <AuthContext.Provider value={auth()}>
          <TaskSummaryContext.Provider
            value={{
              summary: {
                by_state: { question: 4, working: 0, review: 0, done: 0, rejected: 0 },
                open: 4,
                waiting_on_me: 4,
                answered: 0,
              },
              refresh: vi.fn(),
            }}
          >
            <MemoryRouter initialEntries={['/p/v']}>
              <Routes>
                <Route element={<Layout />}>
                  <Route path="/p/v" element={<div>home page</div>} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TaskSummaryContext.Provider>
        </AuthContext.Provider>
      </I18nextProvider>,
    )

    await user.click(screen.getByRole('button', { name: 'Open the menu' }))
    const drawer = await screen.findByRole('dialog')
    const row = within(drawer).getByRole('link', { name: /Tasks/ })
    expect(within(row).getByTestId('waiting-badge')).toHaveTextContent('4')
    expect(within(row).getByText('4 tasks wait on you')).toBeInTheDocument()
  })
})
