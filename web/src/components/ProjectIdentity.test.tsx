import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { ProjectContext, type ProjectContextValue } from '../project/ProjectContext'
import type { ProjectMembership } from '../services/auth'

import { ProjectIdentity } from './ProjectIdentity'

/** An approved membership of the active project `slug`. */
function member(slug: string, name: string): ProjectMembership {
  return { uid: `p-${slug}`, slug, name, active: true, role: 'viewer', approved: true }
}

/** Mounts the identity with `ctx` as the project context (none for `null`). */
function renderIdentity(ctx: ProjectContextValue | null) {
  const identity = (
    <MemoryRouter initialEntries={['/p/v/albums']}>
      <ProjectIdentity />
    </MemoryRouter>
  )
  return render(
    <I18nextProvider i18n={i18n}>
      {ctx === null ? (
        identity
      ) : (
        <ProjectContext.Provider value={ctx}>{identity}</ProjectContext.Provider>
      )}
    </I18nextProvider>,
  )
}

/** A context working in `project`, the address naming it unless `inAddress` says otherwise. */
function context(project: ProjectMembership | null, inAddress = true): ProjectContextValue {
  return {
    project,
    projects: project === null ? [] : [project, member('rodina', 'Rodina')],
    status: 'ready',
    inAddress,
    switchProject: vi.fn(),
  }
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('ProjectIdentity', () => {
  it('names the current project and links to its library', () => {
    renderIdentity(context(member('v', 'Veselice')))

    const link = screen.getByRole('link', { name: 'Veselice' })
    expect(link).toHaveAttribute('href', '/p/v')
    expect(link).toHaveAttribute('title', 'The library of the project Veselice')
    // The chooser's stand-in, small and decorative: the name is the link's.
    const mark = link.querySelector('.kk-project-identity__mark')
    expect(mark).toHaveTextContent('V')
    expect(mark).toHaveAttribute('aria-hidden', 'true')
    // A calm identity: nothing to open, no other project.
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
    expect(screen.queryByText('Rodina')).not.toBeInTheDocument()
  })

  it('is the neutral app title, a link to the entry rule, outside a project', () => {
    renderIdentity(context(member('v', 'Veselice'), false))

    const link = screen.getByTestId('project-identity')
    expect(link).toHaveTextContent(/^Kukátko$/)
    expect(link).toHaveAttribute('href', '/')
    expect(screen.queryByText('Veselice')).not.toBeInTheDocument()
  })

  it('renders nothing without a provider or a project', () => {
    const { unmount } = renderIdentity(null)
    expect(screen.queryByTestId('project-identity')).not.toBeInTheDocument()
    unmount()

    renderIdentity(context(null))
    expect(screen.queryByTestId('project-identity')).not.toBeInTheDocument()
  })

  it('speaks Czech by default', async () => {
    await i18n.changeLanguage('cs')
    renderIdentity(context(member('v', 'Veselice')))

    expect(screen.getByRole('link', { name: 'Veselice' })).toHaveAttribute(
      'title',
      'Knihovna projektu Veselice',
    )
  })
})
