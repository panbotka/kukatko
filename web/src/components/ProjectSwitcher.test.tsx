import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import Navbar from 'react-bootstrap/Navbar'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { ProjectContext, type ProjectContextValue } from '../project/ProjectContext'
import type { ProjectMembership } from '../services/auth'

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

/** Mounts the switcher inside a navbar at `path`, as the shell does. */
function renderSwitcher(
  ctx: ProjectContextValue | null,
  props: ProjectSwitcherProps = { variant: 'bar' },
  path = '/p/v/albums',
) {
  const switcher = <ProjectSwitcher {...props} />
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={[path]}>
        <Navbar expand="lg">
          {ctx === null ? (
            switcher
          ) : (
            <ProjectContext.Provider value={ctx}>{switcher}</ProjectContext.Provider>
          )}
        </Navbar>
      </MemoryRouter>
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

  it('opens every project at its home from an instance page', async () => {
    const user = userEvent.setup()
    renderSwitcher(value([member('v'), member('rodina')], false), { variant: 'bar' }, '/account')
    await user.click(screen.getByRole('button', { name: 'Veselice' }))

    expect(await screen.findByRole('link', { name: /Projekt rodina/ })).toHaveAttribute(
      'href',
      '/p/rodina',
    )
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

  it('renders nothing without a project', () => {
    const { container } = renderSwitcher(null)
    expect(container.querySelector('.kk-project-switcher')).toBeNull()

    const none: ProjectContextValue = {
      project: null,
      projects: [],
      status: 'ready',
      inAddress: false,
      switchProject: vi.fn(),
    }
    renderSwitcher(none)
    expect(screen.queryByTestId('project-switcher')).not.toBeInTheDocument()
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
