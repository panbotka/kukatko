import { render, screen, within } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import {
  NOTIFICATION_INBOX_DEFAULT,
  NotificationInboxContext,
} from '../notifications/NotificationInboxContext'
import { ProjectContext, type ProjectContextValue } from '../project/ProjectContext'
import type { ProjectMembership } from '../services/auth'
import type { UnreadByProject } from '../services/notifications'

import { ProjectsPage } from './ProjectsPage'

/** An approved membership of an active project `slug`, overridable. */
function member(slug: string, extra: Partial<ProjectMembership> = {}): ProjectMembership {
  return {
    uid: `p-${slug}`,
    slug,
    name: `Projekt ${slug}`,
    active: true,
    role: 'viewer',
    approved: true,
    ...extra,
  }
}

/** A signed-in account; a maintainer when asked. */
function auth(isMaintainer = false): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'U', role: 'viewer' },
    isMaintainer,
    logout: vi.fn(),
  } as unknown as AuthContextValue
}

/**
 * Mounts the chooser for an account with `projects`, with `unreadByProject` as
 * the inbox provider's per-project counts.
 */
function renderChooser(
  projects: ProjectMembership[],
  isMaintainer = false,
  unreadByProject: UnreadByProject | null = null,
) {
  const ctx: ProjectContextValue = {
    project: projects[0] ?? null,
    projects,
    status: 'ready',
    inAddress: false,
    switchProject: vi.fn(),
  }
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(isMaintainer)}>
        <ProjectContext.Provider value={ctx}>
          <NotificationInboxContext.Provider
            value={{ ...NOTIFICATION_INBOX_DEFAULT, unreadByProject }}
          >
            <MemoryRouter initialEntries={['/projects']}>
              <ProjectsPage />
            </MemoryRouter>
          </NotificationInboxContext.Provider>
        </ProjectContext.Provider>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/** The card of the project named `name`. */
function cardOf(name: string): HTMLElement {
  const card = screen
    .getAllByTestId('project-card')
    .find((el) => within(el).queryByRole('heading', { name }) !== null)
  if (card === undefined) {
    throw new Error(`no card for ${name}`)
  }
  return card
}

beforeEach(async () => {
  await i18n.changeLanguage('cs')
})

describe('ProjectsPage', () => {
  it('draws a card per membership: name, description, role, a link home', () => {
    renderChooser([
      member('v', { name: 'Veselice', description: 'Obecní archiv', role: 'curator' }),
      member('rodina', { name: 'Rodina', role: 'admin' }),
    ])

    expect(screen.getByRole('heading', { level: 1, name: 'Projekty' })).toBeInTheDocument()
    const v = cardOf('Veselice')
    expect(within(v).getByRole('link', { name: 'Veselice' })).toHaveAttribute('href', '/p/v')
    expect(within(v).getByRole('link', { name: 'Veselice' })).toHaveClass('stretched-link')
    expect(within(v).getByText('Obecní archiv')).toHaveClass('kk-project-card__description')
    expect(within(v).getByText('Kurátor')).toBeInTheDocument()
    expect(v).not.toHaveAttribute('aria-disabled')

    const rodina = cardOf('Rodina')
    expect(within(rodina).getByRole('link', { name: 'Rodina' })).toHaveAttribute(
      'href',
      '/p/rodina',
    )
    expect(within(rodina).getByText('Správce')).toBeInTheDocument()
    // No description, no empty paragraph standing in for one.
    expect(rodina.querySelector('.kk-project-card__description')).toBeNull()
  })

  it('greys a deactivated project and does not link it', () => {
    renderChooser([
      member('v', { name: 'Veselice' }),
      member('off', { name: 'Staré', active: false }),
    ])

    const off = cardOf('Staré')
    expect(off).toHaveClass('kk-project-card--inactive')
    expect(off).toHaveAttribute('aria-disabled', 'true')
    expect(within(off).getByText('Nedostupné')).toBeInTheDocument()
    expect(within(off).queryByRole('link')).not.toBeInTheDocument()
  })

  it('says a membership awaits approval and does not link it', () => {
    renderChooser([member('wait', { name: 'Škola', approved: false, role: 'editor' })])

    const wait = cardOf('Škola')
    expect(within(wait).getByText('Čeká na schválení')).toBeInTheDocument()
    expect(within(wait).getByText('Editor')).toBeInTheDocument()
    expect(within(wait).queryByRole('link')).not.toBeInTheDocument()
    expect(wait).toHaveAttribute('aria-disabled', 'true')
    // Not switched off — so not greyed like one.
    expect(wait).not.toHaveClass('kk-project-card--inactive')
  })

  it('asks for an invitation when the account belongs nowhere', () => {
    renderChooser([])

    expect(
      screen.getByText('Nejste členem žádného projektu. Požádejte správce projektu o pozvánku.'),
    ).toBeInTheDocument()
    expect(screen.queryByTestId('project-card')).not.toBeInTheDocument()
    // Still a way out: the account and sign-out.
    expect(screen.getByRole('link', { name: 'Účet' })).toHaveAttribute('href', '/account')
    expect(screen.getByRole('button', { name: 'Odhlásit se' })).toBeInTheDocument()
  })

  it('says it in English too', async () => {
    await i18n.changeLanguage('en')
    renderChooser([])

    expect(
      screen.getByText(
        'You are not a member of any project. Ask a project administrator for an invitation.',
      ),
    ).toBeInTheDocument()
  })

  it('offers project administration to a maintainer only', () => {
    const { unmount } = renderChooser([member('v')], true)
    expect(screen.getByRole('link', { name: 'Správa projektů' })).toHaveAttribute(
      'href',
      '/system/projects',
    )
    unmount()

    renderChooser([member('v')], false)
    expect(screen.queryByRole('link', { name: 'Správa projektů' })).not.toBeInTheDocument()
  })

  it('marks a project with unread notifications with their count', () => {
    renderChooser([member('v'), member('rodina'), member('skola')], false, {
      'p-rodina': 3,
      'p-skola': 0,
    })

    const badge = within(cardOf('Projekt rodina')).getByTestId('project-unread')
    expect(badge).toHaveTextContent(/^3/)
    expect(badge).toHaveAttribute('title', '3 nepřečtená oznámení')
    expect(within(cardOf('Projekt v')).queryByTestId('project-unread')).not.toBeInTheDocument()
    expect(within(cardOf('Projekt skola')).queryByTestId('project-unread')).not.toBeInTheDocument()
  })

  it('draws no unread mark while the counts are unknown', () => {
    renderChooser([member('v'), member('rodina')])

    expect(screen.queryByTestId('project-unread')).not.toBeInTheDocument()
  })

  it('titles the tab after the chooser, with no project in it', () => {
    renderChooser([member('v')])

    expect(document.title).toBe('Projekty · Kukátko')
  })
})
