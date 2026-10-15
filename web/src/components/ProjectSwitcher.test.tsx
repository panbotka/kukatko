import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import Navbar from 'react-bootstrap/Navbar'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import {
  NOTIFICATION_INBOX_DEFAULT,
  NotificationInboxContext,
} from '../notifications/NotificationInboxContext'
import { ProjectContext, type ProjectContextValue } from '../project/ProjectContext'
import type { ProjectMembership } from '../services/auth'
import type { UnreadByProject } from '../services/notifications'

import { ProjectSwitcher, type ProjectSwitcherProps } from './ProjectSwitcher'

/** An approved membership of an active project `slug`, overridable. */
function member(slug: string, extra: Partial<ProjectMembership> = {}): ProjectMembership {
  return {
    uid: `p-${slug}`,
    slug,
    name: slug === 'v' ? 'Veselice' : `Projekt ${slug}`,
    active: true,
    role: 'viewer',
    approved: true,
    ...extra,
  }
}

/** A provider value for an account in `projects`, working in the first one. */
function value(projects: ProjectMembership[], inAddress = true): ProjectContextValue {
  return { project: projects[0], projects, status: 'ready', inAddress, switchProject: vi.fn() }
}

/**
 * Mounts the switcher inside a navbar at `path`, as the shell does, with
 * `unreadByProject` as the inbox provider's per-project counts.
 */
