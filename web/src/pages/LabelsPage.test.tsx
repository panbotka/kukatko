import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type Label, type LabelCount } from '../services/organize'

import { LabelsPage } from './LabelsPage'

vi.mock('../services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/organize')>()
  return {
    ...actual,
    fetchLabels: vi.fn(),
    createLabel: vi.fn(),
    updateLabel: vi.fn(),
    deleteLabel: vi.fn(),
  }
})

const { fetchLabels, createLabel, updateLabel, deleteLabel } = await import('../services/organize')
const fetchMock = vi.mocked(fetchLabels)
const createMock = vi.mocked(createLabel)
const updateMock = vi.mocked(updateLabel)
const deleteMock = vi.mocked(deleteLabel)

function label(uid: string, name: string, priority = 0): LabelCount {
  return {
    uid,
    slug: name.toLowerCase(),
    name,
    priority,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: 5,
  }
}

function auth(canWrite: boolean): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'U', role: canWrite ? 'editor' : 'viewer' },
    role: canWrite ? 'editor' : 'viewer',
    downloadToken: null,
    canWrite,
    isAdmin: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

function renderPage(canWrite = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <MemoryRouter>
          <LabelsPage />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
  createMock.mockReset()
  updateMock.mockReset()
  deleteMock.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('LabelsPage', () => {
  it('lists labels with their counts', async () => {
    fetchMock.mockResolvedValue([label('lb_1', 'Sunset')])
    renderPage()
    expect(await screen.findByText('Sunset')).toBeInTheDocument()
    expect(screen.getByText('5')).toBeInTheDocument()
  })

  it('creates a label: calls the API and adds it to the list', async () => {
    fetchMock.mockResolvedValue([])
    const created: Label = {
      uid: 'lb_new',
      slug: 'beach',
      name: 'Beach',
      priority: 0,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    }
    createMock.mockResolvedValue(created)
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('No labels yet')
    await user.click(screen.getByRole('button', { name: 'New label' }))
    await user.type(screen.getByLabelText('Name'), 'Beach')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(createMock).toHaveBeenCalledWith({ name: 'Beach', priority: 0 })
    })
    expect(await screen.findByText('Beach')).toBeInTheDocument()
  })

  it('renames a label: calls the API and updates the list', async () => {
    fetchMock.mockResolvedValue([label('lb_1', 'Sunset')])
    updateMock.mockResolvedValue({
      uid: 'lb_1',
      slug: 'sundown',
      name: 'Sundown',
      priority: 0,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-02T00:00:00Z',
    })
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Sunset')
    await user.click(screen.getByRole('button', { name: 'Rename' }))
    const input = screen.getByLabelText('Name')
    await user.clear(input)
    await user.type(input, 'Sundown')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(updateMock).toHaveBeenCalledWith('lb_1', { name: 'Sundown', priority: 0 })
    })
    expect(await screen.findByText('Sundown')).toBeInTheDocument()
  })

  it('deletes a label after confirming in the styled dialog and drops it from the list', async () => {
    fetchMock.mockResolvedValue([label('lb_1', 'Sunset')])
    deleteMock.mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Sunset')
    // The row control opens the dialog; nothing is deleted until it is confirmed.
    await user.click(screen.getByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    expect(deleteMock).not.toHaveBeenCalled()
    await user.click(within(dialog).getByRole('button', { name: 'Delete label' }))

    await waitFor(() => {
      expect(deleteMock).toHaveBeenCalledWith('lb_1')
    })
    await waitFor(() => {
      expect(screen.queryByText('Sunset')).not.toBeInTheDocument()
    })
  })

  it('hides mutation controls from viewers', async () => {
    fetchMock.mockResolvedValue([label('lb_1', 'Sunset')])
    renderPage(false)
    await screen.findByText('Sunset')
    expect(screen.queryByRole('button', { name: 'New label' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Rename' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()
  })
})
