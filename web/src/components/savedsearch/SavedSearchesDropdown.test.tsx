import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type SavedSearch } from '../../services/savedSearches'

import { SavedSearchesDropdown } from './SavedSearchesDropdown'

vi.mock('../../services/savedSearches', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/savedSearches')>()
  return { ...actual, fetchSavedSearches: vi.fn() }
})

const { fetchSavedSearches } = await import('../../services/savedSearches')
const fetchMock = vi.mocked(fetchSavedSearches)

function saved(uid: string, name: string, params: SavedSearch['params']): SavedSearch {
  return {
    uid,
    name,
    params,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

function renderDropdown() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <SavedSearchesDropdown />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
  fetchMock.mockResolvedValue([saved('ss1', 'Cats', { q: 'cat', mode: 'semantic' })])
})

describe('SavedSearchesDropdown', () => {
  it('fetches nothing until the menu is opened', () => {
    renderDropdown()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('lists the saved searches and links each one to the view it captured', async () => {
    const user = userEvent.setup()
    renderDropdown()

    await user.click(screen.getByRole('button', { name: 'Saved searches' }))

    const entry = await screen.findByRole('link', { name: 'Cats' })
    expect(entry).toHaveAttribute('href', '/search?q=cat&mode=semantic')
    expect(entry).toHaveAttribute('title', 'Open the saved search "Cats"')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('keeps the /saved management page reachable', async () => {
    const user = userEvent.setup()
    renderDropdown()

    await user.click(screen.getByRole('button', { name: 'Saved searches' }))

    const manage = await screen.findByRole('link', { name: 'Manage saved searches' })
    expect(manage).toHaveAttribute('href', '/saved')
    expect(manage).toHaveAttribute('title', 'Rename or delete your saved searches')
  })

  it('reports a failed load inside the menu', async () => {
    fetchMock.mockRejectedValue(new Error('offline'))
    const user = userEvent.setup()
    renderDropdown()

    await user.click(screen.getByRole('button', { name: 'Saved searches' }))
    expect(await screen.findByText('Could not load saved searches.')).toBeInTheDocument()
  })
})
