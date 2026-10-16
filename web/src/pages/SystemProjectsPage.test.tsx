import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import type { AdminProject } from '../services/projects'
import type { RekeyRun, RekeyState } from '../services/rekey'

import { SystemProjectsPage } from './SystemProjectsPage'

vi.mock('../services/projects', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/projects')>()
  return { ...actual, fetchAdminProjects: vi.fn(), createProject: vi.fn() }
})

vi.mock('../services/rekey', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/rekey')>()
  return { ...actual, fetchRekeyRun: vi.fn() }
})

const { fetchAdminProjects, createProject, ProjectApiError } = await import('../services/projects')
const { fetchRekeyRun } = await import('../services/rekey')
const rekeyMock = vi.mocked(fetchRekeyRun)
const fetchMock = vi.mocked(fetchAdminProjects)
const createMock = vi.mocked(createProject)

const GB = 1024 * 1024 * 1024

/** A project row, overridable. */
function project(overrides: Partial<AdminProject> = {}): AdminProject {
  return {
    uid: 'pjv',
    slug: 'v',
    name: 'Veselice',
    description: '',
    active: true,
    storage_prefix: '',
    max_file_bytes: null,
    max_total_bytes: null,
    members_count: 12,
    photos_count: 20310,
    originals_bytes: 3 * GB,
    created_at: '2026-10-01T08:00:00Z',
    updated_at: '2026-10-01T08:00:00Z',
    ...overrides,
  }
}

/** A storage move of `projectUid` in `state`, `done` of 200 photos moved. */
function move(projectUid: string, state: RekeyState, done: number): RekeyRun {
  return {
    uid: `rk-${projectUid}`,
    project_uid: projectUid,
    state,
    target_prefix: `projects/${projectUid}/`,
    total_photos: 200,
    total_bytes: 200 * 1024,
    done_photos: done,
    done_bytes: done * 1024,
    last_key: '',
    errors: [],
    error_count: 0,
    derived_failed: 0,
    started_at: '2026-10-13T08:00:00Z',
    updated_at: '2026-10-13T08:00:00Z',
  }
}

/** The session's re-read, which a created project's creator needs. */
let refreshMock = vi.fn()

function renderPage() {
  const value = { status: 'authenticated', refresh: refreshMock } as unknown as AuthContextValue
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={value}>
        <MemoryRouter initialEntries={['/system/projects']}>
          <Routes>
            <Route path="/system/projects" element={<SystemProjectsPage />} />
            <Route path="/system/projects/:slug" element={<p>detail page</p>} />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('cs')
  fetchMock.mockReset()
  createMock.mockReset()
  refreshMock = vi.fn().mockResolvedValue(undefined)
  rekeyMock.mockReset()
  rekeyMock.mockResolvedValue(null)
  fetchMock.mockResolvedValue([
    project(),
    project({
      uid: 'pjr',
      slug: 'rodina',
      name: 'Rodina',
      active: false,
      max_file_bytes: 50 * 1024 * 1024,
      max_total_bytes: 2 * GB,
      members_count: 3,
      photos_count: 0,
      originals_bytes: 0,
    }),
  ])
})

/** The table row naming `name`. */
function rowOf(name: string): HTMLElement {
  const row = screen.getByRole('link', { name }).closest('tr')
  if (row === null) {
    throw new Error(`no row for ${name}`)
  }
  return row
}

