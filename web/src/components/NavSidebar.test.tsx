import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { CAPABILITIES_DEFAULT, CapabilitiesContext } from '../capabilities/CapabilitiesContext'
import i18n from '../i18n'
import { writeNavPosition } from '../lib/navPosition'
import {
  NOTIFICATION_INBOX_DEFAULT,
  NotificationInboxContext,
} from '../notifications/NotificationInboxContext'
import { ProjectContext, type ProjectContextValue } from '../project/ProjectContext'
import type { ProjectMembership } from '../services/auth'
import type { TaskSummary } from '../services/tasks'
import { TaskSummaryContext, type TaskSummaryState } from '../tasks/TaskSummaryContext'

import { Layout } from './Layout'
import { NavPositionSwitcher } from './NavPositionSwitcher'

/** A signed-in account of `uid`: a maintainer by default, the widest menu. */
function auth(uid = 'u1', opts: { maintainer?: boolean } = {}): AuthContextValue {
  const maintainer = opts.maintainer ?? true
  return {
    status: 'authenticated',
    user: {
      uid,
      username: uid,
      display_name: 'User One',
      role: maintainer ? 'maintainer' : 'viewer',
      subject_uid: 'sub1',
    },
    role: maintainer ? 'maintainer' : 'viewer',
    downloadToken: null,
    canCurate: maintainer,
    canWrite: maintainer,
    isAdmin: maintainer,
    isMaintainer: maintainer,
    canImport: maintainer,
    instanceRole: maintainer ? 'maintainer' : 'user',
    projects: [],
    pictureVersion: 0,
    pictureChanged: vi.fn(),
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

/** A membership of `role` in a project; the reader is the person `sub1` in it. */
function member(slug: string, name: string, role = 'admin'): ProjectMembership {
  return {
    uid: `p-${slug}`,
    slug,
    name,
    active: true,
    role,
    approved: true,
    subject_uid: 'sub1',
  } as ProjectMembership
}

/** Two tasks wait on the reader, so the Úkoly badge has something to show. */
const TASKS: TaskSummaryState = {
  summary: { waiting_on_me: 2 } as TaskSummary,
  refresh: vi.fn(),
}

/**
 * The shell inside project `v` at `/p/v`, its page being `page` (by default a
 * placeholder). Everything the sidebar and the bar read is provided.
 */
function renderShell(
  value: AuthContextValue = auth(),
  page = <div>page content</div>,
  role = 'admin',
) {
  const projects = [member('v', 'Veselice', role), member('rodina', 'Rodina', role)]
  const project: ProjectContextValue = {
    project: projects[0],
    projects,
    status: 'ready',
    inAddress: true,
    switchProject: vi.fn(),
  }
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={value}>
        <CapabilitiesContext.Provider
          value={{ ...CAPABILITIES_DEFAULT, version: { version: '0.5.1', commit: 'abc' } }}
        >
          <NotificationInboxContext.Provider
            value={{ ...NOTIFICATION_INBOX_DEFAULT, unread: 3, unreadByProject: null }}
          >
            <TaskSummaryContext.Provider value={TASKS}>
              <ProjectContext.Provider value={project}>
                <MemoryRouter initialEntries={['/p/v']}>
                  <Routes>
                    <Route element={<Layout />}>
                      <Route path="/p/v" element={page} />
                    </Route>
                  </Routes>
                </MemoryRouter>
              </ProjectContext.Provider>
            </TaskSummaryContext.Provider>
          </NotificationInboxContext.Provider>
        </CapabilitiesContext.Provider>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/** Pins `window.matchMedia` to a phone (`narrow`) or a desktop answer. */
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

/** The sidebar's root, or null when the shell drew the top bar. */
function sidebar(container: HTMLElement): HTMLElement | null {
  return container.querySelector<HTMLElement>('.kk-sidebar')
}

/**
 * Every destination `root` links to, in document order — the project's own
 * identity link aside, which is where you stand rather than somewhere to go.
 */
function destinations(root: HTMLElement): string[] {
  return (
    Array.from(root.querySelectorAll('a[href]'))
      .filter((link) => link.getAttribute('data-testid') !== 'project-identity')
      .map((link) => link.getAttribute('href') ?? '')
      // The bar's dropdown toggles are `#` anchors: doors to a list, not places.
      .filter((href) => href !== '#')
  )
}

beforeEach(async () => {
  window.localStorage.clear()
  await i18n.changeLanguage('en')
  mockViewport(false)
})

describe('navigation position', () => {
  it('draws the top bar by default', () => {
    const { container } = renderShell()

    expect(container.querySelector('.kukatko-navbar')).not.toBeNull()
    expect(sidebar(container)).toBeNull()
    expect(container.querySelector('.kukatko-shell--sidebar')).toBeNull()
  })

  it('draws the sidebar, and no top bar, for an account that chose the left', () => {
    writeNavPosition('u1', 'left')
    const { container } = renderShell()

    const aside = sidebar(container)
    expect(aside).not.toBeNull()
    expect(container.querySelector('.kukatko-navbar')).toBeNull()
    expect(container.querySelector('.kukatko-shell--sidebar')).not.toBeNull()
    // The page still renders, in its own column beside the sidebar.
    expect(screen.getByText('page content')).toBeInTheDocument()
  })

  it('keeps another account’s choice to that account', () => {
    writeNavPosition('u2', 'left')
    const { container } = renderShell(auth('u1'))

    expect(sidebar(container)).toBeNull()
  })

  it('moves the shell the moment the account page switch is clicked, and remembers it', async () => {
    const user = userEvent.setup()
    const { container } = renderShell(auth(), <NavPositionSwitcher />)
    expect(screen.getByRole('button', { name: 'Top' })).toHaveAttribute('aria-pressed', 'true')

    await user.click(screen.getByRole('button', { name: 'Left' }))

    expect(sidebar(container)).not.toBeNull()
    expect(container.querySelector('.kukatko-navbar')).toBeNull()
    expect(screen.getByRole('button', { name: 'Left' })).toHaveAttribute('aria-pressed', 'true')
    expect(window.localStorage.getItem('kukatko.navPosition.u1')).toBe('left')

    await user.click(screen.getByRole('button', { name: 'Top' }))
    expect(sidebar(container)).toBeNull()
    expect(container.querySelector('.kukatko-navbar')).not.toBeNull()
  })

  it('keeps the top bar when storage refuses to work', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    const { container } = renderShell()

    expect(sidebar(container)).toBeNull()
    expect(container.querySelector('.kukatko-navbar')).not.toBeNull()
  })

  it('ignores the choice on a phone: the tab bar and the drawer, never a sidebar', () => {
    writeNavPosition('u1', 'left')
    mockViewport(true)
    const { container } = renderShell()

    expect(sidebar(container)).toBeNull()
    expect(screen.getByRole('button', { name: /open the menu/i })).toBeInTheDocument()
    expect(screen.getByRole('navigation', { name: 'Primary navigation' })).toBeInTheDocument()
  })
})

describe('NavSidebar', () => {
  it('offers exactly the top bar’s destinations, in the top bar’s order', async () => {
    const user = userEvent.setup()

    // The top bar, every dropdown opened once so its entries are in the DOM.
    const top = renderShell()
    const bar = top.container.querySelector<HTMLElement>('.kukatko-navbar')
    if (bar === null) throw new Error('no top bar')
    await user.click(within(bar).getByRole('button', { name: /Browse/ }))
    await user.click(within(bar).getByRole('button', { name: /Tools/ }))
    await user.click(within(bar).getByRole('button', { name: /User One/ }))
    const barRoutes = destinations(bar)
    top.unmount()

    // The sidebar: its groups are always in the DOM; the account menu opens.
    writeNavPosition('u1', 'left')
    const left = renderShell()
    const aside = sidebar(left.container)
    if (aside === null) throw new Error('no sidebar')
    await user.click(within(aside).getByRole('button', { name: /User One/ }))
    const sidebarRoutes = destinations(aside)

    expect(barRoutes.length).toBeGreaterThan(20)
    expect(barRoutes).toContain('/projects')
    expect(barRoutes.some((route) => route.endsWith('?person=sub1'))).toBe(true)
    expect(sidebarRoutes).toEqual(barRoutes)
  })

  it('carries the bar’s controls: identity, search, bell, shortcuts and the account', () => {
    writeNavPosition('u1', 'left')
    const { container } = renderShell()
    const aside = sidebar(container)
    if (aside === null) throw new Error('no sidebar')

    expect(within(aside).getByTestId('project-identity')).toHaveAttribute('href', '/p/v')
    expect(within(aside).getByRole('button', { name: 'Search' })).toBeInTheDocument()
    expect(within(aside).getByRole('button', { name: /notifications/i })).toBeInTheDocument()
    expect(within(aside).getByRole('button', { name: /keyboard shortcuts/i })).toBeInTheDocument()
    expect(within(aside).getByRole('button', { name: /User One/ })).toBeInTheDocument()
  })

  it('lights the current page and shows the tasks count on Úkoly', () => {
    writeNavPosition('u1', 'left')
    const { container } = renderShell()
    const aside = sidebar(container)
    if (aside === null) throw new Error('no sidebar')

    expect(within(aside).getByRole('link', { name: 'Library' })).toHaveClass('active')
    expect(within(aside).getByRole('link', { name: 'Albums' })).not.toHaveClass('active')
    const tasks = within(aside).getByRole('link', { name: /Tasks/ })
    expect(within(tasks).getByTestId('waiting-badge')).toHaveTextContent('2')
  })

  it('folds a group behind a toggle that says whether it is open', async () => {
    const user = userEvent.setup()
    writeNavPosition('u1', 'left')
    const { container } = renderShell()
    const aside = sidebar(container)
    if (aside === null) throw new Error('no sidebar')

    // Browse starts open, the tools start folded.
    const browse = within(aside).getByRole('button', { name: /Browse/ })
    const tools = within(aside).getByRole('button', { name: /Tools/ })
    expect(browse).toHaveAttribute('aria-expanded', 'true')
    expect(tools).toHaveAttribute('aria-expanded', 'false')
    expect(within(aside).queryByRole('link', { name: 'Trash' })).not.toBeInTheDocument()

    await user.click(tools)
    expect(tools).toHaveAttribute('aria-expanded', 'true')
    expect(within(aside).getByRole('link', { name: 'Trash' })).toBeInTheDocument()

    // Folding Browse moves the tasks count onto its toggle.
    await user.click(browse)
    expect(browse).toHaveAttribute('aria-expanded', 'false')
    expect(within(aside).queryByRole('link', { name: /Tasks/ })).not.toBeInTheDocument()
    expect(within(browse).getByTestId('waiting-badge')).toHaveTextContent('2')
  })

  it('hides the tools and the administration from a viewer, as the bar does', async () => {
    const user = userEvent.setup()
    writeNavPosition('u1', 'left')
    const { container } = renderShell(
      auth('u1', { maintainer: false }),
      <div>page content</div>,
      'viewer',
    )
    const aside = sidebar(container)
    if (aside === null) throw new Error('no sidebar')

    expect(within(aside).queryByRole('button', { name: /Tools/ })).not.toBeInTheDocument()
    expect(within(aside).getByRole('link', { name: 'Upload' })).toBeInTheDocument()
    await user.click(within(aside).getByRole('button', { name: /User One/ }))
    expect(screen.queryByRole('link', { name: 'Maintenance' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /sign out/i })).toBeInTheDocument()
  })
})
