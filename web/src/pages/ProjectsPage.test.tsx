import { fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
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
              <Routes>
                <Route path="/projects" element={<ProjectsPage />} />
                <Route path="/p/:slug" element={<p>entered the project</p>} />
              </Routes>
            </MemoryRouter>
          </NotificationInboxContext.Provider>
        </ProjectContext.Provider>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/** The tile of the project named `name`. */
function tileOf(name: string): HTMLElement {
  const tile = screen
    .getAllByTestId('project-card')
    .find((el) => el.querySelector('.kk-chooser__name')?.textContent === name)
  if (tile === undefined) {
    throw new Error(`no tile for ${name}`)
  }
  return tile
}

/** The names of the tiles inside `root`, in document order. */
function tileNames(root: HTMLElement): string[] {
  return within(root)
    .getAllByTestId('project-card')
    .map((el) => el.querySelector('.kk-chooser__name')?.textContent ?? '')
}

beforeEach(async () => {
  await i18n.changeLanguage('cs')
})

describe('ProjectsPage', () => {
  it('asks where to go and draws one linked tile per project', () => {
    renderChooser([
      member('v', { name: 'Veselice', description: 'Obecní archiv', role: 'curator' }),
      member('rodina', { name: 'Rodina', role: 'admin' }),
    ])

    expect(
      screen.getByRole('heading', { level: 1, name: 'Kam se chcete podívat?' }),
    ).toBeInTheDocument()
    const v = screen.getByRole('link', { name: 'Veselice' })
    expect(v).toHaveAttribute('href', '/p/v')
    expect(v).toBe(tileOf('Veselice'))
    expect(v).toHaveAccessibleDescription('Obecní archiv Kurátor')
    expect(within(v).getByText('Obecní archiv')).toHaveAttribute('title', 'Obecní archiv')
    // The role is a quiet line, not a badge.
    expect(within(v).getByText('Kurátor')).toHaveClass('kk-chooser__role')
    expect(v.querySelector('.badge')).toBeNull()

    const rodina = screen.getByRole('link', { name: 'Rodina' })
    expect(rodina).toHaveAttribute('href', '/p/rodina')
    // No description, no empty line standing in for one.
    expect(rodina.querySelector('.kk-chooser__description')).toBeNull()
  })

  it('keeps a long name whole in its title', () => {
    const long = 'Sbor dobrovolných hasičů a přátelé obecní knihovny z horní vsi'
    renderChooser([member('v', { name: long })])

    expect(within(tileOf(long)).getByText(long)).toHaveAttribute('title', long)
  })

  it('puts the most recently visited project first, then the rest by name', () => {
    renderChooser([
      member('a', { name: 'Archiv' }),
      member('c', { name: 'Chalupa', last_visit_at: '2026-10-01T08:00:00Z' }),
      member('r', { name: 'Rodina', last_visit_at: '2026-10-15T08:00:00Z' }),
      member('b', { name: 'Brno' }),
    ])

    expect(tileNames(document.body)).toEqual(['Rodina', 'Chalupa', 'Archiv', 'Brno'])
  })

  it('sets deactivated projects apart in a collapsed section, unlinked', () => {
    renderChooser([
      member('v', { name: 'Veselice' }),
      member('off', { name: 'Staré', active: false, last_visit_at: '2026-10-16T08:00:00Z' }),
    ])

    const section = screen.getByTestId('projects-inactive')
    expect(section.tagName).toBe('DETAILS')
    expect(section).not.toHaveAttribute('open')
    expect(within(section).getByText('Vypnuté projekty (1)')).toBeInTheDocument()
    expect(tileNames(section)).toEqual(['Staré'])
    const off = tileOf('Staré')
    expect(off).toHaveAttribute('aria-disabled', 'true')
    expect(off).toHaveClass('kk-chooser__tile--inactive')
    expect(off).toHaveAttribute('title', 'Projekt je vypnutý, nelze ho otevřít.')
    expect(within(off).getByText('Prohlížeč, projekt je vypnutý')).toBeInTheDocument()
    expect(within(off).queryByRole('link')).not.toBeInTheDocument()
    // The active grid holds only the active project.
    expect(screen.getAllByRole('link', { name: 'Veselice' })).toHaveLength(1)
  })

  it('says a membership awaits approval and does not link it', () => {
    renderChooser([member('wait', { name: 'Škola', approved: false, role: 'editor' })])

    const wait = tileOf('Škola')
    expect(within(wait).getByText('Editor, čeká na schválení')).toBeInTheDocument()
    expect(wait).toHaveAttribute('title', 'Vaše členství ještě musí schválit správce projektu.')
    expect(wait).not.toHaveAttribute('href')
    expect(wait).toHaveAttribute('aria-disabled', 'true')
    // Not switched off — so not in the switched-off section.
    expect(screen.queryByTestId('projects-inactive')).not.toBeInTheDocument()
  })

  it('shows the cover photo the server names', () => {
    renderChooser([
      member('v', { name: 'Veselice', cover_thumb_url: '/api/v1/p/v/photos/ph1/thumb/tile_500' }),
    ])

    const cover = within(tileOf('Veselice')).getByTestId('project-cover')
    expect(cover).toHaveAttribute('src', '/api/v1/p/v/photos/ph1/thumb/tile_500')
    // Decorative: the name is written out under it.
    expect(cover).toHaveAttribute('alt', '')
    expect(within(tileOf('Veselice')).queryByTestId('project-placeholder')).not.toBeInTheDocument()
  })

  it('falls back to the initial when there is no cover or it fails to load', () => {
    renderChooser([
      member('v', { name: 'Veselice', cover_thumb_url: '/broken.jpg' }),
      member('r', { name: 'rodina', cover_thumb_url: '' }),
    ])

    const rodina = within(tileOf('rodina')).getByTestId('project-placeholder')
    expect(rodina).toHaveTextContent(/^R$/)
    expect(rodina.className).toMatch(/kk-avatar--tone-\d/)

    fireEvent.error(within(tileOf('Veselice')).getByTestId('project-cover'))
    expect(within(tileOf('Veselice')).queryByTestId('project-cover')).not.toBeInTheDocument()
    expect(within(tileOf('Veselice')).getByTestId('project-placeholder')).toHaveTextContent('V')
  })

  it('enters a project from the keyboard', async () => {
    const user = userEvent.setup()
    renderChooser([member('v', { name: 'Veselice' }), member('r', { name: 'Rodina' })])

    await user.tab()
    await user.tab()
    await user.tab()
    expect(screen.getByRole('link', { name: 'Rodina' })).toHaveFocus()
    await user.keyboard('{Enter}')
    expect(await screen.findByText('entered the project')).toBeInTheDocument()
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
    renderChooser([member('v', { name: 'Veselice', active: false })])

    expect(
      screen.getByRole('heading', { level: 1, name: 'Which library do you want to open?' }),
    ).toBeInTheDocument()
    expect(screen.getByText('Switched-off projects (1)')).toBeInTheDocument()
  })

  it('offers project administration to a maintainer only, in the quiet account row', () => {
    const { unmount } = renderChooser([member('v')], true)
    const row = screen.getByRole('navigation', { name: 'Účet a správa' })
    expect(within(row).getByRole('link', { name: 'Správa projektů' })).toHaveAttribute(
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

    const badge = within(tileOf('Projekt rodina')).getByTestId('project-unread')
    expect(badge).toHaveTextContent(/^3/)
    expect(badge).toHaveAttribute('title', '3 nepřečtená oznámení')
    expect(within(tileOf('Projekt v')).queryByTestId('project-unread')).not.toBeInTheDocument()
    expect(within(tileOf('Projekt skola')).queryByTestId('project-unread')).not.toBeInTheDocument()
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