describe('SystemProjectsPage', () => {
  it('lists every project: name link, slug, status, counts, bytes, limits, created', async () => {
    renderPage()

    expect(await screen.findByRole('link', { name: 'Veselice' })).toHaveAttribute(
      'href',
      '/system/projects/v',
    )
    const headers = screen.getAllByRole('columnheader').map((th) => th.textContent)
    expect(headers).toEqual([
      'Název',
      'Slug',
      'Stav',
      'Členové',
      'Fotky',
      'Originály',
      'Limity',
      'Založen',
    ])

    const veselice = within(rowOf('Veselice'))
    expect(veselice.getByText('v')).toHaveClass('font-monospace')
    expect(veselice.getByText('Aktivní')).toBeInTheDocument()
    expect(veselice.getByText('12')).toBeInTheDocument()
    expect(veselice.getByText(/20\s310/)).toBeInTheDocument()
    expect(veselice.getByText('3,0 GB')).toBeInTheDocument()
    expect(veselice.getByText('neomezeno')).toBeInTheDocument()

    const rodina = within(rowOf('Rodina'))
    expect(rodina.getByText('Vypnutý')).toBeInTheDocument()
    expect(rodina.getByText('soubor 50,0 MB · celkem 2,0 GB')).toBeInTheDocument()
  })

  it('offers a retry when the list cannot be loaded', async () => {
    fetchMock.mockRejectedValueOnce(new Error('boom'))
    renderPage()

    expect(await screen.findByText('Projekty se nepodařilo načíst.')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /znovu/i }))
    expect(await screen.findByRole('link', { name: 'Veselice' })).toBeInTheDocument()
  })

  it('checks the slug live and refuses an empty form before any round trip', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.click(await screen.findByRole('button', { name: 'Nový projekt' }))
    const dialog = await screen.findByRole('dialog')

    // The hint says up front that the slug is for good.
    expect(within(dialog).getByText(/Nelze později změnit/)).toBeInTheDocument()

    const slug = within(dialog).getByLabelText('Slug')
    await user.type(slug, 'Rodina')
    expect(slug).toHaveClass('is-invalid')
    expect(within(dialog).getByText(/Jen malá písmena/)).toBeInTheDocument()

    await user.clear(slug)
    await user.type(slug, 'system')
    expect(within(dialog).getByText('„system“ je vyhrazené slovo aplikace.')).toBeInTheDocument()

    await user.clear(slug)
    await user.type(slug, 'rodina')
    expect(slug).not.toHaveClass('is-invalid')

    // No name and a nonsense limit: refused here, nothing sent.
    await user.type(within(dialog).getByLabelText('Největší soubor'), 'hodně')
    await user.click(within(dialog).getByRole('button', { name: 'Založit projekt' }))
    expect(within(dialog).getByText('Vyplňte název.')).toBeInTheDocument()
    expect(within(dialog).getByText(/Zadejte kladné číslo/)).toBeInTheDocument()
    expect(createMock).not.toHaveBeenCalled()
  })

  it('creates the project with the limits in bytes and opens its page', async () => {
    const user = userEvent.setup()
    createMock.mockResolvedValue(project({ uid: 'pjn', slug: 'nova', name: 'Nová' }))
    renderPage()
    await user.click(await screen.findByRole('button', { name: 'Nový projekt' }))
    const dialog = await screen.findByRole('dialog')

    await user.type(within(dialog).getByLabelText('Slug'), 'nova')
    await user.type(within(dialog).getByLabelText('Název'), 'Nová')
    await user.type(within(dialog).getByLabelText('Největší soubor'), '1,5')
    const total = within(dialog).getByLabelText('Kvóta všech originálů')
    await user.type(total, '2')
    const units = within(dialog).getAllByLabelText('Jednotka')
    await user.selectOptions(units[1], 'GB')
    await user.click(within(dialog).getByRole('button', { name: 'Založit projekt' }))

    await waitFor(() => {
      expect(createMock).toHaveBeenCalledWith({
        slug: 'nova',
        name: 'Nová',
        description: '',
        max_file_bytes: Math.round(1.5 * 1024 * 1024),
        max_total_bytes: 2 * GB,
      })
    })
    expect(await screen.findByText('detail page')).toBeInTheDocument()
    // The creator is its admin: the session is re-read before the page opens,
    // so the switcher lists it and the page offers no "add me".
    expect(refreshMock).toHaveBeenCalledTimes(1)
  })

  it('opens the new project only once the session has been re-read', async () => {
    const user = userEvent.setup()
    let finishRefresh: () => void = () => undefined
    refreshMock = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          finishRefresh = resolve
        }),
    )
    createMock.mockResolvedValue(project({ uid: 'pjn', slug: 'nova', name: 'Nová' }))
    renderPage()
    await user.click(await screen.findByRole('button', { name: 'Nový projekt' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText('Slug'), 'nova')
    await user.type(within(dialog).getByLabelText('Název'), 'Nová')
    await user.click(within(dialog).getByRole('button', { name: 'Založit projekt' }))

    await waitFor(() => {
      expect(refreshMock).toHaveBeenCalled()
    })
    expect(screen.queryByText('detail page')).not.toBeInTheDocument()
    finishRefresh()
    expect(await screen.findByText('detail page')).toBeInTheDocument()
  })

  it('puts slug_taken on the slug field in words', async () => {
    const user = userEvent.setup()
    createMock.mockRejectedValue(new ProjectApiError(409, 'slug_taken', ''))
    renderPage()
    await user.click(await screen.findByRole('button', { name: 'Nový projekt' }))
    const dialog = await screen.findByRole('dialog')

    await user.type(within(dialog).getByLabelText('Slug'), 'v2')
    await user.type(within(dialog).getByLabelText('Název'), 'Druhá')
    await user.click(within(dialog).getByRole('button', { name: 'Založit projekt' }))

    expect(await within(dialog).findByText('Tento slug už existuje')).toBeInTheDocument()
    expect(within(dialog).getByLabelText('Slug')).toHaveClass('is-invalid')
    // Unlimited limits are left out of the body, not sent as null.
    expect(createMock).toHaveBeenCalledWith({ slug: 'v2', name: 'Druhá', description: '' })
  })

  it('marks a project whose storage move is running or paused with its percent', async () => {
    rekeyMock.mockImplementation((uid) =>
      Promise.resolve(uid === 'pjv' ? move('pjv', 'running', 84) : move('pjr', 'done', 200)),
    )
    renderPage()

    const chip = await screen.findByTestId('move-chip-v')
    expect(chip).toHaveTextContent('přesun 42 %')
    expect(chip).toHaveAttribute('title', 'Úložiště projektu se přesouvá')
    expect(rekeyMock).toHaveBeenCalledTimes(2)
    // A done move is history, not news.
    expect(screen.queryByTestId('move-chip-rodina')).not.toBeInTheDocument()
  })

  it('marks a paused move too, and shows the list when a run cannot be read', async () => {
    rekeyMock.mockImplementation((uid) =>
      uid === 'pjv'
        ? Promise.reject(new Error('offline'))
        : Promise.resolve(move('pjr', 'paused', 50)),
    )
    renderPage()

    const chip = await screen.findByTestId('move-chip-rodina')
    expect(chip).toHaveTextContent('přesun 25 %')
    expect(chip).toHaveAttribute('title', 'Přesun úložiště je pozastavený')
    expect(screen.getByRole('link', { name: 'Veselice' })).toBeInTheDocument()
    expect(screen.queryByTestId('move-chip-v')).not.toBeInTheDocument()
  })

  describe('setting a project up for somebody else', () => {
    /** Opens the dialog, fills slug and name and switches the first-admin section on. */
    async function openForOther(user: ReturnType<typeof userEvent.setup>) {
      renderPage()
      await user.click(await screen.findByRole('button', { name: 'Nový projekt' }))
      const dialog = await screen.findByRole('dialog')
      await user.type(within(dialog).getByLabelText('Slug'), 'chata')
      await user.type(within(dialog).getByLabelText('Název'), 'Chata')
      expect(within(dialog).queryByTestId('first-admin-section')).not.toBeInTheDocument()
      await user.click(within(dialog).getByLabelText('Zakládám projekt pro někoho jiného'))
      expect(within(dialog).getByText('První správce projektu')).toBeInTheDocument()
      return dialog
    }

    it('requires a valid e-mail in the section before any round trip', async () => {
      const user = userEvent.setup()
      const dialog = await openForOther(user)

      await user.click(within(dialog).getByRole('button', { name: 'Založit projekt' }))
      expect(within(dialog).getByLabelText('E-mail')).toHaveClass('is-invalid')
      expect(within(dialog).getByText('Zadejte platnou e-mailovou adresu.')).toBeInTheDocument()
      expect(createMock).not.toHaveBeenCalled()
    })

    it('sends the admin, stays on the result and says the e-mail went out', async () => {
      const user = userEvent.setup()
      createMock.mockResolvedValue({
        ...project({ uid: 'pjc', slug: 'chata', name: 'Chata', members_count: 1 }),
        first_admin: {
          user_uid: 'usj',
          username: 'jana',
          display_name: 'Jana Nováková',
          email: 'jana@example.com',
          account: 'created',
          mail_sent: true,
          set_password_url: 'https://k.example/password-reset/tok?project=chata',
          expires_at: '2026-10-23T10:00:00Z',
        },
      })
      const dialog = await openForOther(user)
      await user.type(within(dialog).getByLabelText('E-mail'), 'jana@example.com')
      await user.type(
        within(dialog).getByLabelText('Zobrazované jméno (nepovinné)'),
        'Jana Nováková',
      )
      fetchMock.mockClear()
      await user.click(within(dialog).getByRole('button', { name: 'Založit projekt' }))

      expect(createMock).toHaveBeenCalledWith({
        slug: 'chata',
        name: 'Chata',
        description: '',
        admin: { email: 'jana@example.com', display_name: 'Jana Nováková' },
      })
      const result = await screen.findByTestId('first-admin-result')
      expect(screen.getByText('Projekt „Chata“ je založený')).toBeInTheDocument()
      expect(screen.getByRole('link', { name: '/p/chata' })).toHaveAttribute('href', '/p/chata')
      expect(result).toHaveTextContent('jana')
      expect(result).toHaveTextContent('nový účet')
      expect(result).toHaveTextContent('E-mail odeslán na jana@example.com.')
      expect(result).toHaveTextContent(/Odkaz platí do/)
      // Mailed: the link is not put on the screen.
      expect(within(result).queryByRole('textbox')).not.toBeInTheDocument()
      // The creator is no member: the session is not re-read, the list is.
      expect(refreshMock).not.toHaveBeenCalled()
      await waitFor(() => {
        expect(fetchMock).toHaveBeenCalled()
      })
      expect(screen.queryByText('detail page')).not.toBeInTheDocument()

      // The footer's button; the header's × carries the same name.
      const closers = screen.getAllByRole('button', { name: 'Zavřít' })
      await user.click(closers[closers.length - 1])
      await waitFor(() => {
        expect(screen.queryByTestId('first-admin-result')).not.toBeInTheDocument()
      })
    })

    it('shows the copyable link when no e-mail was sent', async () => {
      const user = userEvent.setup()
      createMock.mockResolvedValue({
        ...project({ uid: 'pjc', slug: 'chata', name: 'Chata' }),
        first_admin: {
          user_uid: 'usj',
          username: 'jana',
          display_name: '',
          email: 'jana@example.com',
          account: 'created',
          mail_sent: false,
          set_password_url: 'https://k.example/password-reset/tok?project=chata',
          expires_at: '2026-10-23T10:00:00Z',
        },
      })
      const dialog = await openForOther(user)
      await user.type(within(dialog).getByLabelText('E-mail'), 'jana@example.com')
      await user.click(within(dialog).getByRole('button', { name: 'Založit projekt' }))

      const result = await screen.findByTestId('first-admin-result')
      expect(result).toHaveTextContent(/E-mail se neodeslal/)
      expect(within(result).getByLabelText('Odkaz na nastavení hesla')).toHaveValue(
        'https://k.example/password-reset/tok?project=chata',
      )
      expect(within(result).getByRole('button', { name: 'Kopírovat' })).toBeInTheDocument()
    })

    it('tells an existing account apart and gives no link', async () => {
      const user = userEvent.setup()
      createMock.mockResolvedValue({
        ...project({ uid: 'pjc', slug: 'chata', name: 'Chata' }),
        first_admin: {
          user_uid: 'usp',
          username: 'petr',
          display_name: 'Petr',
          email: 'petr@example.com',
          account: 'existing',
          mail_sent: true,
        },
      })
      const dialog = await openForOther(user)
      await user.type(within(dialog).getByLabelText('E-mail'), 'petr@example.com')
      await user.click(within(dialog).getByRole('button', { name: 'Založit projekt' }))

      const result = await screen.findByTestId('first-admin-result')
      expect(result).toHaveTextContent('stávající účet')
      expect(result).toHaveTextContent(/odešel e-mail s odkazem na projekt/)
      expect(result).not.toHaveTextContent(/Odkaz platí do/)
    })

    it('puts an admin refusal on its field', async () => {
      const user = userEvent.setup()
      createMock.mockRejectedValue(new ProjectApiError(409, 'username_taken', ''))
      const dialog = await openForOther(user)
      await user.type(within(dialog).getByLabelText('E-mail'), 'jana@example.com')
      await user.type(within(dialog).getByLabelText('Uživatelské jméno (nepovinné)'), 'jana')
      await user.click(within(dialog).getByRole('button', { name: 'Založit projekt' }))

      expect(
        await within(dialog).findByText('Toto uživatelské jméno už má jiný účet.'),
      ).toBeInTheDocument()
      expect(within(dialog).getByLabelText('Uživatelské jméno (nepovinné)')).toHaveClass(
        'is-invalid',
      )
    })
  })
})