function renderSwitcher(
  ctx: ProjectContextValue | null,
  props: ProjectSwitcherProps = { variant: 'bar' },
  path = '/p/v/albums',
  unreadByProject: UnreadByProject | null = null,
) {
  const switcher = <ProjectSwitcher {...props} />
  return render(
    <I18nextProvider i18n={i18n}>
      <NotificationInboxContext.Provider
        value={{ ...NOTIFICATION_INBOX_DEFAULT, unread: 0, unreadByProject }}
      >
        <MemoryRouter initialEntries={[path]}>
          <Navbar expand="lg">
            {ctx === null ? (
              switcher
            ) : (
              <ProjectContext.Provider value={ctx}>{switcher}</ProjectContext.Provider>
            )}
          </Navbar>
        </MemoryRouter>
      </NotificationInboxContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('ProjectSwitcher — bar', () => {
  it('is plain text for a single membership: no caret, no menu', () => {
    renderSwitcher(value([member('v')]))

    const switcher = screen.getByTestId('project-switcher')
    expect(switcher).toHaveTextContent('Veselice')
    expect(switcher).toHaveAttribute('title', 'Project Veselice')
    expect(within(switcher).queryByRole('button')).not.toBeInTheDocument()
    expect(switcher.querySelector('.dropdown-toggle')).toBeNull()
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('is a dropdown with more than one membership', async () => {
    const user = userEvent.setup()
    renderSwitcher(
      value([
        member('v'),
        member('rodina', { role: 'curator' }),
        member('skola', { role: 'admin' }),
      ]),
    )

    const toggle = screen.getByRole('button', { name: 'Veselice' })
    expect(toggle).toHaveClass('dropdown-toggle')
    expect(toggle).toHaveAttribute('title', 'Project Veselice — switch to another')
    await user.click(toggle)

    // The others, each with the role held there, and the chooser last.
    const rodina = await screen.findByRole('link', { name: /Projekt rodina/ })
    expect(within(rodina).getByText('Curator')).toBeInTheDocument()
    const skola = screen.getByRole('link', { name: /Projekt skola/ })
    expect(within(skola).getByText('Administrator')).toBeInTheDocument()
    const links = screen.getAllByRole('link')
    expect(links.at(-1)).toHaveTextContent('All projects…')
    expect(links.at(-1)).toHaveAttribute('href', '/projects')
    // The current project is the toggle, not an entry of its own menu.
    expect(screen.queryByRole('link', { name: /Veselice/ })).not.toBeInTheDocument()
  })

  it('keeps a list page and goes home from a page with a uid', async () => {
    const user = userEvent.setup()
    const ctx = value([member('v'), member('rodina')])
    const { unmount } = renderSwitcher(ctx, { variant: 'bar' }, '/p/v/albums')
    await user.click(screen.getByRole('button', { name: 'Veselice' }))
    expect(await screen.findByRole('link', { name: /Projekt rodina/ })).toHaveAttribute(
      'href',
      '/p/rodina/albums',
    )
    unmount()

    renderSwitcher(ctx, { variant: 'bar' }, '/p/v/albums/al1')
    await user.click(screen.getByRole('button', { name: 'Veselice' }))
    expect(await screen.findByRole('link', { name: /Projekt rodina/ })).toHaveAttribute(
      'href',
      '/p/rodina',
    )
  })

  it('names no project on an instance page, and opens every project at its home', async () => {
    const user = userEvent.setup()
    renderSwitcher(value([member('v'), member('rodina')], false), { variant: 'bar' }, '/account')
    // The context's project is only the entry rule's fallback: not named.
    expect(screen.queryByRole('button', { name: 'Veselice' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'My projects' }))

    expect(await screen.findByRole('link', { name: 'Veselice' })).toHaveAttribute('href', '/p/v')
    expect(screen.getByRole('link', { name: /Projekt rodina/ })).toHaveAttribute(
      'href',
      '/p/rodina',
    )
    expect(screen.getByRole('link', { name: 'All projects…' })).toHaveAttribute('href', '/projects')
  })

  it('is a plain link to the chooser outside a project when no menu fits', () => {
    renderSwitcher(
      value([member('v'), member('rodina')], false),
      { variant: 'bar', menu: false },
      '/account',
    )

    expect(screen.getByRole('link', { name: 'My projects' })).toHaveAttribute('href', '/projects')
  })

  it('offers only the projects that can be entered', async () => {
    const user = userEvent.setup()
    renderSwitcher(
      value([
        member('v'),
        member('off', { active: false }),
        member('wait', { approved: false }),
        member('rodina'),
      ]),
    )
    await user.click(screen.getByRole('button', { name: 'Veselice' }))

    await screen.findByRole('link', { name: /Projekt rodina/ })
    expect(screen.queryByRole('link', { name: /Projekt off/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /Projekt wait/ })).not.toBeInTheDocument()
    // The chooser is where those are listed, with the reason.
    expect(screen.getByRole('link', { name: 'All projects…' })).toHaveAttribute('href', '/projects')
  })

  it('is plain text when the shell asks for no menu (below lg)', () => {
    renderSwitcher(value([member('v'), member('rodina')]), { variant: 'bar', menu: false })

    expect(screen.getByTestId('project-switcher')).toHaveTextContent('Veselice')
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('renders nothing outside a provider', () => {
    const { container } = renderSwitcher(null)
    expect(container.querySelector('.kk-project-switcher')).toBeNull()
  })

  it('leads an account with no project to the chooser', () => {
    const none: ProjectContextValue = {
      project: null,
      projects: [],
      status: 'ready',
      inAddress: false,
      switchProject: vi.fn(),
    }
    renderSwitcher(none, { variant: 'bar' }, '/account')
    expect(screen.getByRole('link', { name: 'My projects' })).toHaveAttribute('href', '/projects')
  })
})

describe('ProjectSwitcher — drawer', () => {
  it('names the project and lists the others as rows, the chooser last', async () => {
    const user = userEvent.setup()
    const onNavigate = vi.fn()
    renderSwitcher(value([member('v'), member('rodina', { role: 'editor' })]), {
      variant: 'drawer',
      onNavigate,
    })

    const section = screen.getByRole('region', { name: 'Project' })
    expect(within(section).getByText('Veselice')).toBeInTheDocument()
    const links = within(section).getAllByRole('link')
    expect(links.map((a) => a.getAttribute('href'))).toEqual(['/p/rodina/albums', '/projects'])
    expect(within(links[0]).getByText('Editor')).toBeInTheDocument()

    await user.click(links[0])
    expect(onNavigate).toHaveBeenCalled()
  })

  it('is the name alone for a single membership', () => {
    renderSwitcher(value([member('v')]), { variant: 'drawer' })

    const section = screen.getByRole('region', { name: 'Project' })
    expect(within(section).getByText('Veselice')).toBeInTheDocument()
    expect(within(section).queryByRole('link')).not.toBeInTheDocument()
  })
})

describe('ProjectSwitcher — unread notifications of other projects', () => {
  // Veselice is current (its own news is the bell's); rodina and skola are not.
  const three = () =>
    value([member('v'), member('rodina', { role: 'curator' }), member('skola', { role: 'admin' })])
  const counts: UnreadByProject = { 'p-v': 4, 'p-rodina': 2 }

  it("marks the bar's name with the other projects' total, and each row with its own", async () => {
    const user = userEvent.setup()
    renderSwitcher(three(), { variant: 'bar' }, '/p/v/albums', counts)

    // 2, not 6: the current project's 4 are the bell's.
    const toggle = screen.getByRole('button', {
      name: 'Veselice Unread notifications in other projects: 2',
    })
    expect(within(toggle).getByTestId('project-unread')).toHaveTextContent(/^2/)
    await user.click(toggle)

    const rodina = await screen.findByRole('link', { name: /Projekt rodina/ })
    expect(rodina).toHaveAccessibleName('Projekt rodina 2 unread notifications Curator')
    const skola = screen.getByRole('link', { name: /Projekt skola/ })
    expect(within(skola).queryByTestId('project-unread')).not.toBeInTheDocument()
  })

  it('marks the plain name below lg, where the drawer is the way to switch', () => {
    renderSwitcher(three(), { variant: 'bar', menu: false }, '/p/v/albums', counts)

    const badge = within(screen.getByTestId('project-switcher')).getByTestId('project-unread')
    expect(badge).toHaveTextContent('Unread notifications in other projects: 2')
  })

  it("marks each of the drawer's project rows", () => {
    renderSwitcher(three(), { variant: 'drawer' }, '/p/v/albums', counts)

    const section = screen.getByRole('region', { name: 'Project' })
    const rodina = within(section).getByRole('link', { name: /Projekt rodina/ })
    expect(within(rodina).getByTestId('project-unread')).toHaveTextContent('2 unread notifications')
    const skola = within(section).getByRole('link', { name: /Projekt skola/ })
    expect(within(skola).queryByTestId('project-unread')).not.toBeInTheDocument()
    // The current project's name carries no pill: the bell speaks for it.
    expect(within(section).getAllByTestId('project-unread')).toHaveLength(1)
  })

  it('counts every project outside a project, where none is current', async () => {
    const user = userEvent.setup()
    const ctx = value([member('v'), member('rodina')], false)
    renderSwitcher(ctx, { variant: 'bar' }, '/account', counts)

    await user.click(
      screen.getByRole('button', { name: 'My projects Unread notifications in other projects: 6' }),
    )
    expect(
      await screen.findByRole('link', { name: 'Veselice 4 unread notifications' }),
    ).toBeVisible()
    expect(
      screen.getByRole('link', { name: 'Projekt rodina 2 unread notifications' }),
    ).toBeVisible()
  })

  it('uses the Czech plural forms', async () => {
    await i18n.changeLanguage('cs')
    renderSwitcher(three(), { variant: 'drawer' }, '/p/v/albums', { 'p-rodina': 1, 'p-skola': 5 })

    expect(screen.getByRole('link', { name: /Projekt rodina 1 nepřečtené oznámení/ })).toBeVisible()
    expect(
      screen.getByRole('link', { name: /Projekt skola 5 nepřečtených oznámení/ }),
    ).toBeVisible()
  })

  it('draws nothing while the counts are unknown or zero', () => {
    const { unmount } = renderSwitcher(three(), { variant: 'drawer' })
    expect(screen.queryByTestId('project-unread')).not.toBeInTheDocument()
    unmount()

    renderSwitcher(three(), { variant: 'bar' }, '/p/v/albums', { 'p-v': 3 })
    expect(screen.getByRole('button', { name: 'Veselice' })).toBeVisible()
    expect(screen.queryByTestId('project-unread')).not.toBeInTheDocument()
  })
})
