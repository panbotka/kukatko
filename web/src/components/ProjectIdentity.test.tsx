import { cleanup, render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { writeLastProject } from '../lib/lastProject'
import { ProjectContext, type ProjectContextValue } from '../project/ProjectContext'
import type { ProjectMembership } from '../services/auth'

import { ProjectIdentity } from './ProjectIdentity'

/** An approved membership of the active project `slug`. */
function member(slug: string, name: string): ProjectMembership {
  return { uid: `p-${slug}`, slug, name, active: true, role: 'viewer', approved: true }
}

/** A signed-in account, the system administrator when `isMaintainer`. */
function auth(isMaintainer = false): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'User One', role: 'viewer' },
    isMaintainer,
  } as unknown as AuthContextValue
}

/**
 * Mounts the identity with `ctx` as the project context (none for `null`),
 * the sidebar's variant with the name when `withName`.
 */
function renderIdentity(
  ctx: ProjectContextValue | null,
  opts: { withName?: boolean; compact?: boolean; account?: AuthContextValue } = {},
) {
  const identity = (
    <MemoryRouter initialEntries={['/p/v/albums']}>
      <ProjectIdentity withName={opts.withName} compact={opts.compact} />
    </MemoryRouter>
  )
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={opts.account ?? auth()}>
        {ctx === null ? (
          identity
        ) : (
          <ProjectContext.Provider value={ctx}>{identity}</ProjectContext.Provider>
        )}
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/**
 * A context working in `project`, the address naming it unless `inAddress`
 * says otherwise; `others` are the account's further memberships.
 */
function context(
  project: ProjectMembership | null,
  inAddress = true,
  others: ProjectMembership[] = [member('rodina', 'Rodina')],
): ProjectContextValue {
  return {
    project,
    projects: project === null ? [] : [project, ...others],
    status: 'ready',
    inAddress,
    switchProject: vi.fn(),
  }
}

beforeEach(async () => {
  window.localStorage.clear()
  await i18n.changeLanguage('en')
})

describe('ProjectIdentity', () => {
  it('is the letter mark alone, opening the project switch', () => {
    renderIdentity(context(member('v', 'Veselice')))

    const link = screen.getByRole('link', { name: 'Veselice — switch library' })
    expect(link).toHaveAttribute('href', '/projects')
    expect(link).toHaveAttribute('title', 'Veselice — switch library')
    // The name is the label, never text in the bar.
    expect(screen.queryByText('Veselice')).not.toBeInTheDocument()
    expect(link).toHaveTextContent(/^V$/)
    // The chooser's stand-in, small and decorative: the label is the link's.
    const mark = link.querySelector('.kk-project-identity__mark')
    expect(mark).toHaveTextContent('V')
    expect(mark).toHaveAttribute('aria-hidden', 'true')
    // No menu, no other project: picking happens on the chooser.
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
    expect(screen.queryByText('Rodina')).not.toBeInTheDocument()
  })

  it('links to the library and names only the project with nowhere to switch', () => {
    // A membership waiting for approval is not somewhere to go.
    renderIdentity(
      context(member('v', 'Veselice'), true, [{ ...member('rodina', 'Rodina'), approved: false }]),
    )

    const link = screen.getByRole('link', { name: 'Veselice' })
    expect(link).toHaveAttribute('href', '/p/v')
    expect(link).toHaveAttribute('title', 'Veselice')
    expect(link).toHaveTextContent(/^V$/)
  })

  it('opens the switch for the system administrator even with one project', () => {
    renderIdentity(context(member('v', 'Veselice'), true, []), { account: auth(true) })

    expect(screen.getByRole('link', { name: 'Veselice — switch library' })).toHaveAttribute(
      'href',
      '/projects',
    )
  })

  it('draws the name beside the mark in the sidebar', () => {
    renderIdentity(context(member('v', 'Veselice')), { withName: true })

    const link = screen.getByRole('link', { name: 'Veselice — switch library' })
    expect(link).toHaveAttribute('href', '/projects')
    expect(link.querySelector('.kk-project-identity__name')).toHaveTextContent('Veselice')
  })

  it('leads back to the remembered project outside one, with its mark', () => {
    writeLastProject('u1', 'rodina')
    renderIdentity(context(member('v', 'Veselice'), false))

    const link = screen.getByRole('link', { name: 'Back to Rodina' })
    expect(link).toHaveAttribute('href', '/p/rodina')
    expect(link).toHaveAttribute('title', 'Back to Rodina')
    expect(link.querySelector('.kk-project-identity__mark')).toHaveTextContent('R')
    // The neutral title stays beside the mark, but it is no longer the only way back.
    expect(link.querySelector('.kk-project-identity__name')).toHaveTextContent('Kukátko')
    expect(screen.queryByText('Veselice')).not.toBeInTheDocument()
  })

  it('leads to the only project when nothing is remembered', () => {
    renderIdentity(context(member('v', 'Veselice'), false, []))

    expect(screen.getByRole('link', { name: 'Back to Veselice' })).toHaveAttribute('href', '/p/v')
  })

  it('is the neutral app title opening the chooser with no project to name', () => {
    renderIdentity(context(member('v', 'Veselice'), false))

    const link = screen.getByTestId('project-identity')
    expect(link).toHaveTextContent(/^Kukátko$/)
    expect(link).toHaveAttribute('href', '/projects')
    expect(link).toHaveAccessibleName('Choose a library')
    expect(link.querySelector('.kk-project-identity__mark')).toBeNull()
  })

  it('folds to the mark alone on the rail', () => {
    writeLastProject('u1', 'rodina')
    renderIdentity(context(member('v', 'Veselice'), false), { compact: true })

    const link = screen.getByRole('link', { name: 'Back to Rodina' })
    expect(link).toHaveTextContent(/^R$/)
  })

  it('is the neutral title linking to the entry rule without an account', () => {
    const signedOut = {
      status: 'unauthenticated',
      user: null,
      isMaintainer: false,
    } as unknown as AuthContextValue
    renderIdentity(context(member('v', 'Veselice'), false), { account: signedOut })

    const link = screen.getByTestId('project-identity')
    expect(link).toHaveTextContent(/^Kukátko$/)
    expect(link).toHaveAttribute('href', '/')
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
    const { unmount } = renderIdentity(context(member('v', 'Veselice')))

    expect(screen.getByRole('link', { name: 'Veselice — přepnout projekt' })).toHaveAttribute(
      'title',
      'Veselice — přepnout projekt',
    )
    unmount()

    renderIdentity(context(member('v', 'Veselice'), true, []))
    expect(screen.getByRole('link', { name: 'Veselice' })).toHaveAttribute('title', 'Veselice')
    cleanup()

    renderIdentity(context(member('v', 'Veselice'), false, []))
    expect(screen.getByRole('link', { name: 'Zpět do projektu Veselice' })).toHaveAttribute(
      'href',
      '/p/v',
    )
    cleanup()

    renderIdentity(context(member('v', 'Veselice'), false))
    expect(screen.getByRole('link', { name: 'Vybrat projekt' })).toHaveAttribute(
      'href',
      '/projects',
    )
  })
})
