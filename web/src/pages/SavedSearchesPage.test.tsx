import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { type SavedSearch } from '../services/savedSearches'

import { SavedSearchesPage } from './SavedSearchesPage'

vi.mock('../services/savedSearches', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/savedSearches')>()
  return {
    ...actual,
    fetchSavedSearches: vi.fn(),
    updateSavedSearch: vi.fn(),
    deleteSavedSearch: vi.fn(),
  }
})

const { fetchSavedSearches, updateSavedSearch, deleteSavedSearch } =
  await import('../services/savedSearches')
const fetchMock = vi.mocked(fetchSavedSearches)
const updateMock = vi.mocked(updateSavedSearch)
const deleteMock = vi.mocked(deleteSavedSearch)

function saved(uid: string, name: string, params: Record<string, string>): SavedSearch {
  return {
    uid,
    name,
    params,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

function renderPage() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <SavedSearchesPage />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
  updateMock.mockReset()
  deleteMock.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('SavedSearchesPage', () => {
  it('shows the empty state when there are no saved searches', async () => {
    fetchMock.mockResolvedValue([])
    renderPage()
    expect(await screen.findByText('No saved searches yet')).toBeInTheDocument()
  })

  it('lists saved searches, each linking to the exact restored view', async () => {
    fetchMock.mockResolvedValue([
      saved('ss_1', 'Old Canon shots', { sort: 'oldest', camera: 'Canon' }),
      saved('ss_2', 'Sunset search', { mode: 'semantic', q: 'sunset' }),
    ])
    renderPage()

    // A library saved search restores to /library with its filters/sort.
    const libraryLink = await screen.findByRole('link', { name: 'Old Canon shots' })
    expect(libraryLink).toHaveAttribute('href', '/library?sort=oldest&camera=Canon')

    // A search saved search (mode present) restores to /search with query + mode.
    const searchLink = screen.getByRole('link', { name: 'Sunset search' })
    expect(searchLink).toHaveAttribute('href', '/search?q=sunset&mode=semantic')
  })

  it('renames a saved search via the API and updates the row', async () => {
    fetchMock.mockResolvedValue([saved('ss_1', 'Trip', { sort: 'oldest' })])
    updateMock.mockResolvedValue(saved('ss_1', 'Holiday', { sort: 'oldest' }))
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Trip')
    await user.click(screen.getByRole('button', { name: 'Rename' }))
    const input = screen.getByLabelText('Name')
    await user.clear(input)
    await user.type(input, 'Holiday')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(updateMock).toHaveBeenCalledWith('ss_1', { name: 'Holiday' })
    })
    expect(await screen.findByText('Holiday')).toBeInTheDocument()
  })

  it('optimistically deletes a saved search after confirmation', async () => {
    fetchMock.mockResolvedValue([saved('ss_1', 'Trip', { sort: 'oldest' })])
    deleteMock.mockResolvedValue(undefined)
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Trip')
    await user.click(screen.getByRole('button', { name: 'Delete' }))

    await waitFor(() => {
      expect(screen.queryByText('Trip')).not.toBeInTheDocument()
    })
    expect(deleteMock).toHaveBeenCalledWith('ss_1')
  })

  it('restores the row and shows an error when deletion fails', async () => {
    fetchMock.mockResolvedValue([saved('ss_1', 'Trip', { sort: 'oldest' })])
    deleteMock.mockRejectedValue(new Error('boom'))
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Trip')
    await user.click(screen.getByRole('button', { name: 'Delete' }))

    // The row comes back and the action error surfaces.
    expect(await screen.findByText('The action failed. Please try again.')).toBeInTheDocument()
    expect(screen.getByText('Trip')).toBeInTheDocument()
  })
})
