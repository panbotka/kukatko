import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import type { ProjectMembership } from '../services/auth'
import type { AdminProject } from '../services/projects'

import { SystemProjectPage } from './SystemProjectPage'

vi.mock('../services/projects', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/projects')>()
  return {
    ...actual,
    fetchAdminProjects: vi.fn(),
    updateProject: vi.fn(),
    addProjectMember: vi.fn(),
  }
})

const { fetchAdminProjects, updateProject, addProjectMember } = await import('../services/projects')
const fetchMock = vi.mocked(fetchAdminProjects)
const updateMock = vi.mocked(updateProject)
const addMock = vi.mocked(addProjectMember)

const MB = 1024 * 1024

/** The project the page shows, overridable. */
function project(overrides: Partial<AdminProject> = {}): AdminProject {
  return {
    uid: 'pjr',
    slug: 'rodina',
    name: 'Rodina',
    description: 'Rodinné fotky',
    active: true,
    storage_prefix: 'projects/pjr/',
    max_file_bytes: 50 * MB,
    max_total_bytes: null,
    members_count: 3,
    photos_count: 120,
    originals_bytes: 300 * MB,
    created_at: '2026-10-01T08:00:00Z',
    updated_at: '2026-10-01T08:00:00Z',
    ...overrides,
  }
}

/** A maintainer session with `memberships`. */
function auth(memberships: ProjectMembership[], refresh = vi.fn()): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'admin', display_name: 'Admin', role: 'maintainer' },
    isMaintainer: true,
    projects: memberships,
    refresh,
  } as unknown as AuthContextValue
}

function renderPage(memberships: ProjectMembership[] = [], refresh = vi.fn()) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(memberships, refresh)}>
        <MemoryRouter initialEntries={['/system/projects/rodina']}>
          <Routes>
            <Route path="/system/projects/:slug" element={<SystemProjectPage />} />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('cs')
  fetchMock.mockReset()
  updateMock.mockReset()
  addMock.mockReset()
  fetchMock.mockResolvedValue([project({ uid: 'pjv', slug: 'v', name: 'Veselice' }), project()])
})

describe('SystemProjectPage', () => {
  it('picks the project by slug and shows its usage, members link and storage', async () => {
    renderPage()

    expect(await screen.findByRole('heading', { level: 1, name: 'Rodina' })).toBeInTheDocument()
    expect(screen.getByLabelText('Název')).toHaveValue('Rodina')
    expect(screen.getByLabelText('Největší soubor')).toHaveValue('50')
    expect(screen.getByLabelText('Kvóta všech originálů')).toHaveValue('')
    expect(screen.getByRole('link', { name: 'Členové projektu' })).toHaveAttribute(
      'href',
      '/p/rodina/members',
    )
    expect(within(screen.getByTestId('project-storage')).getByText('projects/pjr/')).toHaveClass(
      'font-monospace',
    )
  })

  it('says "v kořeni úložiště" for the empty storage prefix', async () => {
    fetchMock.mockResolvedValue([project({ storage_prefix: '' })])
    renderPage()

    const storage = await screen.findByTestId('project-storage')
    expect(within(storage).getByText('v kořeni úložiště')).toBeInTheDocument()
  })

  it('names a slug that does not exist', async () => {
    fetchMock.mockResolvedValue([project({ slug: 'jiny' })])
    renderPage()

    expect(await screen.findByText('Projekt „rodina“ neexistuje.')).toBeInTheDocument()
  })

  it('saves only the fields that changed, a cleared limit as null', async () => {
    const user = userEvent.setup()
    updateMock.mockResolvedValue(project({ name: 'Rodina Novákových', max_file_bytes: null }))
    renderPage()

    const name = await screen.findByLabelText('Název')
    const save = screen.getByRole('button', { name: 'Uložit změny' })
    expect(save).toBeDisabled()

    await user.clear(name)
    await user.type(name, 'Rodina Novákových')
    await user.clear(screen.getByLabelText('Největší soubor'))
    await user.click(save)

    await waitFor(() => {
      expect(updateMock).toHaveBeenCalledWith('pjr', {
        name: 'Rodina Novákových',
        max_file_bytes: null,
      })
    })
    expect(await screen.findByText('Změny jsou uložené.')).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 1, name: 'Rodina Novákových' })).toBeInTheDocument()
  })

  it('deactivates only after the confirmation, and cancelling sends nothing', async () => {
    const user = userEvent.setup()
    updateMock.mockResolvedValue(project({ active: false }))
    renderPage()

    const toggle = await screen.findByLabelText('Projekt je aktivní')
    expect(toggle).toBeChecked()

    await user.click(toggle)
    let dialog = await screen.findByRole('dialog')
    expect(
      within(dialog).getByText('Členové projekt neotevřou, data zůstanou.'),
    ).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Zrušit' }))
    expect(updateMock).not.toHaveBeenCalled()
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(toggle).toBeChecked()

    await user.click(toggle)
    dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Vypnout projekt' }))

    await waitFor(() => {
      expect(updateMock).toHaveBeenCalledWith('pjr', { active: false })
    })
    await waitFor(() => {
      expect(screen.getByLabelText('Projekt je aktivní')).not.toBeChecked()
    })
  })

  it('adds the caller as an admin and then hides the button', async () => {
    const user = userEvent.setup()
    const refresh = vi.fn().mockResolvedValue(undefined)
    addMock.mockResolvedValue({
      project_uid: 'pjr',
      user_uid: 'u1',
      role: 'admin',
      approved_at: '2026-10-13T08:00:00Z',
      username: 'admin',
      display_name: 'Admin',
    })
    renderPage([], refresh)

    await user.click(await screen.findByRole('button', { name: 'Přidat mě jako člena' }))

    await waitFor(() => {
      expect(addMock).toHaveBeenCalledWith('pjr', 'u1', 'admin')
    })
    expect(refresh).toHaveBeenCalled()
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: 'Přidat mě jako člena' })).not.toBeInTheDocument()
    })
  })

  it('hides "add me" from a member of the project', async () => {
    renderPage([
      {
        uid: 'pjr',
        slug: 'rodina',
        name: 'Rodina',
        active: true,
        role: 'viewer',
        approved: true,
      },
    ])

    expect(await screen.findByRole('link', { name: 'Členové projektu' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Přidat mě jako člena' })).not.toBeInTheDocument()
  })
})
