import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

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

    // A library saved search restores to the homepage with its filters/sort.
    const libraryLink = await screen.findByRole('link', { name: 'Old Canon shots' })
    expect(libraryLink).toHaveAttribute('href', '/?sort=oldest&camera=Canon')

    // A search saved search (mode present) restores to /search with query + mode.
    const searchLink = screen.getByRole('link', { name: 'Sunset search' })
    expect(searchLink).toHaveAttribute('href', '/search?q=sunset&mode=semantic')
  })

  it('shows the saved query under each name, inside the link', async () => {
    const query = 'svatba person:jana year:1960-1969 '.repeat(4).trim()
    fetchMock.mockResolvedValue([
      saved('ss_1', 'Wedding', { mode: 'hybrid', q: query, album: 'al1', sort: 'oldest' }),
      saved('ss_2', 'Sunset search', { mode: 'semantic', q: 'sunset' }),
      saved('ss_3', 'Old Canon shots', { sort: 'oldest' }),
      saved('ss_4', 'Everything', {}),
    ])
    renderPage()

    // The query sits inside the link, truncated on one line with the full text
    // in a tooltip, and describes the link whose name stays the saved name.
    const wedding = await screen.findByRole('link', { name: 'Wedding' })
    const queryText = within(wedding).getByText(query)
    expect(queryText).toHaveClass('text-truncate', 'font-monospace')
    expect(queryText).toHaveAttribute('title', query)
    expect(wedding).toHaveAccessibleDescription(`${query} +1 filter`)
    // Filters outrank the sort: one album facet is the badge.
    expect(within(wedding).getByText('+1 filter')).toHaveClass('badge')

    // With nothing filtering, a non-default mode is the badge…
    const sunset = screen.getByRole('link', { name: 'Sunset search' })
    expect(within(sunset).getByText('sunset')).toBeInTheDocument()
    expect(within(sunset).getByText('by content')).toHaveClass('badge')

    // …and a view saved without query text says so, with its sort as the badge.
    const canon = screen.getByRole('link', { name: 'Old Canon shots' })
    expect(within(canon).getByText('no search text')).toBeInTheDocument()
    expect(within(canon).getByText('sort: Oldest')).toHaveClass('badge')

    // An all-default view carries no badge at all.
    const everything = screen.getByRole('link', { name: 'Everything' })
    expect(everything.querySelector('.badge')).toBeNull()
  })

  it('shows the query line in Czech', async () => {
    await i18n.changeLanguage('cs')
    fetchMock.mockResolvedValue([
      saved('ss_1', 'Rodina', { person: 'su1', label: 'lb1', favorite: 'true' }),
    ])
    renderPage()

    const link = await screen.findByRole('link', { name: 'Rodina' })
    expect(within(link).getByText('bez hledaného textu')).toBeInTheDocument()
    expect(within(link).getByText('+3 filtry')).toBeInTheDocument()
  })

  it('keeps a long unbroken name inside the row on a phone', async () => {
    // A 360px row cannot fit a name plus two Czech-worded buttons: the name
    // truncates, the actions collapse to their glyph, and nothing widens the
    // page beyond the viewport.
    const long = 'Hledání'.repeat(20)
    fetchMock.mockResolvedValue([saved('ss_1', long, { sort: 'oldest' })])
    renderPage()

    const link = await screen.findByRole('link', { name: long })
    expect(link).toHaveClass('kk-min-w-0')
    expect(within(link).getByText(long)).toHaveClass('text-truncate')
    for (const [name, glyph] of [
      ['Rename', 'bi-pencil'],
      ['Delete', 'bi-trash'],
    ]) {
      const button = screen.getByRole('button', { name })
      expect(button.querySelector(`.${glyph}`)).toHaveAttribute('aria-hidden', 'true')
      expect(within(button).getByText(name)).toHaveClass('d-none', 'd-sm-inline')
      expect(button).toHaveClass('kukatko-tap-target-touch')
    }
    expect(screen.getByRole('button', { name: 'Rename' }).parentElement).toHaveClass(
      'flex-shrink-0',
    )
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

  it('optimistically deletes a saved search after confirming in the dialog', async () => {
    fetchMock.mockResolvedValue([saved('ss_1', 'Trip', { sort: 'oldest' })])
    deleteMock.mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Trip')
    await user.click(screen.getByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Delete saved search' }))

    await waitFor(() => {
      expect(screen.queryByText('Trip')).not.toBeInTheDocument()
    })
    expect(deleteMock).toHaveBeenCalledWith('ss_1')
  })

  it('restores the row and shows an error when deletion fails', async () => {
    fetchMock.mockResolvedValue([saved('ss_1', 'Trip', { sort: 'oldest' })])
    deleteMock.mockRejectedValue(new Error('boom'))
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Trip')
    await user.click(screen.getByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Delete saved search' }))

    // The row comes back and the action error surfaces.
    expect(await screen.findByText('The action failed. Please try again.')).toBeInTheDocument()
    expect(screen.getByText('Trip')).toBeInTheDocument()
  })
})
